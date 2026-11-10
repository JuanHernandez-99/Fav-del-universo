package handler

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/usuarios/internal/modelo"
	"github.com/JuanHernandez-99/Fav-del-universo/services/usuarios/internal/service"
)

type repoFalso struct{ guardados map[string]modelo.Usuario }

func (r repoFalso) Insertar(_ context.Context, u modelo.Usuario) error {
	if _, existe := r.guardados[u.Email]; existe {
		return modelo.ErrEmailEnUso
	}
	r.guardados[u.Email] = u
	return nil
}

func (r repoFalso) PorEmail(_ context.Context, email string) (modelo.Usuario, error) {
	if u, ok := r.guardados[email]; ok {
		return u, nil
	}
	return modelo.Usuario{}, modelo.ErrUsuarioNoEncontrado
}

func (repoFalso) Ping(context.Context) error { return nil }

func router(t *testing.T) (http.Handler, repoFalso) {
	t.Helper()
	clave, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	repo := repoFalso{map[string]modelo.Usuario{}}
	s, err := service.Nuevo(repo, clave, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return NuevoRouter(s), repo
}

func pedir(t *testing.T, h http.Handler, ruta, cuerpo string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, ruta, strings.NewReader(cuerpo)))
	var r map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatalf("no es JSON (%d): %s", w.Code, w.Body)
	}
	return w, r
}

const ana = `{"email":"ana@example.com","contrasena":"remera-blanca"}`

// HU-18.2: ni la contraseña ni su hash salen en ninguna respuesta.
func TestNingunaRespuestaExponeLaContrasena(t *testing.T) {
	h, repo := router(t)
	registro, r := pedir(t, h, "/v1/registro", ana)
	if registro.Code != 201 || r["rol"] != "comprador" || r["email"] != "ana@example.com" || r["usuarioId"] == "" {
		t.Fatalf("%d %v", registro.Code, r)
	}
	sesion, s := pedir(t, h, "/v1/sesiones", ana)
	if sesion.Code != 200 || s["token"] == "" || s["tipo"] != "Bearer" || s["venceEn"] == "" {
		t.Fatalf("%d %v", sesion.Code, s)
	}
	if sesion.Header().Get("Cache-Control") != "no-store" {
		t.Error("el token no se puede cachear")
	}
	hash := repo.guardados["ana@example.com"].Hash
	for _, cuerpo := range []string{registro.Body.String(), sesion.Body.String()} {
		if strings.Contains(cuerpo, "remera-blanca") || strings.Contains(cuerpo, hash) || strings.Contains(strings.ToLower(cuerpo), "hash") {
			t.Errorf("expone datos de la contraseña: %s", cuerpo)
		}
	}
}

func TestNoSePuedeElegirElRol(t *testing.T) {
	h, repo := router(t)
	w, r := pedir(t, h, "/v1/registro", `{"email":"ana@example.com","contrasena":"remera-blanca","rol":"administrador"}`)
	if w.Code != 400 || r["codigo"] != "PEDIDO_INVALIDO" || len(repo.guardados) != 0 {
		t.Errorf("%d %v", w.Code, r)
	}
}

func TestErroresDelRegistro(t *testing.T) {
	h, _ := router(t)
	pedir(t, h, "/v1/registro", ana)
	w, r := pedir(t, h, "/v1/registro", ana)
	if w.Code != 409 || r["codigo"] != "EMAIL_EN_USO" {
		t.Errorf("repetido: %d %v", w.Code, r)
	}
	w, r = pedir(t, h, "/v1/registro", `{"email":"nada","contrasena":"corta"}`)
	if w.Code != 400 || len(r["errores"].([]any)) != 2 {
		t.Errorf("inválido: %d %v", w.Code, r)
	}
	if w.Header().Get("Content-Type") != "application/problem+json" {
		t.Errorf("content-type: %s", w.Header().Get("Content-Type"))
	}
}

// HU-18.3: la respuesta es idéntica para un email inexistente y para una contraseña incorrecta.
func TestCredencialesIncorrectasDanLaMismaRespuesta(t *testing.T) {
	h, _ := router(t)
	pedir(t, h, "/v1/registro", ana)
	a, _ := pedir(t, h, "/v1/sesiones", `{"email":"ana@example.com","contrasena":"otra-cosa"}`)
	b, r := pedir(t, h, "/v1/sesiones", `{"email":"nadie@example.com","contrasena":"otra-cosa"}`)
	if a.Code != 401 || b.Code != 401 || a.Body.String() != b.Body.String() || r["codigo"] != "CREDENCIALES_INVALIDAS" {
		t.Errorf("%d %s / %d %s", a.Code, a.Body, b.Code, b.Body)
	}
}

// Un JSON roto no puede devolver un pedazo del cuerpo, que trae la contraseña.
func TestJSONRotoNoRepiteElCuerpo(t *testing.T) {
	h, _ := router(t)
	w, _ := pedir(t, h, "/v1/sesiones", `{"email":"ana@example.com","contrasena":secreto-sin-comillas}`)
	if w.Code != 400 || strings.Contains(w.Body.String(), "secreto") {
		t.Errorf("%d %s", w.Code, w.Body)
	}
}
