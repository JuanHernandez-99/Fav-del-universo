// Package puertos declara lo que el núcleo de Inventario necesita del exterior.
// Los adaptadores (MySQL, reloj, generador de ids) implementan estas interfaces;
// el núcleo no depende de ellos.
package puertos

import (
	"context"
	"errors"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/dominio"
)

// ErrEstadoCambio: la reserva cambió de estado entre que se leyó y se intentó
// modificar (por ejemplo, venció justo antes de confirmarla). El caso de uso
// la vuelve a leer y decide con las reglas del dominio.
var ErrEstadoCambio = errors.New("la reserva cambió de estado")

// RepositorioInventario guarda el stock y las reservas.
//
// Las operaciones que modifican stock son atómicas: cada una se aplica completa
// o no se aplica, aunque haya otras operaciones concurrentes sobre las mismas
// variantes. Esa garantía la da la base (transacción y actualización condicional),
// no el núcleo, porque es la única que ve todas las operaciones a la vez.
type RepositorioInventario interface {
	// GuardarReservaSiAlcanza aparta el stock online de todos los ítems y guarda la
	// reserva, o no hace nada. Errores: *dominio.ErrStockInsuficiente,
	// *dominio.ErrVarianteInexistente, dominio.ErrClaveReutilizada.
	// Si la clave ya se usó con la misma huella, devuelve la reserva original.
	GuardarReservaSiAlcanza(ctx context.Context, r dominio.Reserva, clave, huella string) (dominio.Reserva, error)

	// ObtenerReserva devuelve dominio.ErrReservaInexistente si no existe.
	ObtenerReserva(ctx context.Context, id string) (dominio.Reserva, error)

	// ConfirmarSiVigente pasa la reserva a CONFIRMADA y descuenta el stock en forma
	// definitiva, solo si sigue ACTIVA y sin vencer. Si no, devuelve ErrEstadoCambio.
	ConfirmarSiVigente(ctx context.Context, id string, ahora time.Time) error

	// LiberarSiActiva pasa la reserva a LIBERADA y devuelve las unidades,
	// solo si sigue ACTIVA. Si no, devuelve ErrEstadoCambio.
	LiberarSiActiva(ctx context.Context, id string) error

	// VencerSiCorresponde pasa la reserva a VENCIDA, devuelve las unidades y guarda
	// el evento en el outbox, todo en una transacción y solo si sigue ACTIVA con el
	// plazo cumplido. Devuelve false si ya no correspondía (y no guarda el evento).
	VencerSiCorresponde(ctx context.Context, id string, ahora time.Time, evento dominio.Evento) (bool, error)

	// ReservasParaVencer devuelve ids de reservas activas con el plazo cumplido.
	ReservasParaVencer(ctx context.Context, ahora time.Time, limite int) ([]string, error)

	// ConsultarStock devuelve el stock de las variantes pedidas en los dos canales.
	ConsultarStock(ctx context.Context, varianteIDs []string) ([]dominio.Stock, error)

	// Disponible indica si la base responde (para /health).
	Disponible(ctx context.Context) error
}

// MensajeOutbox es un evento guardado en el outbox, ya serializado y listo para publicar.
type MensajeOutbox struct {
	ID            string
	Tipo          string
	CorrelacionID string
	Cuerpo        []byte
}

// Outbox da los eventos guardados que falta publicar (ADR-005 §5).
type Outbox interface {
	// Pendientes devuelve los eventos sin publicar, del más viejo al más nuevo.
	Pendientes(ctx context.Context, limite int) ([]MensajeOutbox, error)
	MarcarPublicado(ctx context.Context, id string, ahora time.Time) error
}

// PublicadorEventos entrega un evento al broker. Devuelve nil solo si el broker
// lo recibió y lo guardó.
type PublicadorEventos interface {
	Publicar(ctx context.Context, m MensajeOutbox) error
}

// Reloj da la hora actual. En los tests se reemplaza por uno fijo.
type Reloj interface {
	Ahora() time.Time
}

// GeneradorID crea identificadores únicos para las reservas y los eventos.
type GeneradorID interface {
	Nuevo() string
}
