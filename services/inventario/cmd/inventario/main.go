// Servicio de Inventario: stock por variante y canal, y reservas con vencimiento.
// Arquitectura hexagonal: este archivo solo arma las piezas (adaptadores → núcleo).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/adaptadores"
	httpadapter "github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/adaptadores/http"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/adaptadores/mysql"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/adaptadores/rabbitmq"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/aplicacion"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("servicio", "inventario")
	slog.SetDefault(log)

	db, err := mysql.Abrir(mysql.Config{
		Host:    env("INVENTARIO_DB_HOST", "localhost"),
		Puerto:  env("INVENTARIO_DB_PORT", "3306"),
		Usuario: env("INVENTARIO_DB_USER", "inventario"),
		Clave:   env("INVENTARIO_DB_PASSWORD", ""),
		Base:    env("INVENTARIO_DB_NAME", "inventario"),
	})
	if err != nil {
		log.Error("configuración de base inválida", "error", err.Error())
		os.Exit(1)
	}
	defer db.Close()
	esperarBase(db.PingContext, log)

	repo := mysql.NuevoRepositorio(db)
	reloj := adaptadores.RelojSistema{}
	svc := aplicacion.NuevoServicio(repo, reloj, adaptadores.GeneradorUUID{})

	ctx, parar := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer parar()

	go vencerPeriodicamente(ctx, svc, envDuracion("INVENTARIO_VENCIMIENTO_INTERVALO", 30*time.Second), log)

	if url := env("INVENTARIO_RABBITMQ_URL", ""); url != "" {
		pub := rabbitmq.NuevoPublicador(url)
		defer pub.Cerrar()
		relevo := aplicacion.NuevoRelevo(repo, pub, reloj)
		go publicarPeriodicamente(ctx, relevo, envDuracion("INVENTARIO_OUTBOX_INTERVALO", time.Second), log)
	} else {
		log.Warn("INVENTARIO_RABBITMQ_URL no definida: los eventos quedan en el outbox sin publicar")
	}

	srv := &http.Server{
		Addr:              ":" + env("INVENTARIO_PUERTO", "8080"),
		Handler:           httpadapter.NuevoHandler(svc, log),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}
	go func() {
		log.Info("escuchando", "direccion", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("servidor detenido", "error", err.Error())
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	log.Info("apagando")
	apagar, cancelar := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelar()
	srv.Shutdown(apagar) //nolint:errcheck
}

// vencerPeriodicamente libera las reservas con el plazo cumplido (RN-08, RN-09).
func vencerPeriodicamente(ctx context.Context, svc *aplicacion.Servicio, cada time.Duration, log *slog.Logger) {
	t := time.NewTicker(cada)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n, err := svc.VencerPendientes(ctx, 100)
			if err != nil {
				log.Error("vencimiento de reservas", "error", err.Error())
			} else if n > 0 {
				log.Info("reservas vencidas", "cantidad", n)
			}
		}
	}
}

// publicarPeriodicamente publica los eventos del outbox en RabbitMQ (ADR-005 §5).
// Si el broker no responde, los eventos esperan y se reintenta en la vuelta
// siguiente. Solo se loguea al empezar a fallar y al recuperarse.
func publicarPeriodicamente(ctx context.Context, relevo *aplicacion.Relevo, cada time.Duration, log *slog.Logger) {
	const lote = 100
	t := time.NewTicker(cada)
	defer t.Stop()
	fallando := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for {
				n, err := relevo.PublicarPendientes(ctx, lote)
				if err != nil {
					if !fallando && ctx.Err() == nil {
						log.Error("publicación de eventos: quedan en el outbox", "error", err.Error())
					}
					fallando = true
					break
				}
				if fallando {
					log.Info("publicación de eventos restablecida")
					fallando = false
				}
				if n > 0 {
					log.Info("eventos publicados", "cantidad", n)
				}
				if n < lote {
					break
				}
			}
		}
	}
}

// esperarBase reintenta la conexión mientras la base arranca (hasta 90 segundos).
func esperarBase(ping func(context.Context) error, log *slog.Logger) {
	limite := time.Now().Add(90 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := ping(ctx)
		cancel()
		if err == nil {
			return
		}
		if time.Now().After(limite) {
			log.Error("la base no responde", "error", err.Error())
			os.Exit(1)
		}
		log.Info("esperando la base", "error", err.Error())
		time.Sleep(2 * time.Second)
	}
}

func env(clave, porDefecto string) string {
	if v := os.Getenv(clave); v != "" {
		return v
	}
	return porDefecto
}

func envDuracion(clave string, porDefecto time.Duration) time.Duration {
	if v := os.Getenv(clave); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
		if s, err := strconv.Atoi(v); err == nil && s > 0 {
			return time.Duration(s) * time.Second
		}
	}
	return porDefecto
}
