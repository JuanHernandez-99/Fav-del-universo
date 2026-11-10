// Package procesos es el adaptador de entrada de las tareas periódicas: lo que Ventas le debe a
// Inventario y el envío de mails. La base evita que dos réplicas procesen lo mismo (SKIP LOCKED).
package procesos

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/aplicacion"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/observabilidad"
)

const lote = 20

var tracer = otel.Tracer("ventas/procesos")

func EjecutarTareas(ctx context.Context, s *aplicacion.Servicio, cada time.Duration) {
	cadaVuelta(ctx, cada, func() {
		tareas, err := s.TomarTareas(ctx, lote)
		if err != nil {
			slog.ErrorContext(ctx, "no se pudieron tomar las tareas", "error", err)
			return
		}
		for _, t := range tareas {
			procesar(ctx, "tarea_"+string(t.Tipo), attribute.String("venta_id", t.VentaID), func(ctx context.Context) (bool, error) {
				return s.EjecutarTarea(ctx, t)
			})
		}
	})
}

func EnviarMails(ctx context.Context, s *aplicacion.Servicio, cada time.Duration) {
	cadaVuelta(ctx, cada, func() {
		mails, err := s.TomarMails(ctx, lote)
		if err != nil {
			slog.ErrorContext(ctx, "no se pudieron tomar los mails", "error", err)
			return
		}
		for _, m := range mails {
			procesar(ctx, "enviar_mail", attribute.String("venta_id", m.VentaID), func(ctx context.Context) (bool, error) {
				return s.EnviarMail(ctx, m)
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

// procesar abre una traza y un correlation id por elemento.
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
