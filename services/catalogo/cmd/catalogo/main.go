// Catálogo: productos, variantes, categorías, promociones y precio vigente.
// Capas Handler → Service → Repository (D2). Acá se arman las piezas y se arranca el servidor.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/handler"
	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/repository"
	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/service"
)

const servicio = "catalogo"

func main() {
	if err := correr(); err != nil {
		slog.Error("catalogo terminó con error", "error", err)
		os.Exit(1)
	}
}

func correr() error {
	// Logs en JSON con el servicio y la instancia en cada línea: con dos réplicas hay que saber cuál escribió.
	instancia, _ := os.Hostname()
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("servicio", servicio, "instancia", instancia))

	// ctx se cancela con Ctrl+C o con SIGTERM (lo que manda Docker al detener el contenedor).
	ctx, detener := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer detener()

	repo, err := repository.Conectar(uriMongo(), variable("MONGO_DB", "catalogo"))
	if err != nil {
		return fmt.Errorf("configurando mongo: %w", err)
	}
	defer func() {
		ctxCierre, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelar()
		_ = repo.Cerrar(ctxCierre)
	}()
	if err := esperarBase(ctx, repo); err != nil {
		return err
	}
	if err := repo.AsegurarIndices(ctx); err != nil {
		return fmt.Errorf("creando índices: %w", err)
	}
	slog.Info("conectado a mongo")

	s := service.Nuevo(repo)

	servidor := &http.Server{
		Addr:              ":" + variable("CATALOGO_PUERTO", "8080"),
		Handler:           conLimiteDeTiempo(handler.NuevoRouter(s), 4*time.Second),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	errores := make(chan error, 1)
	go func() {
		slog.Info("catalogo escuchando", "direccion", servidor.Addr)
		errores <- servidor.ListenAndServe()
	}()

	select {
	case err := <-errores:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		slog.Info("apagando catalogo")
	}
	// Cierre ordenado: deja terminar los pedidos en curso, con un límite de 10 s.
	ctxCierre, cancelar := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelar()
	return servidor.Shutdown(ctxCierre)
}

// uriMongo arma la URI con las variables del .env. authSource=admin es obligatorio: el usuario root
// que crea MONGO_INITDB_ROOT_* vive en la base admin, no en la del catálogo.
func uriMongo() string {
	u := url.URL{
		Scheme:   "mongodb",
		User:     url.UserPassword(os.Getenv("MONGO_INITDB_ROOT_USERNAME"), os.Getenv("MONGO_INITDB_ROOT_PASSWORD")),
		Host:     variable("MONGO_HOST", "mongo-catalogo:27017"),
		Path:     "/" + variable("MONGO_DB", "catalogo"),
		RawQuery: "authSource=admin",
	}
	return u.String()
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

// conLimiteDeTiempo corta cada pedido a los 4 s, por debajo de los 5 s del gateway (ADR-005).
// El ctx con límite llega hasta Mongo: si la consulta tarda más, el driver la cancela.
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
