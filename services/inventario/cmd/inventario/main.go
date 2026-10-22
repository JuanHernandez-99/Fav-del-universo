// Inventario: stock por variante y canal, reservas con vencimiento, transferencias y consignación
// en el gimnasio. Arquitectura hexagonal (ARCHITECTURE §4): acá se arman los adaptadores.
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

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/adaptadores/gimnasio"
	adaptadorhttp "github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/adaptadores/http"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/adaptadores/mysql"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/adaptadores/procesos"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/aplicacion"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/observabilidad"
)

const servicio = "inventario"

func main() {
	if err := correr(); err != nil {
		slog.Error("inventario terminó con error", "error", err)
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
	if err := repo.Migrar(ctx, os.Getenv("INVENTARIO_DATOS_EJEMPLO") == "true"); err != nil {
		return fmt.Errorf("migrando la base: %w", err)
	}

	gim := gimnasio.NuevoCliente(gimnasio.Config{
		URLBase:     variable("GIMNASIO_URL", "http://gimnasio-mock:8080"),
		APIKey:      os.Getenv("GIMNASIO_API_KEY"),
		ProveedorID: variable("GIMNASIO_PROVEEDOR_ID", "fav"),
		Timeout:     duracion("GIMNASIO_TIMEOUT_MS", 2000),
	})
	s := aplicacion.NuevoServicio(repo, gim)

	go procesos.VencerReservas(ctx, s, duracion("VENCIMIENTO_CADA_MS", 10_000))
	go procesos.EnviarReposiciones(ctx, s, duracion("REPOSICIONES_CADA_MS", 5_000))

	servidor := &http.Server{
		Addr:              ":" + variable("INVENTARIO_PUERTO", "8080"),
		Handler:           conLimiteDeTiempo(adaptadorhttp.NuevoRouter(s), 4*time.Second),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	errores := make(chan error, 1)
	go func() {
		slog.Info("inventario escuchando", "direccion", servidor.Addr)
		errores <- servidor.ListenAndServe()
	}()

	select {
	case err := <-errores:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		slog.Info("apagando inventario")
	}
	ctxCierre, cancelar := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelar()
	return servidor.Shutdown(ctxCierre)
}

func abrirBase() (*sql.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true&loc=UTC&multiStatements=true&timeout=3s&readTimeout=5s&writeTimeout=5s",
		os.Getenv("INVENTARIO_DB_USER"), os.Getenv("INVENTARIO_DB_PASSWORD"),
		variable("INVENTARIO_DB_HOST", "mysql-inventario:3306"), variable("INVENTARIO_DB_NAME", "inventario"))

	db, err := otelsql.Open("mysql", dsn,
		otelsql.WithAttributes(semconv.DBSystemMySQL),
		otelsql.WithSpanOptions(otelsql.SpanOptions{
			OmitConnResetSession: true,
			OmitConnPrepare:      true,
			OmitRows:             true,
			// Solo las consultas que forman parte de una operación trazada; así las búsquedas
			// periódicas de los procesos no generan trazas sueltas.
			SpanFilter: func(ctx context.Context, _ otelsql.Method, _ string, _ []driver.NamedValue) bool {
				return trace.SpanContextFromContext(ctx).IsValid()
			},
		}))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(enteroVariable("INVENTARIO_DB_MAX_CONEXIONES", 20))
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

// conLimiteDeTiempo corta cada pedido a los 4 s, por debajo de los 5 s del gateway (ADR-005).
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
