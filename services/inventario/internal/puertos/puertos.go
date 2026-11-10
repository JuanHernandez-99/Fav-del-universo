// Package puertos define lo que el núcleo necesita del exterior. Los adaptadores lo implementan.
package puertos

import (
	"context"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/dominio"
)

// Repositorio expresa operaciones completas ("reservar si alcanza"), no lecturas y escrituras sueltas:
// el adaptador las hace atómicas en la base, que es quien resuelve la carrera entre dos operaciones
// sobre las mismas unidades (ARCHITECTURE §4).
type Repositorio interface {
	// CrearReserva aparta todas las unidades o ninguna. Si la clave de idempotencia ya existe,
	// devuelve la reserva guardada y repetida = true, sin apartar nada.
	CrearReserva(ctx context.Context, r dominio.Reserva) (guardada dominio.Reserva, repetida bool, err error)
	ObtenerReserva(ctx context.Context, id string) (dominio.Reserva, error)
	// ModificarReserva bloquea la reserva, aplica la transición y mueve el stock según el cambio de estado.
	ModificarReserva(ctx context.Context, id string, transicion func(*dominio.Reserva) error) (dominio.Reserva, error)
	ReservasVencidas(ctx context.Context, ahora time.Time, limite int) ([]string, error)

	ConsultarStock(ctx context.Context, varianteIDs []string) ([]dominio.Stock, error)
	// StockACorregir lista hasta limite variantes con físico negativo, primero las más negativas (RN-25).
	StockACorregir(ctx context.Context, limite int) ([]dominio.Stock, error)
	Ingresar(ctx context.Context, i dominio.Ingreso) (dominio.Stock, error)
	Transferir(ctx context.Context, t dominio.Transferencia) (dominio.Stock, error)
	// Ajustar suma o resta unidades con un motivo. Al restar, rechaza si el online quedaría por debajo de lo
	// reservado o el físico negativo.
	Ajustar(ctx context.Context, a dominio.Ajuste) (dominio.Stock, error)
	// DescontarFisico aplica una venta del local, una sola vez por clave. Si la variante no tiene
	// fila de stock la crea: la venta del local nunca se bloquea (RN-06).
	DescontarFisico(ctx context.Context, d dominio.DescuentoFisico) ([]dominio.Stock, error)

	// PublicarEventos toma eventos sin publicar, llama a publicar por cada uno y los marca en la misma
	// transacción. Si el proceso se cae entre publicar y marcar, el evento se publica de nuevo:
	// la entrega es al menos una vez y el consumidor es idempotente (ADR-005, punto 5).
	PublicarEventos(ctx context.Context, limite int, publicar func(context.Context, dominio.Evento) error) (int, error)

	// CrearReposicion descuenta el stock físico y guarda la reposición pendiente en la misma transacción.
	CrearReposicion(ctx context.Context, r dominio.Reposicion) (guardada dominio.Reposicion, repetida bool, err error)
	// TomarReposiciones reserva para este proceso las reposiciones listas para enviar,
	// así dos réplicas no envían la misma a la vez.
	TomarReposiciones(ctx context.Context, ahora time.Time, plazo time.Duration, limite int) ([]dominio.Reposicion, error)
	// ConfirmarReposicion guarda el id que generó el gimnasio.
	ConfirmarReposicion(ctx context.Context, id, idGimnasio string, ahora time.Time) error
	// RechazarReposicion devuelve las unidades al stock físico.
	RechazarReposicion(ctx context.Context, id, motivo string, ahora time.Time) error
	ReprogramarReposicion(ctx context.Context, id, motivo string, proximo time.Time) error
	// MarcarReposicionARevisar la saca de los reintentos, sin devolver las unidades.
	MarcarReposicionARevisar(ctx context.Context, id, motivo string, ahora time.Time) error
	// BloquearReposicion la deja reintentándose espaciado, sin devolver las unidades.
	BloquearReposicion(ctx context.Context, id, motivo string, proximo time.Time) error
	// EsFallaDeConexion dice si err significa que no se pudo usar la base, a diferencia de una consulta que está mal.
	EsFallaDeConexion(err error) bool
	// ResumirReposiciones cuenta por estado y da la fecha de la más antigua que falta confirmar (cero si no hay).
	ResumirReposiciones(ctx context.Context) (map[dominio.EstadoReposicion]int, time.Time, error)
	// ListarReposiciones devuelve las más recientes primero.
	ListarReposiciones(ctx context.Context, limite int) ([]dominio.Reposicion, error)

	Ping(ctx context.Context) error
}

// PublicadorEventos envía un evento al broker. Devuelve nil solo si el broker ya lo guardó.
type PublicadorEventos interface {
	Publicar(ctx context.Context, e dominio.Evento) error
}

// Gimnasio es la capacidad consumida (ADR-009). El adaptador traduce su modelo al de FAV.
type Gimnasio interface {
	// EnviarReposicion devuelve el id que generó el gimnasio. El error dice qué hacer con la reposición:
	// dominio.ErrReposicionRechazada (no se registró), dominio.ErrReposicionConflicto (409) o
	// dominio.ErrGimnasioConfiguracion (401 o 403). Cualquier otro es un resultado incierto: se reintenta igual.
	EnviarReposicion(ctx context.Context, r dominio.Reposicion) (string, error)
	// ConsultarStock devuelve dominio.ErrProveedorNoDisponible si el gimnasio no responde.
	ConsultarStock(ctx context.Context) ([]dominio.StockConsignacion, error)
}
