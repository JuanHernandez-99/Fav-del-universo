// Ventas: ventas online y del local, sus estados, los pagos y los mails.
// Arquitectura hexagonal (ADR-002): acá se arman los adaptadores.
package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/XSAM/otelsql"
	_ "github.com/go-sql-driver/mysql"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/adaptadores/catalogo"
	adaptadorhttp "github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/adaptadores/http"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/adaptadores/inventario"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/adaptadores/mail"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/adaptadores/mercadopago"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/adaptadores/mysql"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/adaptadores/procesos"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/adaptadores/rabbitmq"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/aplicacion"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/observabilidad"
)

const servicio = "ventas"

func main() {
	if err := correr(); err != nil {
		slog.Error("ventas terminó con error", "error", err)
		os.Exit(1)
	}
}

func correr() error {
	instancia, _ := os.Hostname()
	nivel := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		nivel = slog.LevelDebug
	}
	observabilidad.ConfigurarLogs(servicio, instancia, nivel)

	ctx, detener := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer detener()

	cerrarTrazas, err := observabilidad.ConfigurarTrazas(ctx, servicio, instancia)
	if err != nil {
		return fmt.Errorf("configurando trazas: %w", err)
	}
	defer func() {
		ctxCierre, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelar()
		_ = cerrarTrazas(ctxCierre)
	}()

	db, err := abrirBase()
	if err != nil {
		return err
	}
	defer db.Close()

	repo := mysql.NuevoRepositorio(db)
	if err := esperarBase(ctx, repo); err != nil {
		return err
	}
	if err := repo.Migrar(ctx); err != nil {
		return fmt.Errorf("migrando la base: %w", err)
	}

	s := aplicacion.NuevoServicio(repo,
		catalogo.NuevoCliente(variable("CATALOGO_URL", "http://gateway:8081/catalogo")),
		inventario.NuevoCliente(variable("INVENTARIO_URL", "http://gateway:8081")),
		mercadopago.NuevoCliente(mercadopago.Config{
			URLBase:      variable("MERCADOPAGO_URL", "http://mercadopago-simulador:8080"),
			AccessToken:  os.Getenv("MERCADOPAGO_ACCESS_TOKEN"),
			SecretoAviso: os.Getenv("MERCADOPAGO_WEBHOOK_SECRET"),
			URLAviso:     variable("MERCADOPAGO_URL_AVISO", "http://gateway:8080/ventas/v1/pagos/aviso"),
			Timeout:      duracion("MERCADOPAGO_TIMEOUT_MS", 5000),
		}),
		mail.NuevoSMTP(variable("SMTP_DIRECCION", "mailpit:1025"), variable("MAIL_FROM", "ventas@fav.example"), duracion("SMTP_TIMEOUT_MS", 10_000)),
		variable("MAIL_ADMIN", "admin@fav.example"))

	go procesos.EjecutarTareas(ctx, s, duracion("TAREAS_CADA_MS", 1000))
	go procesos.EnviarMails(ctx, s, duracion("MAILS_CADA_MS", 2000))

	if url := os.Getenv("RABBITMQ_URL"); url != "" {
		go rabbitmq.NuevoConsumidor(url, s.ProcesarReservaVencida, duracion("REINTENTO_MENSAJE_MS", 5000)).Correr(ctx)
	} else {
		slog.Warn("RABBITMQ_URL sin definir: las ventas no pasan a Vencida")
	}

	var opcionesRouter []adaptadorhttp.Opcion
	if os.Getenv("CAPACIDAD_PUBLICA_ACTIVA") == "false" {
		opcionesRouter = append(opcionesRouter, adaptadorhttp.SinCapacidadPublica())
	}

	servidor := &http.Server{
		Addr: ":" + variable("VENTAS_PUERTO", "8080"),
		Handler: conLimiteDeTiempo(adaptadorhttp.NuevoRouter(s, adaptadorhttp.DatosTransferencia{
			Alias:   variable("TRANSFERENCIA_ALIAS", "FAV.EJEMPLO.ALIAS"),
			Titular: variable("TRANSFERENCIA_TITULAR", "FAV (ejemplo)"),
		}, opcionesRouter...), 4500*time.Millisecond),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	errores := make(chan error, 1)
	go func() {
		slog.Info("ventas escuchando", "direccion", servidor.Addr)
		errores <- servidor.ListenAndServe()
	}()

	select {
	case err := <-errores:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		slog.Info("apagando ventas")
	}
	ctxCierre, cancelar := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelar()
	return servidor.Shutdown(ctxCierre)
}

func abrirBase() (*sql.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true&loc=UTC&multiStatements=true&timeout=3s&readTimeout=5s&writeTimeout=5s",
		os.Getenv("VENTAS_DB_USER"), os.Getenv("VENTAS_DB_PASSWORD"),
		variable("VENTAS_DB_HOST", "mysql-ventas:3306"), variable("VENTAS_DB_NAME", "ventas"))

	db, err := otelsql.Open("mysql", dsn,
		otelsql.WithAttributes(semconv.DBSystemMySQL),
		otelsql.WithSpanOptions(otelsql.SpanOptions{
			OmitConnResetSession: true,
			OmitConnPrepare:      true,
			OmitRows:             true,
			// Solo las consultas de una operación trazada; las búsquedas periódicas no generan trazas sueltas.
			SpanFilter: func(ctx context.Context, _ otelsql.Method, _ string, _ []driver.NamedValue) bool {
				return trace.SpanContextFromContext(ctx).IsValid()
			},
		}))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(enteroVariable("VENTAS_DB_MAX_CONEXIONES", 20))
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)
	return db, nil
}

// esperarBase reintenta durante 60 s: en Docker Compose la base puede tardar en aceptar conexiones.
func esperarBase(ctx context.Context, repo *mysql.Repositorio) error {
	limite := time.Now().Add(60 * time.Second)
	for {
		err := repo.Ping(ctx)
		if err == nil {
			return nil
		}
		if time.Now().After(limite) {
			return fmt.Errorf("la base no responde: %w", err)
		}
		slog.Warn("esperando a la base", "error", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// conLimiteDeTiempo corta cada pedido por debajo de los 5 s del gateway, con los reintentos adentro:
// Ventas responde su propio error en lugar de que el gateway devuelva 504 (ADR-010).
func conLimiteDeTiempo(h http.Handler, limite time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancelar := context.WithTimeout(r.Context(), limite)
		defer cancelar()
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func variable(nombre, porDefecto string) string {
	if v := os.Getenv(nombre); v != "" {
		return v
	}
	return porDefecto
}

func enteroVariable(nombre string, porDefecto int) int {
	if n, err := strconv.Atoi(os.Getenv(nombre)); err == nil && n > 0 {
		return n
	}
	return porDefecto
}

func duracion(nombre string, porDefectoMs int) time.Duration {
	return time.Duration(enteroVariable(nombre, porDefectoMs)) * time.Millisecond
}
