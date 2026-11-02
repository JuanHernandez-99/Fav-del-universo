// Package handler es la capa HTTP: rutas, JSON de entrada y salida, y traducción de errores a
// códigos HTTP. No conoce MongoDB. Rutas de administración bajo /v1/admin (D14).
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/service"
)

type Handler struct {
	s *service.Servicio
}

func NuevoRouter(s *service.Servicio) http.Handler {
	h := &Handler{s: s}
	mux := http.NewServeMux()

	// healthz: el proceso está vivo. readyz: además la base responde, así que puede recibir pedidos.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", h.listo)

	mux.HandleFunc("POST /v1/admin/productos", h.crearProducto)
	mux.HandleFunc("GET /v1/admin/productos/{id}", h.obtenerProducto)

	return mux
}

// ---------- Productos ----------

// productoJSON es la forma del producto en la API de administración. El precio sale en pesos con
// 2 decimales, como en el contrato; adentro se maneja en centavos (D13).
type productoJSON struct {
	ProductoID    string         `json:"productoId"`
	Nombre        string         `json:"nombre"`
	Descripcion   string         `json:"descripcion,omitempty"`
	Categoria     string         `json:"categoria"`
	SistemaTalles string         `json:"sistemaTalles"`
	PrecioBase    precioJSON     `json:"precioBase"`
	Publicado     bool           `json:"publicado"`
	Atributos     map[string]any `json:"atributos,omitempty"`
	Fotos         []fotoJSON     `json:"fotos"`
	Variantes     []varianteJSON `json:"variantes"`
	CreadoEn      time.Time      `json:"creadoEn"`
	ActualizadoEn time.Time      `json:"actualizadoEn"`
	Version       int            `json:"version"`
}

type precioJSON struct {
	Monto  float64 `json:"monto"`
	Moneda string  `json:"moneda"`
}

type fotoJSON struct {
	URL   string  `json:"url"`
	Color *string `json:"color"`
}

type varianteJSON struct {
	VarianteID string `json:"varianteId"`
	Talle      string `json:"talle"`
	Color      string `json:"color"`
}

func productoAJSON(p modelo.Producto) productoJSON {
	fotos := make([]fotoJSON, len(p.Fotos))
	for i, f := range p.Fotos {
		fotos[i] = fotoJSON{f.URL, f.Color}
	}
	variantes := make([]varianteJSON, len(p.Variantes))
	for i, v := range p.Variantes {
		variantes[i] = varianteJSON{v.ID, v.Talle, v.Color}
	}
	return productoJSON{
		ProductoID:    p.ID,
		Nombre:        p.Nombre,
		Descripcion:   p.Descripcion,
		Categoria:     p.Categoria,
		SistemaTalles: p.SistemaTalles,
		PrecioBase:    precioJSON{Monto: pesos(p.PrecioBase.Monto), Moneda: p.PrecioBase.Moneda},
		Publicado:     p.Publicado,
		Atributos:     p.Atributos,
		Fotos:         fotos,
		Variantes:     variantes,
		CreadoEn:      p.CreadoEn.UTC(),
		ActualizadoEn: p.ActualizadoEn.UTC(),
		Version:       p.Version,
	}
}

// pesos convierte centavos a pesos solo para mostrar: 1500000 → 15000, 2499050 → 24990.5.
func pesos(centavos int64) float64 { return float64(centavos) / 100 }

// aCentavos convierte el monto recibido en pesos. Se redondea porque en coma flotante
// 0.29 * 100 da 28.999…; ok es false si tenía más de 2 decimales.
func aCentavos(pesos float64) (centavos int64, ok bool) {
	redondeado := math.Round(pesos * 100)
	return int64(redondeado), math.Abs(pesos*100-redondeado) < 1e-6
}

// pedidoProducto es lo que manda el administrador. No lleva ids, publicado ni version: los pone
// el servidor, y como se rechazan los campos desconocidos, no se pueden forzar.
type pedidoProducto struct {
	Nombre        string `json:"nombre"`
	Descripcion   string `json:"descripcion"`
	Categoria     string `json:"categoria"`
	SistemaTalles string `json:"sistemaTalles"`
	PrecioBase    struct {
		Monto  float64 `json:"monto"`
		Moneda string  `json:"moneda"`
	} `json:"precioBase"`
	Atributos map[string]any `json:"atributos"`
	Fotos     []fotoJSON     `json:"fotos"`
	Variantes []struct {
		Talle string `json:"talle"`
		Color string `json:"color"`
	} `json:"variantes"`
}

func (h *Handler) crearProducto(w http.ResponseWriter, r *http.Request) {
	var pedido pedidoProducto
	if !leerJSON(w, r, &pedido) {
		return
	}
	monto, ok := aCentavos(pedido.PrecioBase.Monto)
	if !ok {
		v := &modelo.ErrorValidacion{}
		v.Agregar("precioBase.monto", "admite hasta 2 decimales")
		escribirError(w, r, v)
		return
	}

	nuevo := service.NuevoProducto{
		Nombre:        pedido.Nombre,
		Descripcion:   pedido.Descripcion,
		Categoria:     pedido.Categoria,
		SistemaTalles: pedido.SistemaTalles,
		PrecioBase:    modelo.Precio{Monto: monto, Moneda: pedido.PrecioBase.Moneda},
		Atributos:     pedido.Atributos,
	}
	for _, f := range pedido.Fotos {
		nuevo.Fotos = append(nuevo.Fotos, modelo.Foto{URL: f.URL, Color: f.Color})
	}
	for _, v := range pedido.Variantes {
		nuevo.Variantes = append(nuevo.Variantes, service.NuevaVariante{Talle: v.Talle, Color: v.Color})
	}

	p, err := h.s.CrearProducto(r.Context(), nuevo)
	if err != nil {
		escribirError(w, r, err)
		return
	}
	w.Header().Set("Location", "/v1/admin/productos/"+p.ID)
	escribirJSON(w, http.StatusCreated, productoAJSON(p))
}

func (h *Handler) obtenerProducto(w http.ResponseWriter, r *http.Request) {
	p, err := h.s.ObtenerProducto(r.Context(), r.PathValue("id"))
	if err != nil {
		escribirError(w, r, err)
		return
	}
	escribirJSON(w, http.StatusOK, productoAJSON(p))
}

// ---------- Sondas ----------

func (h *Handler) listo(w http.ResponseWriter, r *http.Request) {
	ctx, cancelar := context.WithTimeout(r.Context(), time.Second)
	defer cancelar()
	if err := h.s.Listo(ctx); err != nil {
		http.Error(w, "base no disponible", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ---------- Auxiliares ----------

func leerJSON(w http.ResponseWriter, r *http.Request, destino any) bool {
	decodificador := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decodificador.DisallowUnknownFields()
	if err := decodificador.Decode(destino); err != nil {
		v := &modelo.ErrorValidacion{}
		v.Agregar("cuerpo", "JSON inválido: %v", err)
		escribirError(w, r, v)
		return false
	}
	return true
}

func escribirJSON(w http.ResponseWriter, codigo int, cuerpo any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(codigo)
	_ = json.NewEncoder(w).Encode(cuerpo)
}

// problema sigue RFC 9457 con un campo `codigo` estable, igual que Inventario y el contrato.
type problema struct {
	Type    string                 `json:"type"`
	Title   string                 `json:"title"`
	Status  int                    `json:"status"`
	Detail  string                 `json:"detail,omitempty"`
	Codigo  string                 `json:"codigo"`
	Errores []modelo.CampoInvalido `json:"errores,omitempty"`
}

func escribirError(w http.ResponseWriter, r *http.Request, err error) {
	p := problema{Detail: err.Error()}
	var validacion *modelo.ErrorValidacion
	switch {
	case errors.As(err, &validacion):
		p.Status, p.Codigo, p.Title, p.Errores = 400, "PEDIDO_INVALIDO", "Pedido inválido", validacion.Campos
		p.Detail = "Revisar los campos indicados en errores."
	case errors.Is(err, modelo.ErrVarianteDuplicada):
		p.Status, p.Codigo, p.Title = 409, "VARIANTE_DUPLICADA", "Variante duplicada"
	case errors.Is(err, modelo.ErrProductoNoEncontrado):
		p.Status, p.Codigo, p.Title = 404, "PRODUCTO_NO_ENCONTRADO", "Producto no encontrado"
		p.Detail = "No existe el producto " + r.PathValue("id") + "."
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		w.Header().Set("Retry-After", "1")
		p.Status, p.Codigo, p.Title, p.Detail = 503, "NO_DISPONIBLE", "Servicio no disponible", ""
	default:
		// El detalle de un error inesperado va al log, no al cliente.
		slog.ErrorContext(r.Context(), "error inesperado", "error", err)
		p.Status, p.Codigo, p.Title, p.Detail = 500, "ERROR_INTERNO", "Error interno", ""
	}
	p.Type = "https://fav.example/errores/" + strings.ToLower(strings.ReplaceAll(p.Codigo, "_", "-"))

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}
