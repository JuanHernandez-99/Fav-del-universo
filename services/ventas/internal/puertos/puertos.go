// Package puertos define lo que el núcleo de Ventas necesita del exterior. Los adaptadores lo implementan.
package puertos

import (
	"context"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/dominio"
)

// Repositorio expresa operaciones completas: el adaptador junta en una transacción el cambio de la
// venta, la tarea para Inventario y los mails, para que ninguno se pierda sin los otros.
type Repositorio interface {
	// CrearVenta guarda la venta con sus tareas y mails. Si la clave ya existe devuelve la guardada y repetida = true.
	CrearVenta(ctx context.Context, v dominio.Venta, tareas []dominio.TipoTarea, mails []dominio.Mail) (guardada dominio.Venta, repetida bool, err error)
	VentaPorClave(ctx context.Context, clave string) (dominio.Venta, error)
	ObtenerVenta(ctx context.Context, id string) (dominio.Venta, error)
	GuardarUrlPago(ctx context.Context, id, url string) error
	// ModificarVenta bloquea la venta y aplica cambio, que devuelve las tareas y mails a guardar junto con el estado nuevo.
	ModificarVenta(ctx context.Context, id string, cambio func(*dominio.Venta) ([]dominio.TipoTarea, []dominio.Mail, error)) (dominio.Venta, error)
	// RegistrarAvisoPago hace lo mismo que ModificarVenta, salvo que el pago ya se haya procesado:
	// el id de pago es único, así un aviso repetido no tiene efecto (HU-11.6).
	RegistrarAvisoPago(ctx context.Context, pagoID, ventaID string, cambio func(*dominio.Venta) ([]dominio.TipoTarea, []dominio.Mail, error)) error
	// ProcesarEvento aplica el cambio una sola vez por id de mensaje, en la misma transacción (ADR-005, punto 5).
	ProcesarEvento(ctx context.Context, mensajeID, ventaID string, cambio func(*dominio.Venta) ([]dominio.Mail, error)) (procesado bool, err error)

	ListarVentas(ctx context.Context, f FiltroVentas) ([]dominio.Venta, error)

	// TomarTareas aparta las tareas listas para este proceso, así dos réplicas no ejecutan la misma.
	TomarTareas(ctx context.Context, ahora time.Time, plazo time.Duration, limite int) ([]dominio.Tarea, error)
	CompletarTarea(ctx context.Context, id string) error
	ReprogramarTarea(ctx context.Context, id, motivo string, proximo time.Time) error

	TomarMails(ctx context.Context, ahora time.Time, plazo time.Duration, limite int) ([]dominio.Mail, error)
	MarcarMailEnviado(ctx context.Context, id string) error
	ReprogramarMail(ctx context.Context, id, motivo string, proximo time.Time) error
	MarcarMailFallido(ctx context.Context, id, motivo string) error

	Ping(ctx context.Context) error
}

// FiltroVentas: por estado para el panel (HU-16), por comprador para "Mis compras" (HU-14).
type FiltroVentas struct {
	Estado      dominio.Estado
	CompradorID string
	Limite      int
	Desde       int
}

// DetalleVariante es lo que Ventas necesita de Catálogo para vender una variante.
type DetalleVariante struct {
	VarianteID string
	Nombre     string
	Talle      string
	Color      string
	Publicado  bool
	Precio     int64 // centavos, con la promoción vigente para el medio de pago
}

type Catalogo interface {
	// Variantes devuelve dominio.ErrorVarianteInexistente si alguna no existe
	// y dominio.ErrCatalogoNoDisponible si no responde.
	Variantes(ctx context.Context, ids []string, medio dominio.Medio) ([]DetalleVariante, error)
}

// Reserva es la parte de la reserva de Inventario que le importa a Ventas.
type Reserva struct {
	ID       string
	Estado   string
	VenceEn  time.Time
	CreadaEn time.Time
}

const ReservaActiva = "ACTIVA"

type Inventario interface {
	// Reservar es todo o nada y usa una clave de idempotencia: un reintento devuelve la misma reserva.
	Reservar(ctx context.Context, clave, referencia string, lineas []dominio.LineaPedido, vigencia time.Duration) (Reserva, error)
	// Confirmar devuelve dominio.ErrReservaVencida o ErrReservaLiberada si ya no está vigente.
	Confirmar(ctx context.Context, reservaID string) error
	Liberar(ctx context.Context, reservaID string) error
	DescontarFisico(ctx context.Context, clave, referencia string, lineas []dominio.LineaPedido) error
}

// EstadoPago traduce los estados del proveedor de pagos.
type EstadoPago string

const (
	PagoAprobado  EstadoPago = "APROBADO"
	PagoRechazado EstadoPago = "RECHAZADO"
	PagoPendiente EstadoPago = "PENDIENTE"
)

type Pago struct {
	ID      string
	VentaID string
	Estado  EstadoPago
}

// ProveedorPagos es Mercado Pago detrás de un puerto. Hoy lo implementa un adaptador que
// habla con el simulador local (ADR-014).
type ProveedorPagos interface {
	// CrearPago devuelve la URL a la que se redirige al comprador, o dominio.ErrPagoNoDisponible.
	CrearPago(ctx context.Context, v dominio.Venta) (url string, err error)
	ConsultarPago(ctx context.Context, pagoID string) (Pago, error)
	// VerificarFirma comprueba que el aviso venga del proveedor.
	VerificarFirma(firma, pedidoID, pagoID string) bool
}

type Mailer interface {
	Enviar(ctx context.Context, m dominio.Mail) error
}
