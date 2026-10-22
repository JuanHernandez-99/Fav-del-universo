// Package procesos es el adaptador de entrada de las tareas periódicas: vencer reservas y
// enviar reposiciones al gimnasio. Corren en todas las réplicas; la base evita que dos réplicas
// procesen lo mismo (bloqueo de la fila de la reserva y SKIP LOCKED en las reposiciones).
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
