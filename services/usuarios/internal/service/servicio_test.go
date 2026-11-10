package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/JuanHernandez-99/Fav-del-universo/services/usuarios/internal/modelo"
)

type repoFalso struct {
	porEmail map[string]modelo.Usuario
}

func (r *repoFalso) Insertar(_ context.Context, u modelo.Usuario) error {
	if _, existe := r.porEmail[u.Email]; existe {
		return modelo.ErrEmailEnUso
	}
	r.porEmail[u.Email] = u
	return nil
}

func (r *repoFalso) PorEmail(_ context.Context, email string) (modelo.Usuario, error) {
	u, ok := r.porEmail[email]
	if !ok {
		return modelo.Usuario{}, modelo.ErrUsuarioNoEncontrado
	}
	return u, nil
}

func (r *repoFalso) Ping(context.Context) error { return nil }

// Una sola clave y un costo real serían lentos en cada test: la clave se genera una vez.
var claveDePrueba = func() *rsa.PrivateKey {
	clave, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return clave
}()

func armar(t *testing.T) (*Servicio, *repoFalso) {
	t.Helper()
	repo := &repoFalso{porEmail: map[string]modelo.Usuario{}}
	s, err := Nuevo(repo, claveDePrueba, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return s, repo
}

// HU-18.2: en la base queda un hash que verifica la contraseña, nunca la contraseña.
func TestRegistrarGuardaUnHashYNoLaContrasena(t *testing.T) {
	s, repo := armar(t)
	u, err := s.Registrar(context.Background(), "ana@example.com", "remera-blanca")
	if err != nil {
		t.Fatal(err)
	}
	guardado := repo.porEmail["ana@example.com"]
	if strings.Contains(guardado.Hash, "remera-blanca") {
		t.Error("la contraseña no puede quedar en la base")
	}
	if bcrypt.CompareHashAndPassword([]byte(guardado.Hash), []byte("remera-blanca")) != nil {
		t.Error("el hash tiene que verificar la contraseña")
	}
	if costo, _ := bcrypt.Cost([]byte(guardado.Hash)); costo != costoBcrypt {
		t.Errorf("costo de bcrypt: %d", costo)
	}
	if u.Rol != modelo.RolComprador {
		t.Errorf("el registro público siempre da comprador: %s", u.Rol)
	}
}

func TestEmailRepetidoSeRechazaAunqueCambienLasMayusculas(t *testing.T) {
	s, _ := armar(t)
	if _, err := s.Registrar(context.Background(), "ana@example.com", "remera-blanca"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Registrar(context.Background(), " ANA@example.com ", "otra-contrasena"); !errors.Is(err, modelo.ErrEmailEnUso) {
		t.Errorf("HU-18.1: %v", err)
	}
}

func TestDatosInvalidos(t *testing.T) {
	casos := map[string]struct{ email, contrasena, campo string }{
		"email sin arroba":  {"ana", "remera-blanca", "email"},
		"email vacío":       {"", "remera-blanca", "email"},
		"con nombre":        {"Ana <ana@example.com>", "remera-blanca", "email"},
		"contraseña corta":  {"ana@example.com", "1234567", "contrasena"},
		"contraseña de más": {"ana@example.com", strings.Repeat("x", 73), "contrasena"},
	}
	for nombre, c := range casos {
		s, repo := armar(t)
		_, err := s.Registrar(context.Background(), c.email, c.contrasena)
		var v *modelo.ErrorValidacion
		if !errors.As(err, &v) || v.Campos[0].Campo != c.campo {
			t.Errorf("%s: %v", nombre, err)
		}
		if len(repo.porEmail) != 0 {
			t.Errorf("%s: no debía guardarse", nombre)
		}
	}
	s, _ := armar(t)
	if _, err := s.Registrar(context.Background(), "ana@example.com", "12345678"); err != nil {
		t.Errorf("8 caracteres alcanzan (RN-26): %v", err)
	}
}

// HU-18.3: el token lleva los claims de ADR-001, firmados con RS256, y vence en una hora.
func TestIniciarSesionEmiteElToken(t *testing.T) {
	s, _ := armar(t)
	u, err := s.Registrar(context.Background(), "ana@example.com", "remera-blanca")
	if err != nil {
		t.Fatal(err)
	}
	momento := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	s.ahora = func() time.Time { return momento }
	sesion, err := s.IniciarSesion(context.Background(), "ANA@example.com", "remera-blanca")
	if err != nil {
		t.Fatal(err)
	}
	if !sesion.VenceEn.Equal(momento.Add(time.Hour)) {
		t.Errorf("vence: %v", sesion.VenceEn)
	}

	leidos := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(sesion.Token, leidos, func(*jwt.Token) (any, error) { return &claveDePrueba.PublicKey, nil },
		jwt.WithValidMethods([]string{"RS256"}), jwt.WithoutClaimsValidation())
	if err != nil || !token.Valid {
		t.Fatalf("la clave pública tiene que verificarlo: %v", err)
	}
	if leidos["sub"] != u.ID || leidos["email"] != "ana@example.com" || leidos["role"] != modelo.RolComprador {
		t.Errorf("claims: %v", leidos)
	}
	if leidos["iat"] != float64(momento.Unix()) || leidos["exp"] != float64(momento.Add(time.Hour).Unix()) {
		t.Errorf("fechas: %v", leidos)
	}
	if len(leidos) != 5 {
		t.Errorf("solo los claims de ADR-001: %v", leidos)
	}
}

// HU-18.3: email inexistente y contraseña incorrecta dan exactamente el mismo error.
func TestCredencialesIncorrectasDanElMismoError(t *testing.T) {
	s, _ := armar(t)
	if _, err := s.Registrar(context.Background(), "ana@example.com", "remera-blanca"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.IniciarSesion(context.Background(), "ana@example.com", "otra-cosa"); err != modelo.ErrCredenciales {
		t.Errorf("contraseña incorrecta: %v", err)
	}
	if _, err := s.IniciarSesion(context.Background(), "nadie@example.com", "otra-cosa"); err != modelo.ErrCredenciales {
		t.Errorf("email inexistente: %v", err)
	}
}

func TestAdministradorSeCreaUnaSolaVez(t *testing.T) {
	s, repo := armar(t)
	if err := s.AsegurarAdministrador(context.Background(), "admin@fav.example", "clave-del-admin"); err != nil {
		t.Fatal(err)
	}
	if repo.porEmail["admin@fav.example"].Rol != modelo.RolAdministrador {
		t.Errorf("rol: %s", repo.porEmail["admin@fav.example"].Rol)
	}
	hash := repo.porEmail["admin@fav.example"].Hash

	if err := s.AsegurarAdministrador(context.Background(), "admin@fav.example", "otra-clave-nueva"); err != nil {
		t.Fatalf("el segundo arranque no es un error: %v", err)
	}
	if repo.porEmail["admin@fav.example"].Hash != hash {
		t.Error("si ya existe no se toca su contraseña")
	}
}

// Nadie se vuelve administrador registrándose antes con ese email.
func TestNoConvierteEnAdministradorAUnComprador(t *testing.T) {
	s, repo := armar(t)
	if _, err := s.Registrar(context.Background(), "admin@fav.example", "soy-un-comprador"); err != nil {
		t.Fatal(err)
	}
	if err := s.AsegurarAdministrador(context.Background(), "admin@fav.example", "clave-del-admin"); err == nil {
		t.Error("tiene que avisar del conflicto")
	}
	if repo.porEmail["admin@fav.example"].Rol != modelo.RolComprador {
		t.Error("no puede cambiarle el rol")
	}
}

func TestAdministradorConContrasenaInvalidaFalla(t *testing.T) {
	s, repo := armar(t)
	if err := s.AsegurarAdministrador(context.Background(), "admin@fav.example", "corta"); err == nil {
		t.Error("tiene que fallar")
	}
	if len(repo.porEmail) != 0 {
		t.Error("no debía crearse")
	}
}
