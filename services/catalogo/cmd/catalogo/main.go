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
	"strconv"
	"syscall"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/busqueda"
	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/cache"
	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/eventos"
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

	var opcionesServicio []service.Opcion
	urlBusqueda := os.Getenv("ELASTICSEARCH_URL")
	if urlBusqueda != "" {
		opcionesServicio = append(opcionesServicio, service.ConIndice(busqueda.Nuevo(urlBusqueda)))
	} else {
		slog.Warn("ELASTICSEARCH_URL sin definir: la búsqueda usa el texto de MongoDB")
	}
	if direccion := os.Getenv("MEMCACHED_ADDR"); direccion != "" && os.Getenv("CACHE_ENABLED") != "false" {
		memcached := cache.Nuevo(direccion)
		defer func() { _ = memcached.Cerrar() }()
		opcionesServicio = append(opcionesServicio, service.ConCache(memcached))
		go service.VigilarEvicciones(ctx, memcached.Evicciones)
		slog.Info("caché activada", "memcached", direccion)
	} else {
		slog.Info("caché apagada") // CACHE_ENABLED=false sirve para medir sin caché (ADR-007)
	}
	s := service.Nuevo(repo, opcionesServicio...)
	if urlBusqueda != "" {
		cerrarEventos := iniciarIndexacion(ctx, s)
		defer cerrarEventos()
	}

	var opcionesRouter []handler.Opcion
	if os.Getenv("CAPACIDAD_PUBLICA_ACTIVA") == "false" {
		opcionesRouter = append(opcionesRouter, handler.SinCapacidadPublica())
	}

	servidor := &http.Server{
		Addr:              ":" + variable("CATALOGO_PUERTO", "8080"),
		Handler:           handler.ConCORS(conLimiteDeTiempo(handler.NuevoRouter(s, opcionesRouter...), 4*time.Second), os.Getenv("CORS_ORIGENES")),
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

// iniciarIndexacion arranca en segundo plano lo que mantiene el índice (ADR-006). Corre en todas las réplicas: si
// dos avisan o indexan lo mismo no hay daño, porque Elasticsearch no aplica una versión igual o menor. Catálogo
// atiende aunque Elasticsearch o RabbitMQ estén caídos.
func iniciarIndexacion(ctx context.Context, s *service.Servicio) func() {
	go func() {
		for ctx.Err() == nil {
			err := s.PrepararIndice(ctx)
			if err == nil {
				return
			}
			slog.Warn("no se pudo preparar el índice de búsqueda; se reintenta", "error", err)
			select {
			case <-time.After(5 * time.Second):
			case <-ctx.Done():
			}
		}
	}()

	urlBroker := os.Getenv("RABBITMQ_URL")
	if urlBroker == "" {
		slog.Warn("RABBITMQ_URL sin definir: los cambios no llegan al índice hasta reconstruirlo")
		return func() {}
	}
	esperaReintento := duracion("REINTENTO_INDEXADOR_MS", 5_000)
	publicador := eventos.NuevoPublicador(urlBroker, esperaReintento)
	go func() {
		reloj := time.NewTicker(duracion("PUBLICADOR_CADA_MS", 1_000))
		defer reloj.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-reloj.C:
				if _, err := s.PublicarCambios(ctx, 100, publicador.Publicar); err != nil && ctx.Err() == nil {
					slog.Warn("no se pudieron avisar los cambios; quedan pendientes", "error", err)
				}
			}
		}
	}()
	consumidor := eventos.NuevoConsumidor(urlBroker, func(ctx context.Context, productoID string) error {
		err := s.IndexarProducto(ctx, productoID)
		if errors.Is(err, busqueda.ErrNoDisponible) {
			return fmt.Errorf("%w: %v", eventos.ErrTransitorio, err)
		}
		return err
	}, esperaReintento)
	go consumidor.Correr(ctx)
	return publicador.Cerrar
}

func duracion(nombre string, porDefectoMS int) time.Duration {
	if v, err := strconv.Atoi(os.Getenv(nombre)); err == nil && v > 0 {
		return time.Duration(v) * time.Millisecond
	}
	return time.Duration(porDefectoMS) * time.Millisecond
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
