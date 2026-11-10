// Usuarios: registro, inicio de sesión, emisión del token y roles. Arquitectura en capas (ADR-002).
//
// Con el argumento generar-claves solo crea el par de claves del token y termina: es lo que corre el
// contenedor usuarios-init (ADR-015).
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

	"github.com/JuanHernandez-99/Fav-del-universo/services/usuarios/internal/claves"
	"github.com/JuanHernandez-99/Fav-del-universo/services/usuarios/internal/handler"
	"github.com/JuanHernandez-99/Fav-del-universo/services/usuarios/internal/observabilidad"
	"github.com/JuanHernandez-99/Fav-del-universo/services/usuarios/internal/repository"
	"github.com/JuanHernandez-99/Fav-del-universo/services/usuarios/internal/service"
)

const servicio = "usuarios"

func main() {
	var err error
	if len(os.Args) > 1 && os.Args[1] == "generar-claves" {
		err = generarClaves()
	} else {
		err = correr()
	}
	if err != nil {
		slog.Error("usuarios terminó con error", "error", err)
		os.Exit(1)
	}
}

func rutaPrivada() string { return variable("JWT_CLAVE_PRIVADA", "/claves/privada/jwt_private.pem") }

func generarClaves() error {
	nueva, err := claves.Asegurar(rutaPrivada(), variable("JWT_CLAVE_PUBLICA", "/claves/publica/jwt_public.pem"))
	if err != nil {
		return fmt.Errorf("generando las claves: %w", err)
	}
	slog.Info("claves del token listas", "nueva", nueva)
	return nil
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

	clave, err := claves.LeerPrivada(rutaPrivada())
	if err != nil {
		return fmt.Errorf("leyendo la clave del token: %w", err)
	}

	db, err := abrirBase()
	if err != nil {
		return err
	}
	defer db.Close()

	repo := repository.Nuevo(db)
	if err := esperarBase(ctx, repo); err != nil {
		return err
	}
	if err := repo.Migrar(ctx); err != nil {
		return fmt.Errorf("migrando la base: %w", err)
	}

	s, err := service.Nuevo(repo, clave, time.Duration(enteroVariable("JWT_EXPIRATION_MINUTES", 60))*time.Minute)
	if err != nil {
		return err
	}
	if err := s.AsegurarAdministrador(ctx, os.Getenv("ADMIN_EMAIL"), os.Getenv("ADMIN_PASSWORD")); err != nil {
		return err
	}

	servidor := &http.Server{
		Addr:              ":" + variable("USUARIOS_PUERTO", "8080"),
		Handler:           conLimiteDeTiempo(handler.NuevoRouter(s), 4*time.Second),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	errores := make(chan error, 1)
	go func() {
		slog.Info("usuarios escuchando", "direccion", servidor.Addr)
		errores <- servidor.ListenAndServe()
	}()

	select {
	case err := <-errores:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		slog.Info("apagando usuarios")
	}
	ctxCierre, cancelar := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelar()
	return servidor.Shutdown(ctxCierre)
}

func abrirBase() (*sql.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true&loc=UTC&multiStatements=true&timeout=3s&readTimeout=5s&writeTimeout=5s",
		os.Getenv("USUARIOS_DB_USER"), os.Getenv("USUARIOS_DB_PASSWORD"),
		variable("USUARIOS_DB_HOST", "mysql-usuarios:3306"), variable("USUARIOS_DB_NAME", "usuarios"))

	db, err := otelsql.Open("mysql", dsn,
		otelsql.WithAttributes(semconv.DBSystemMySQL),
		otelsql.WithSpanOptions(otelsql.SpanOptions{
			OmitConnResetSession: true,
			OmitConnPrepare:      true,
			OmitRows:             true,
			SpanFilter: func(ctx context.Context, _ otelsql.Method, _ string, _ []driver.NamedValue) bool {
				return trace.SpanContextFromContext(ctx).IsValid()
			},
		}))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(enteroVariable("USUARIOS_DB_MAX_CONEXIONES", 20))
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)
	return db, nil
}

// esperarBase reintenta durante 60 s: en Docker Compose la base puede tardar en aceptar conexiones.
func esperarBase(ctx context.Context, repo *repository.Repositorio) error {
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

// conLimiteDeTiempo corta cada pedido por debajo de los 5 s del gateway (ADR-005).
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
