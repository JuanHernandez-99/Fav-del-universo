// Package handler es la capa HTTP de Usuarios: rutas, JSON y traducción de errores. No conoce MySQL.
// Ninguna respuesta incluye la contraseña ni su hash (HU-18.2).
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/JuanHernandez-99/Fav-del-universo/services/usuarios/internal/modelo"
	"github.com/JuanHernandez-99/Fav-del-universo/services/usuarios/internal/observabilidad"
	"github.com/JuanHernandez-99/Fav-del-universo/services/usuarios/internal/service"
)

type Handler struct {
	s *service.Servicio
}

func NuevoRouter(s *service.Servicio) http.Handler {
	h := &Handler{s}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", h.listo)
	mux.Handle("GET /metrics", promhttp.Handler())

	mux.HandleFunc("POST /v1/registro", h.registrar)
	mux.HandleFunc("POST /v1/sesiones", h.iniciarSesion)
	return observabilidad.Middleware(mux)
}

// credenciales es el cuerpo de las dos rutas. Como se rechazan los campos desconocidos, nadie puede
// mandar un "rol" para registrarse como administrador.
type credenciales struct {
	Email      string `json:"email"`
	Contrasena string `json:"contrasena"`
}

type usuarioJSON struct {
	UsuarioID string    `json:"usuarioId"`
	Email     string    `json:"email"`
	Rol       string    `json:"rol"`
	CreadoEn  time.Time `json:"creadoEn"`
}

func (h *Handler) registrar(w http.ResponseWriter, r *http.Request) {
	var c credenciales
	if !leerJSON(w, r, &c) {
		return
	}
	u, err := h.s.Registrar(r.Context(), c.Email, c.Contrasena)
	if err != nil {
		escribirError(w, r, err)
		return
	}
	escribirJSON(w, http.StatusCreated, usuarioJSON{u.ID, u.Email, u.Rol, u.CreadoEn.UTC()})
}

func (h *Handler) iniciarSesion(w http.ResponseWriter, r *http.Request) {
	var c credenciales
	if !leerJSON(w, r, &c) {
		return
	}
	sesion, err := h.s.IniciarSesion(r.Context(), c.Email, c.Contrasena)
	if err != nil {
		escribirError(w, r, err)
		return
	}
	escribirJSON(w, http.StatusOK, map[string]any{"token": sesion.Token, "tipo": "Bearer", "venceEn": sesion.VenceEn.UTC()})
}

func (h *Handler) listo(w http.ResponseWriter, r *http.Request) {
	ctx, cancelar := context.WithTimeout(r.Context(), time.Second)
	defer cancelar()
	if err := h.s.Listo(ctx); err != nil {
		http.Error(w, "base no disponible", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func leerJSON(w http.ResponseWriter, r *http.Request, destino any) bool {
	decodificador := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decodificador.DisallowUnknownFields()
	if err := decodificador.Decode(destino); err != nil {
		// Sin el detalle del error: el cuerpo trae una contraseña.
		escribirError(w, r, &modelo.ErrorValidacion{Campos: []modelo.CampoInvalido{{Campo: "cuerpo", Mensaje: "JSON inválido: se esperan email y contrasena"}}})
		return false
	}
	return true
}

func escribirJSON(w http.ResponseWriter, codigo int, cuerpo any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store") // las respuestas pueden traer el token
	w.WriteHeader(codigo)
	_ = json.NewEncoder(w).Encode(cuerpo)
}

// problema sigue RFC 9457 con un campo `codigo` estable, igual que los demás servicios.
type problema struct {
	Type    string                 `json:"type"`
	Title   string                 `json:"title"`
	Status  int                    `json:"status"`
	Detail  string                 `json:"detail,omitempty"`
	Codigo  string                 `json:"codigo"`
	Errores []modelo.CampoInvalido `json:"errores,omitempty"`
}

func escribirError(w http.ResponseWriter, r *http.Request, err error) {
	var p problema
	var validacion *modelo.ErrorValidacion
	switch {
	case errors.As(err, &validacion):
		p.Status, p.Codigo, p.Title, p.Errores = 400, "PEDIDO_INVALIDO", "Pedido inválido", validacion.Campos
	case errors.Is(err, modelo.ErrEmailEnUso):
		p.Status, p.Codigo, p.Title, p.Detail = 409, "EMAIL_EN_USO", "Email en uso", err.Error()
	case errors.Is(err, modelo.ErrCredenciales):
		p.Status, p.Codigo, p.Title, p.Detail = 401, "CREDENCIALES_INVALIDAS", "Credenciales inválidas", err.Error()
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		w.Header().Set("Retry-After", "1")
		p.Status, p.Codigo, p.Title = 503, "NO_DISPONIBLE", "Servicio no disponible"
	default:
		// El detalle de un error inesperado va al log, no al cliente.
		slog.ErrorContext(r.Context(), "error inesperado", "error", err)
		p.Status, p.Codigo, p.Title = 500, "ERROR_INTERNO", "Error interno"
	}
	p.Type = "https://fav.example/errores/" + strings.ToLower(strings.ReplaceAll(p.Codigo, "_", "-"))
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}
