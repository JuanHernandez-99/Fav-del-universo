// Package procesos es el adaptador de entrada de las tareas periódicas: lo que Ventas le debe a
// Inventario y el envío de mails. La base evita que dos réplicas procesen lo mismo (SKIP LOCKED).
package procesos

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/aplicacion"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/observabilidad"
)

const lote = 20

func EjecutarTareas(ctx context.Context, s *aplicacion.Servicio, cada time.Duration) {
	cadaVuelta(ctx, cada, func() {
		tareas, err := s.TomarTareas(ctx, lote)
		if err != nil {
			slog.ErrorContext(ctx, "no se pudieron tomar las tareas", "error", err)
			return
		}
		for _, t := range tareas {
			procesar(ctx, t.TrackID, "tarea_"+string(t.Tipo), slog.String("venta_id", t.VentaID), func(ctx context.Context) (bool, error) {
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
			procesar(ctx, m.TrackID, "enviar_mail", slog.String("venta_id", m.VentaID), func(ctx context.Context) (bool, error) {
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

// procesar usa el track id de la venta de cada elemento (ADR-020). Las tareas y los mails
// guardados antes de ese cambio no lo tienen: usan uno propio del proceso.
func procesar(ctx context.Context, trackID, nombre string, id slog.Attr, tarea func(context.Context) (bool, error)) {
	if trackID == "" {
		trackID = "proceso-" + uuid.NewString()
	}
	ctx = observabilidad.ConCorrelationID(ctx, trackID)

	hecho, err := tarea(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "falló el proceso", "proceso", nombre, id, "error", err)
		return
	}
	if hecho {
		observabilidad.ProcesosEjecutados.WithLabelValues(nombre).Inc()
	}
}
