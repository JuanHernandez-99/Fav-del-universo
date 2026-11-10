package dominio

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Límites de una reserva. La capacidad publicada pide como máximo 20 variantes por pedido (ADR-008);
// las reservas de la tienda online usan los mismos límites.
const (
	MaxItemsPorReserva = 20
	VigenciaMinima     = time.Minute
	VigenciaMaxima     = 72 * time.Hour
)

type EstadoReserva string

const (
	ReservaActiva     EstadoReserva = "ACTIVA"
	ReservaConfirmada EstadoReserva = "CONFIRMADA"
	ReservaLiberada   EstadoReserva = "LIBERADA"
	ReservaVencida    EstadoReserva = "VENCIDA"
)

// Item es una variante y la cantidad que se aparta de su stock online.
type Item struct {
	VarianteID string
	Cantidad   int
}

// Reserva aparta stock online para una compra hasta VenceEn. Inventario no conoce la venta:
// solo guarda la referencia que envía quien reserva (ADR-005).
type Reserva struct {
	ID                string
	ClaveIdempotencia string
	Huella            string // resumen del pedido, para detectar una clave reutilizada con otro cuerpo
	Referencia        string
	Estado            EstadoReserva
	Items             []Item
	VenceEn           time.Time
	CreadaEn          time.Time
	ActualizadaEn     time.Time
}

// NuevaReserva valida el pedido y arma la reserva activa. Los ítems quedan ordenados por variante:
// así todas las transacciones bloquean las filas de stock en el mismo orden y no se cruzan.
func NuevaReserva(id, clave, referencia string, items []Item, vigencia time.Duration, ahora time.Time) (Reserva, error) {
	var ev ErrorValidacion
	validarClave(&ev, clave)
	if len(referencia) > 100 {
		ev.agregar("referencia", "no puede superar los 100 caracteres")
	}
	if vigencia < VigenciaMinima || vigencia > VigenciaMaxima {
		ev.agregar("vigenciaMinutos", "debe estar entre %d y %d", int(VigenciaMinima.Minutes()), int(VigenciaMaxima.Minutes()))
	}
	ordenados := validarItems(&ev, items)
	if err := ev.comoError(); err != nil {
		return Reserva{}, err
	}

	return Reserva{
		ID:                id,
		ClaveIdempotencia: clave,
		Huella:            huellaReserva(referencia, ordenados, vigencia),
		Referencia:        referencia,
		Estado:            ReservaActiva,
		Items:             ordenados,
		VenceEn:           ahora.Add(vigencia).UTC().Truncate(time.Second),
		CreadaEn:          ahora.UTC().Truncate(time.Second),
		ActualizadaEn:     ahora.UTC().Truncate(time.Second),
	}, nil
}

func validarItems(ev *ErrorValidacion, items []Item) []Item {
	if len(items) == 0 || len(items) > MaxItemsPorReserva {
		ev.agregar("items", "debe tener entre 1 y %d variantes", MaxItemsPorReserva)
	}
	vistos := make(map[string]bool, len(items))
	for i, it := range items {
		campo := fmt.Sprintf("items[%d]", i)
		if strings.TrimSpace(it.VarianteID) == "" || len(it.VarianteID) > 100 {
			ev.agregar(campo+".varianteId", "es obligatorio y de hasta 100 caracteres")
		}
		if it.Cantidad < 1 {
			ev.agregar(campo+".cantidad", "debe ser mayor a 0")
		}
		if vistos[it.VarianteID] {
			ev.agregar(campo+".varianteId", "la variante %s está repetida", it.VarianteID)
		}
		vistos[it.VarianteID] = true
	}
	ordenados := append([]Item(nil), items...)
	sort.Slice(ordenados, func(a, b int) bool { return ordenados[a].VarianteID < ordenados[b].VarianteID })
	return ordenados
}

func validarClave(ev *ErrorValidacion, clave string) {
	if len(clave) < 8 || len(clave) > 100 {
		ev.agregar("Idempotency-Key", "es obligatoria y debe tener entre 8 y 100 caracteres")
	}
}

func huellaReserva(referencia string, items []Item, vigencia time.Duration) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s|%d", referencia, int(vigencia.Seconds()))
	for _, it := range items {
		fmt.Fprintf(&b, "|%s:%d", it.VarianteID, it.Cantidad)
	}
	suma := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(suma[:])
}

// Las transiciones modifican la reserva y devuelven un error de negocio si no corresponde.
// El adaptador de persistencia compara el estado antes y después para saber qué stock mover,
// y guarda el cambio aunque haya error (por ejemplo, una reserva que vence al intentar confirmarla).

// Confirmar descuenta definitivamente el stock al aprobarse el pago. Solo si la reserva no venció:
// Inventario es quien decide la carrera entre el pago y el vencimiento (ADR-005, punto 4).
func (r *Reserva) Confirmar(ahora time.Time) error {
	switch r.Estado {
	case ReservaConfirmada:
		return nil // reintento: mismo resultado
	case ReservaLiberada:
		return ErrReservaYaLiberada
	case ReservaVencida:
		return ErrReservaVencida
	}
	if !ahora.Before(r.VenceEn) {
		r.cambiar(ReservaVencida, ahora)
		return ErrReservaVencida
	}
	r.cambiar(ReservaConfirmada, ahora)
	return nil
}

// Liberar devuelve las unidades al stock online (compra cancelada o pago rechazado).
func (r *Reserva) Liberar(ahora time.Time) error {
	switch r.Estado {
	case ReservaLiberada, ReservaVencida:
		return nil // el stock ya está libre
	case ReservaConfirmada:
		return ErrReservaYaConfirmada
	}
	r.cambiar(ReservaLiberada, ahora)
	return nil
}

// Vencer libera la reserva si pasó su plazo. Lo usa el proceso de vencimiento.
func (r *Reserva) Vencer(ahora time.Time) bool {
	if r.Estado != ReservaActiva || ahora.Before(r.VenceEn) {
		return false
	}
	r.cambiar(ReservaVencida, ahora)
	return true
}

func (r *Reserva) cambiar(estado EstadoReserva, ahora time.Time) {
	r.Estado = estado
	r.ActualizadaEn = ahora.UTC().Truncate(time.Second)
}
