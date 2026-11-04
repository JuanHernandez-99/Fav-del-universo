// Package service tiene las reglas de Usuarios. No conoce HTTP ni MySQL.
package service

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/JuanHernandez-99/Fav-del-universo/services/usuarios/internal/modelo"
)

const (
	// LargoMinimoContrasena es [propuesta] en SPEC RN-26.
	LargoMinimoContrasena = 8
	// largoMaximoContrasena es el límite de bcrypt, en bytes: más allá, no podría calcular el hash.
	largoMaximoContrasena = 72
	costoBcrypt           = 12
)

// Repositorio es lo que el servicio necesita de la base. El email único lo garantiza la base.
type Repositorio interface {
	// Insertar devuelve modelo.ErrEmailEnUso si el email ya existe.
	Insertar(ctx context.Context, u modelo.Usuario) error
	// PorEmail devuelve modelo.ErrUsuarioNoEncontrado si no existe.
	PorEmail(ctx context.Context, email string) (modelo.Usuario, error)
	Ping(ctx context.Context) error
}

type Servicio struct {
	repo      Repositorio
	clave     *rsa.PrivateKey
	duracion  time.Duration
	ahora     func() time.Time
	hashFalso []byte
}

func Nuevo(repo Repositorio, clave *rsa.PrivateKey, duracion time.Duration) (*Servicio, error) {
	// Un hash que no corresponde a nadie, para que un email inexistente tarde lo mismo que uno existente.
	falso, err := bcrypt.GenerateFromPassword([]byte(uuid.NewString()), costoBcrypt)
	if err != nil {
		return nil, err
	}
	return &Servicio{repo, clave, duracion, time.Now, falso}, nil
}

// Registrar crea un comprador. El rol no se elige: la cuenta de administrador nace con AsegurarAdministrador (ADR-015).
func (s *Servicio) Registrar(ctx context.Context, email, contrasena string) (modelo.Usuario, error) {
	email = normalizarEmail(email)
	if err := validar(email, contrasena); err != nil {
		return modelo.Usuario{}, err
	}
	u, err := s.crear(ctx, email, contrasena, modelo.RolComprador)
	if err == nil {
		slog.InfoContext(ctx, "usuario registrado", "usuario_id", u.ID)
	}
	return u, err
}

// IniciarSesion devuelve un token con vencimiento (HU-18.3). Siempre calcula un hash, también si el
// email no existe: la respuesta no revela si existe, ni por el mensaje ni por lo que tarda.
func (s *Servicio) IniciarSesion(ctx context.Context, email, contrasena string) (modelo.Sesion, error) {
	u, err := s.repo.PorEmail(ctx, normalizarEmail(email))
	if errors.Is(err, modelo.ErrUsuarioNoEncontrado) {
		_ = bcrypt.CompareHashAndPassword(s.hashFalso, []byte(contrasena))
		return modelo.Sesion{}, modelo.ErrCredenciales
	}
	if err != nil {
		return modelo.Sesion{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.Hash), []byte(contrasena)) != nil {
		return modelo.Sesion{}, modelo.ErrCredenciales
	}
	return s.emitirToken(u)
}

// claims son los de ADR-001: sub, email, role, iat y exp.
type claims struct {
	Email string `json:"email"`
	Role  string `json:"role"`
	jwt.RegisteredClaims
}

// emitirToken firma con la clave privada, que solo tiene este servicio. El gateway verifica con la pública.
func (s *Servicio) emitirToken(u modelo.Usuario) (modelo.Sesion, error) {
	emitido := s.ahora().UTC().Truncate(time.Second)
	vence := emitido.Add(s.duracion)
	token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims{
		Email: u.Email,
		Role:  u.Rol,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   u.ID,
			IssuedAt:  jwt.NewNumericDate(emitido),
			ExpiresAt: jwt.NewNumericDate(vence),
		},
	}).SignedString(s.clave)
	if err != nil {
		return modelo.Sesion{}, fmt.Errorf("firmando el token: %w", err)
	}
	return modelo.Sesion{Token: token, VenceEn: vence}, nil
}

// AsegurarAdministrador crea la única cuenta de administrador (RN-23) si no existe. Es idempotente, así
// que varias réplicas pueden arrancar a la vez. Si ya existe no la toca: cambiar la variable no cambia la
// contraseña guardada. Tampoco convierte en administrador a un comprador que ya use ese email.
func (s *Servicio) AsegurarAdministrador(ctx context.Context, email, contrasena string) error {
	email = normalizarEmail(email)
	if err := validar(email, contrasena); err != nil {
		return fmt.Errorf("cuenta de administrador: %w", err)
	}
	u, err := s.crear(ctx, email, contrasena, modelo.RolAdministrador)
	if errors.Is(err, modelo.ErrEmailEnUso) {
		existente, errLeer := s.repo.PorEmail(ctx, email)
		if errLeer != nil {
			return errLeer
		}
		if existente.Rol != modelo.RolAdministrador {
			return fmt.Errorf("cuenta de administrador: el email %s ya está registrado como comprador", email)
		}
		return nil
	}
	if err == nil {
		slog.InfoContext(ctx, "cuenta de administrador creada", "usuario_id", u.ID)
	}
	return err
}

func (s *Servicio) crear(ctx context.Context, email, contrasena, rol string) (modelo.Usuario, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(contrasena), costoBcrypt)
	if err != nil {
		return modelo.Usuario{}, err
	}
	u := modelo.Usuario{ID: uuid.NewString(), Email: email, Hash: string(hash), Rol: rol, CreadoEn: s.ahora().UTC().Truncate(time.Second)}
	if err := s.repo.Insertar(ctx, u); err != nil {
		return modelo.Usuario{}, err
	}
	return u, nil
}

func (s *Servicio) Listo(ctx context.Context) error { return s.repo.Ping(ctx) }

// normalizarEmail evita dos cuentas para Ana@example.com y ana@example.com.
func normalizarEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

func validar(email, contrasena string) error {
	v := &modelo.ErrorValidacion{}
	if direccion, err := mail.ParseAddress(email); err != nil || direccion.Address != email || len(email) > 254 {
		v.Agregar("email", "tiene que ser un email válido")
	}
	if len(contrasena) < LargoMinimoContrasena {
		v.Agregar("contrasena", fmt.Sprintf("tiene que tener al menos %d caracteres", LargoMinimoContrasena))
	} else if len(contrasena) > largoMaximoContrasena {
		v.Agregar("contrasena", fmt.Sprintf("no puede superar los %d bytes", largoMaximoContrasena))
	}
	return v.ComoError()
}
