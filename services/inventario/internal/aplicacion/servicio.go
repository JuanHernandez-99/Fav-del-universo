// Package aplicacion contiene los casos de uso de Inventario. Coordina el dominio
// (que decide las reglas) y los puertos (que guardan y leen), sin conocer HTTP ni SQL.
package aplicacion

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/dominio"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/puertos"
)

// MaxVariantesConsulta limita cuántas variantes se consultan de una vez.
const MaxVariantesConsulta = 200

// Servicio expone los casos de uso de Inventario.
type Servicio struct {
	repo  puertos.RepositorioInventario
	reloj puertos.Reloj
	ids   puertos.GeneradorID
}

func NuevoServicio(repo puertos.RepositorioInventario, reloj puertos.Reloj, ids puertos.GeneradorID) *Servicio {
	return &Servicio{repo: repo, reloj: reloj, ids: ids}
}

// PedidoReserva son los datos para reservar stock online.
type PedidoReserva struct {
	ClaveIdempotencia  string
	Referencia         string
	VencimientoMinutos int
	Items              []dominio.Item
	// CorrelacionID del pedido; se guarda con la reserva. No forma parte de la
	// huella: un reintento puede llegar con otro.
	CorrelacionID string
}

// Reservar aparta stock online de todas las variantes, o de ninguna.
// Repetir el pedido con la misma clave devuelve la misma reserva, sin apartar dos veces.
func (s *Servicio) Reservar(ctx context.Context, p PedidoReserva) (dominio.Reserva, error) {
	clave := strings.TrimSpace(p.ClaveIdempotencia)
	if clave == "" || len(clave) > 100 {
		return dominio.Reserva{}, &dominio.ErrorValidacion{Errores: []dominio.CampoInvalido{
			{Campo: "Idempotency-Key", Mensaje: "es obligatorio y no puede superar los 100 caracteres"},
		}}
	}
	duracion := time.Duration(p.VencimientoMinutos) * time.Minute
	r, err := dominio.NuevaReserva(s.ids.Nuevo(), p.Referencia, p.Items, duracion, s.reloj.Ahora())
	if err != nil {
		return dominio.Reserva{}, err
	}
	r.CorrelacionID = p.CorrelacionID
	return s.repo.GuardarReservaSiAlcanza(ctx, r, clave, huella(r, p.VencimientoMinutos))
}

// ObtenerReserva devuelve una reserva por su id.
func (s *Servicio) ObtenerReserva(ctx context.Context, id string) (dominio.Reserva, error) {
	return s.repo.ObtenerReserva(ctx, id)
}

// Confirmar descuenta el stock de una reserva en forma definitiva (pago aprobado).
func (s *Servicio) Confirmar(ctx context.Context, id string) (dominio.Reserva, error) {
	return s.transicionar(ctx, id,
		func(r dominio.Reserva, ahora time.Time) (bool, error) { return r.Confirmar(ahora) },
		func(ahora time.Time) error { return s.repo.ConfirmarSiVigente(ctx, id, ahora) })
}

// Liberar devuelve las unidades de una reserva (por ejemplo, compra cancelada).
func (s *Servicio) Liberar(ctx context.Context, id string) (dominio.Reserva, error) {
	return s.transicionar(ctx, id,
		func(r dominio.Reserva, _ time.Time) (bool, error) { return r.Liberar() },
		func(_ time.Time) error { return s.repo.LiberarSiActiva(ctx, id) })
}

// transicionar aplica un cambio de estado: el dominio decide si corresponde y el
// repositorio lo aplica en forma atómica. Si otra operación cambió la reserva en el
// medio (por ejemplo, venció), se vuelve a leer y el dominio decide de nuevo.
func (s *Servicio) transicionar(
	ctx context.Context, id string,
	decidir func(dominio.Reserva, time.Time) (bool, error),
	aplicar func(time.Time) error,
) (dominio.Reserva, error) {
	for intento := 0; intento < 3; intento++ {
		r, err := s.repo.ObtenerReserva(ctx, id)
		if err != nil {
			return dominio.Reserva{}, err
		}
		ahora := s.reloj.Ahora()
		cambia, err := decidir(r, ahora)
		if err != nil || !cambia {
			return r, err
		}
		err = aplicar(ahora)
		if errors.Is(err, puertos.ErrEstadoCambio) {
			continue
		}
		if err != nil {
			return dominio.Reserva{}, err
		}
		return s.repo.ObtenerReserva(ctx, id)
	}
	return dominio.Reserva{}, fmt.Errorf("reserva %s: demasiados cambios concurrentes", id)
}

// VencerPendientes vence las reservas activas con el plazo cumplido y devuelve
// cuántas venció. Por cada una se guarda el evento ReservaVencida, que arma el
// dominio. Lo llama un proceso periódico.
func (s *Servicio) VencerPendientes(ctx context.Context, limite int) (int, error) {
	ahora := s.reloj.Ahora()
	ids, err := s.repo.ReservasParaVencer(ctx, ahora, limite)
	if err != nil {
		return 0, err
	}
	vencidas := 0
	for _, id := range ids {
		r, err := s.repo.ObtenerReserva(ctx, id)
		if err != nil {
			return vencidas, err
		}
		if !r.DebeVencer(ahora) {
			continue // otra operación la confirmó o liberó antes
		}
		ok, err := s.repo.VencerSiCorresponde(ctx, id, ahora, r.EventoVencimiento(s.ids.Nuevo(), ahora))
		if err != nil {
			return vencidas, err
		}
		if ok {
			vencidas++
		}
	}
	return vencidas, nil
}

// ConsultarStock devuelve el stock de las variantes en los dos canales.
func (s *Servicio) ConsultarStock(ctx context.Context, varianteIDs []string) ([]dominio.Stock, error) {
	limpias := make([]string, 0, len(varianteIDs))
	vistas := map[string]bool{}
	for _, id := range varianteIDs {
		id = strings.TrimSpace(id)
		if id != "" && !vistas[id] {
			vistas[id] = true
			limpias = append(limpias, id)
		}
	}
	if len(limpias) == 0 || len(limpias) > MaxVariantesConsulta {
		return nil, &dominio.ErrorValidacion{Errores: []dominio.CampoInvalido{
			{Campo: "varianteId", Mensaje: fmt.Sprintf("indicar entre 1 y %d variantes", MaxVariantesConsulta)},
		}}
	}
	return s.repo.ConsultarStock(ctx, limpias)
}

// Disponible indica si el servicio puede atender (la base responde).
func (s *Servicio) Disponible(ctx context.Context) error { return s.repo.Disponible(ctx) }

// huella resume el contenido del pedido. Con la misma clave de idempotencia,
// una huella distinta significa que se reutilizó la clave para otro pedido.
func huella(r dominio.Reserva, minutos int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%d", r.Referencia, minutos)
	for _, it := range r.Items {
		fmt.Fprintf(&b, "|%s:%d", it.VarianteID, it.Cantidad)
	}
	suma := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(suma[:])
}
