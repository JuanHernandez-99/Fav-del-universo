package dominio

import (
	"errors"
	"fmt"
	"strings"
)

// Errores de negocio. Son respuestas definitivas: quien llama no debe reintentarlas (ADR-005).
var (
	ErrPedidoInvalido      = errors.New("pedido inválido")
	ErrVarianteInexistente = errors.New("variante inexistente")
	ErrStockInsuficiente   = errors.New("stock insuficiente")
	ErrClaveReutilizada    = errors.New("la clave de idempotencia ya se usó con otro pedido")
	ErrVentaNoEncontrada   = errors.New("venta no encontrada")
	ErrTransicionInvalida  = errors.New("la venta no admite ese cambio de estado")
	ErrNoAutenticado       = errors.New("hace falta iniciar sesión")
	ErrNoAutorizado        = errors.New("no autorizado")
	ErrFirmaInvalida       = errors.New("firma del aviso inválida")
)

// Errores de dependencias: la falla es transitoria y se puede reintentar con la misma clave.
var (
	ErrCatalogoNoDisponible   = errors.New("catálogo no disponible")
	ErrInventarioNoDisponible = errors.New("inventario no disponible")
	ErrPagoNoDisponible       = errors.New("mercado pago no disponible")
)

// Respuestas definitivas de Inventario sobre una reserva.
var (
	ErrReservaVencida      = errors.New("la reserva venció")
	ErrReservaLiberada     = errors.New("la reserva ya fue liberada")
	ErrReservaConfirmada   = errors.New("la reserva ya está confirmada")
	ErrReservaNoEncontrada = errors.New("reserva no encontrada")
)

// ErrorValidacion indica qué campos del pedido no cumplen las reglas.
type ErrorValidacion struct {
	Campos []CampoInvalido
}

type CampoInvalido struct {
	Campo   string `json:"campo"`
	Mensaje string `json:"mensaje"`
}

func (e *ErrorValidacion) Error() string {
	partes := make([]string, len(e.Campos))
	for i, c := range e.Campos {
		partes[i] = c.Campo + ": " + c.Mensaje
	}
	return "pedido inválido: " + strings.Join(partes, "; ")
}

func (e *ErrorValidacion) Is(objetivo error) bool { return objetivo == ErrPedidoInvalido }

func (e *ErrorValidacion) agregar(campo, formato string, args ...any) {
	e.Campos = append(e.Campos, CampoInvalido{Campo: campo, Mensaje: fmt.Sprintf(formato, args...)})
}

func (e *ErrorValidacion) comoError() error {
	if len(e.Campos) == 0 {
		return nil
	}
	return e
}

// Faltante es una variante que no alcanza. El contrato público informa solo qué variante y cuánto
// se pidió, no cuánto stock hay.
type Faltante struct {
	VarianteID string `json:"varianteId"`
	Solicitada int    `json:"solicitada"`
}

type ErrorStockInsuficiente struct {
	Faltantes []Faltante
}

func (e *ErrorStockInsuficiente) Error() string {
	return fmt.Sprintf("stock insuficiente en %d variante(s)", len(e.Faltantes))
}

func (e *ErrorStockInsuficiente) Is(objetivo error) bool { return objetivo == ErrStockInsuficiente }

// ErrorVarianteInexistente lista las variantes que no existen o no están publicadas.
type ErrorVarianteInexistente struct {
	Variantes []string
}

func (e *ErrorVarianteInexistente) Error() string {
	return "variantes inexistentes o no publicadas: " + strings.Join(e.Variantes, ", ")
}

func (e *ErrorVarianteInexistente) Is(objetivo error) bool { return objetivo == ErrVarianteInexistente }
