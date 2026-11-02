package modelo

import (
	"errors"
	"fmt"
	"strings"
)

// Errores de negocio. El handler los traduce a códigos HTTP (PROYECTO.md sección 7).
var (
	ErrProductoNoEncontrado = errors.New("producto no encontrado")
	ErrVarianteDuplicada    = errors.New("ya existe una variante con ese id")
	// ErrIDProductoEnUso lo devuelve el repositorio cuando el _id ya existe; el servicio reintenta
	// con un sufijo numérico. No llega al cliente.
	ErrIDProductoEnUso = errors.New("el id de producto ya está en uso")
)

// ErrorValidacion lista todos los campos que no cumplen las reglas, no solo el primero.
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

func (e *ErrorValidacion) Agregar(campo, formato string, args ...any) {
	e.Campos = append(e.Campos, CampoInvalido{Campo: campo, Mensaje: fmt.Sprintf(formato, args...)})
}

// ComoError devuelve nil si no hubo problemas, para poder hacer `return v.ComoError()`.
func (e *ErrorValidacion) ComoError() error {
	if len(e.Campos) == 0 {
		return nil
	}
	return e
}
