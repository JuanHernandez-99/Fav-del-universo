package dominio

import (
	"errors"
	"fmt"
	"strings"
)

// Errores de las reglas del negocio. Los adaptadores los traducen a su protocolo
// (por ejemplo, a códigos HTTP); el dominio no sabe nada de HTTP ni de SQL.
var (
	ErrReservaInexistente = errors.New("la reserva no existe")
	ErrReservaVencida     = errors.New("la reserva está vencida")
	ErrReservaLiberada    = errors.New("la reserva fue liberada")
	ErrReservaConfirmada  = errors.New("la reserva ya fue confirmada")
	ErrClaveReutilizada   = errors.New("la clave de idempotencia ya se usó con otro pedido")
)

// CampoInvalido indica qué dato del pedido no cumple las reglas.
type CampoInvalido struct {
	Campo   string `json:"campo"`
	Mensaje string `json:"mensaje"`
}

// ErrorValidacion agrupa todos los problemas de un pedido, para informarlos juntos.
type ErrorValidacion struct {
	Errores []CampoInvalido
}

func (e *ErrorValidacion) Error() string {
	partes := make([]string, len(e.Errores))
	for i, c := range e.Errores {
		partes[i] = c.Campo + ": " + c.Mensaje
	}
	return "pedido inválido: " + strings.Join(partes, "; ")
}

func (e *ErrorValidacion) agregar(campo, mensaje string) {
	e.Errores = append(e.Errores, CampoInvalido{Campo: campo, Mensaje: mensaje})
}

func (e *ErrorValidacion) vacio() bool { return len(e.Errores) == 0 }

// Faltante es una variante que no alcanza para la cantidad pedida.
type Faltante struct {
	VarianteID string `json:"varianteId"`
	Solicitada int    `json:"solicitada"`
}

// ErrStockInsuficiente: al menos una variante no alcanza. No se reserva nada (todo o nada).
type ErrStockInsuficiente struct {
	Faltantes []Faltante
}

func (e *ErrStockInsuficiente) Error() string {
	return fmt.Sprintf("stock insuficiente en %d variante(s)", len(e.Faltantes))
}

// ErrVarianteInexistente: Inventario no tiene registrada la variante.
type ErrVarianteInexistente struct {
	IDs []string
}

func (e *ErrVarianteInexistente) Error() string {
	return "variantes inexistentes: " + strings.Join(e.IDs, ", ")
}
