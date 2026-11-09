// Package procesos es el adaptador de entrada de las tareas periódicas: vencer reservas, enviar
// reposiciones al gimnasio y publicar eventos. Corren en todas las réplicas; la base evita que dos
// réplicas procesen lo mismo (bloqueo de la fila de la reserva y SKIP LOCKED en lo demás).
package procesos

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/aplicacion"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/observabilidad"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/puertos"
)

const lote = 50

var tracer = otel.Tracer("inventario/procesos")

func VencerReservas(ctx context.Context, s *aplicacion.Servicio, cada time.Duration) {
	cadaVuelta(ctx, cada, func() {
		ids, err := s.ReservasPorVencer(ctx, lote)
		if err != nil {
			slog.ErrorContext(ctx, "no se pudieron buscar las reservas vencidas", "error", err)
			return
		}
		for _, id := range ids {
			procesar(ctx, "vencer_reserva", attribute.String("reserva_id", id), func(ctx context.Context) (bool, error) {
				return s.VencerReserva(ctx, id)
			})
		}
	})
}

func EnviarReposiciones(ctx context.Context, s *aplicacion.Servicio, cada time.Duration) {
	cadaVuelta(ctx, cada, func() {
		pendientes, err := s.TomarReposiciones(ctx, lote)
		if err != nil {
			slog.ErrorContext(ctx, "no se pudieron tomar las reposiciones pendientes", "error", err)
			return
		}
		for _, r := range pendientes {
			procesar(ctx, "enviar_reposicion", attribute.String("reposicion_id", r.ID), func(ctx context.Context) (bool, error) {
				return s.EnviarReposicion(ctx, r)
			})
		}
	})
}

// VigilarReposiciones publica cuántas hay en cada estado, para el tablero y las alertas (ADR-011). Las dos réplicas
// informan lo mismo: se lee con max.
func VigilarReposiciones(ctx context.Context, s *aplicacion.Servicio, cada time.Duration) {
	cadaVuelta(ctx, cada, func() {
		resumen, antiguedad, err := s.ResumirReposiciones(ctx)
		if err != nil {
			slog.WarnContext(ctx, "no se pudieron resumir las reposiciones", "error", err)
			return
		}
		for estado, cantidad := range resumen {
			observabilidad.ReposicionesPorEstado.WithLabelValues(string(estado)).Set(float64(cantidad))
		}
		observabilidad.ReposicionPendienteAntiguedad.Set(antiguedad.Seconds())
	})
}

// PublicarEventos envía el outbox a RabbitMQ. Si el broker está caído, los eventos esperan (ADR-005).
func PublicarEventos(ctx context.Context, s *aplicacion.Servicio, publicador puertos.PublicadorEventos, cada time.Duration) {
	cadaVuelta(ctx, cada, func() {
		publicados, err := s.PublicarEventos(ctx, publicador, lote)
		if err != nil {
			slog.ErrorContext(ctx, "no se pudo leer el outbox", "error", err)
			return
		}
		if publicados > 0 {
			observabilidad.ProcesosEjecutados.WithLabelValues("publicar_evento").Add(float64(publicados))
		}
	})
}

func cadaVuelta(ctx context.Context, cada time.Duration, vuelta func()) {
	reloj := time.NewTicker(cada)
	defer reloj.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-reloj.C:
			vuelta()
		}
	}
}

// procesar abre una traza y un correlation id por elemento. Las búsquedas de trabajo no se trazan:
// una vuelta sin nada que hacer no deja ruido en Jaeger.
func procesar(ctx context.Context, nombre string, id attribute.KeyValue, tarea func(context.Context) (bool, error)) {
	ctx = observabilidad.ConCorrelationID(ctx, "proceso-"+uuid.NewString())
	ctx, span := tracer.Start(ctx, "proceso "+nombre)
	defer span.End()
	span.SetAttributes(id)

	hecho, err := tarea(ctx)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		slog.ErrorContext(ctx, "falló el proceso", "proceso", nombre, "error", err)
		return
	}
	if hecho {
		observabilidad.ProcesosEjecutados.WithLabelValues(nombre).Inc()
	}
}
