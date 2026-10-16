package aplicacion

import (
	"context"
	"fmt"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/puertos"
)

// Relevo publica en el broker los eventos guardados en el outbox (ADR-005 §5).
// La entrega es al menos una vez: si se cae entre publicar y marcar, el evento
// se vuelve a publicar en la vuelta siguiente y el consumidor lo descarta por su id.
type Relevo struct {
	outbox puertos.Outbox
	pub    puertos.PublicadorEventos
	reloj  puertos.Reloj
}

func NuevoRelevo(outbox puertos.Outbox, pub puertos.PublicadorEventos, reloj puertos.Reloj) *Relevo {
	return &Relevo{outbox: outbox, pub: pub, reloj: reloj}
}

// PublicarPendientes publica hasta limite eventos, en el orden en que se guardaron,
// y devuelve cuántos publicó. Se detiene en el primer error para no desordenarlos:
// los que faltan quedan en el outbox para la vuelta siguiente.
func (r *Relevo) PublicarPendientes(ctx context.Context, limite int) (int, error) {
	pendientes, err := r.outbox.Pendientes(ctx, limite)
	if err != nil {
		return 0, err
	}
	for i, m := range pendientes {
		if err := r.pub.Publicar(ctx, m); err != nil {
			return i, fmt.Errorf("publicar evento %s: %w", m.ID, err)
		}
		if err := r.outbox.MarcarPublicado(ctx, m.ID, r.reloj.Ahora()); err != nil {
			return i, fmt.Errorf("marcar evento %s: %w", m.ID, err)
		}
	}
	return len(pendientes), nil
}
