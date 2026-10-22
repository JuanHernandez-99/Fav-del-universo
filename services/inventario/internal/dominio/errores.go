package dominio

import (
	"errors"
	"fmt"
	"strings"
)

// Errores de negocio. Son respuestas definitivas: quien llama no debe reintentarlas (ADR-005).
var (
	ErrPedidoInvalido        = errors.New("pedido inválido")
	ErrVarianteInexistente   = errors.New("variante inexistente")
	ErrStockInsuficiente     = errors.New("stock insuficiente")
	ErrClaveReutilizada      = errors.New("la clave de idempotencia ya se usó con otro pedido")
	ErrReservaNoEncontrada   = errors.New("reserva no encontrada")
	ErrReservaVencida        = errors.New("la reserva venció")
	ErrReservaYaConfirmada   = errors.New("la reserva ya está confirmada")
	ErrReservaYaLiberada     = errors.New("la reserva ya fue liberada")
	ErrReposicionRechazada   = errors.New("el gimnasio rechazó la reposición")
	ErrProveedorNoDisponible = errors.New("el gimnasio no está disponible")
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

// Faltante es una variante que no alcanza para una reserva o una transferencia.
type Faltante struct {
	VarianteID string `json:"varianteId"`
	Solicitada int    `json:"solicitada"`
	Disponible int    `json:"disponible"`
}

// ErrorStockInsuficiente lista todas las variantes que no alcanzan, no solo la primera.
type ErrorStockInsuficiente struct {
	Faltantes []Faltante
}

func (e *ErrorStockInsuficiente) Error() string {
	return fmt.Sprintf("stock insuficiente en %d variante(s)", len(e.Faltantes))
}

func (e *ErrorStockInsuficiente) Is(objetivo error) bool { return objetivo == ErrStockInsuficiente }

// ErrorVarianteInexistente lista las variantes que Inventario no conoce.
type ErrorVarianteInexistente struct {
	Variantes []string
}

func (e *ErrorVarianteInexistente) Error() string {
	return "variantes inexistentes: " + strings.Join(e.Variantes, ", ")
}

func (e *ErrorVarianteInexistente) Is(objetivo error) bool { return objetivo == ErrVarianteInexistente }
