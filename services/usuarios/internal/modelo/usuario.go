// Package modelo tiene los tipos que comparten las capas de Usuarios.
package modelo

import (
	"errors"
	"strings"
	"time"
)

const (
	RolComprador     = "comprador"
	RolAdministrador = "administrador"
)

// Usuario guarda el hash, nunca la contraseña (HU-18.2).
type Usuario struct {
	ID       string
	Email    string
	Hash     string
	Rol      string
	CreadoEn time.Time
}

// Sesion es el resultado de iniciar sesión: el token y cuándo vence.
type Sesion struct {
	Token   string
	VenceEn time.Time
}

// Errores de negocio. El handler los traduce a códigos HTTP.
var (
	ErrEmailEnUso = errors.New("el email ya está registrado")
	// ErrCredenciales es el mismo para un email inexistente y una contraseña incorrecta: no revela
	// si el email existe (HU-18.3).
	ErrCredenciales = errors.New("email o contraseña incorrectos")
	// ErrUsuarioNoEncontrado no llega al cliente: el servicio lo convierte en ErrCredenciales.
	ErrUsuarioNoEncontrado = errors.New("usuario no encontrado")
)

// ErrorValidacion lista todos los campos que no cumplen las reglas.
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

func (e *ErrorValidacion) Agregar(campo, mensaje string) {
	e.Campos = append(e.Campos, CampoInvalido{Campo: campo, Mensaje: mensaje})
}

func (e *ErrorValidacion) ComoError() error {
	if len(e.Campos) == 0 {
		return nil
	}
	return e
}
