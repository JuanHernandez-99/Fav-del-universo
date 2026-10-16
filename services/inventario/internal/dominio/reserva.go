package dominio

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// EstadoReserva es el ciclo de vida de una reserva.
//
//	ACTIVA ──confirmar──► CONFIRMADA
//	   │ ──liberar────► LIBERADA
//	   └──vence───────► VENCIDA
type EstadoReserva string

const (
	EstadoActiva     EstadoReserva = "ACTIVA"
	EstadoConfirmada EstadoReserva = "CONFIRMADA"
	EstadoLiberada   EstadoReserva = "LIBERADA"
	EstadoVencida    EstadoReserva = "VENCIDA"
)

// Límites de duración de una reserva: 20 minutos con Mercado Pago (RN-08)
// y 48 horas con transferencia (RN-09).
const (
	DuracionMinima = time.Minute
	DuracionMaxima = 48 * time.Hour
	largoMaximoID  = 64
)

// Item es una variante y la cantidad que se aparta.
type Item struct {
	VarianteID string
	Cantidad   int
}

// Reserva aparta stock online de una o más variantes hasta que se confirma,
// se libera o vence. Las reservas siempre usan el stock online (RN-06).
type Reserva struct {
	ID         string
	Estado     EstadoReserva
	Referencia string // identificador del que reserva (por ejemplo, la venta)
	Items      []Item
	CreadaEn   time.Time
	VenceEn    time.Time
	// CorrelacionID es el del pedido que creó la reserva. Viaja en los eventos
	// que la reserva genere más tarde, como el vencimiento (ADR-005 §7).
	CorrelacionID string
}

// NuevaReserva valida el pedido y arma una reserva ACTIVA.
// Los ítems quedan ordenados por variante para que la base los bloquee
// siempre en el mismo orden (evita interbloqueos entre reservas concurrentes).
func NuevaReserva(id, referencia string, items []Item, duracion time.Duration, ahora time.Time) (Reserva, error) {
	verr := &ErrorValidacion{}
	if len(referencia) > 100 {
		verr.agregar("referencia", "no puede superar los 100 caracteres")
	}
	if duracion < DuracionMinima || duracion > DuracionMaxima {
		verr.agregar("vencimientoMinutos", fmt.Sprintf("debe estar entre %d y %d", int(DuracionMinima.Minutes()), int(DuracionMaxima.Minutes())))
	}
	if len(items) == 0 {
		verr.agregar("items", "debe tener al menos un ítem")
	}
	vistos := map[string]bool{}
	for i, it := range items {
		campo := fmt.Sprintf("items[%d]", i)
		id := strings.TrimSpace(it.VarianteID)
		switch {
		case id == "":
			verr.agregar(campo+".varianteId", "es obligatorio")
		case len(id) > largoMaximoID:
			verr.agregar(campo+".varianteId", fmt.Sprintf("no puede superar los %d caracteres", largoMaximoID))
		case vistos[id]:
			verr.agregar(campo+".varianteId", "está repetida")
		}
		vistos[id] = true
		if it.Cantidad < 1 {
			verr.agregar(campo+".cantidad", "debe ser mayor o igual a 1")
		}
	}
	if !verr.vacio() {
		return Reserva{}, verr
	}

	copia := make([]Item, len(items))
	for i, it := range items {
		copia[i] = Item{VarianteID: strings.TrimSpace(it.VarianteID), Cantidad: it.Cantidad}
	}
	sort.Slice(copia, func(i, j int) bool { return copia[i].VarianteID < copia[j].VarianteID })

	ahora = ahora.UTC().Truncate(time.Millisecond) // la base guarda milisegundos
	return Reserva{
		ID:         id,
		Estado:     EstadoActiva,
		Referencia: referencia,
		Items:      copia,
		CreadaEn:   ahora,
		VenceEn:    ahora.Add(duracion),
	}, nil
}

// Vigente indica si la reserva sigue activa y sin vencer en ese momento.
func (r Reserva) Vigente(ahora time.Time) bool {
	return r.Estado == EstadoActiva && ahora.Before(r.VenceEn)
}

// Confirmar decide si la reserva se puede confirmar.
// Devuelve cambia=false sin error si ya estaba confirmada: confirmar dos veces
// tiene el mismo efecto que confirmar una (idempotente).
// Una reserva activa pero con el plazo cumplido ya no se puede confirmar:
// la carrera entre el pago y el vencimiento la gana el vencimiento (RN-14).
func (r Reserva) Confirmar(ahora time.Time) (cambia bool, err error) {
	switch r.Estado {
	case EstadoConfirmada:
		return false, nil
	case EstadoLiberada:
		return false, ErrReservaLiberada
	case EstadoVencida:
		return false, ErrReservaVencida
	}
	if !r.Vigente(ahora) {
		return false, ErrReservaVencida
	}
	return true, nil
}

// Liberar decide si la reserva se puede liberar.
// Liberar una reserva ya liberada o vencida no tiene efecto (idempotente);
// una reserva confirmada ya descontó el stock y no se puede liberar.
func (r Reserva) Liberar() (cambia bool, err error) {
	switch r.Estado {
	case EstadoActiva:
		return true, nil
	case EstadoConfirmada:
		return false, ErrReservaConfirmada
	default:
		return false, nil
	}
}

// DebeVencer indica si la reserva está activa y cumplió su plazo.
func (r Reserva) DebeVencer(ahora time.Time) bool {
	return r.Estado == EstadoActiva && !ahora.Before(r.VenceEn)
}
