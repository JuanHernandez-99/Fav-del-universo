// Package http es el adaptador de entrada HTTP de Inventario: traduce pedidos
// HTTP a casos de uso y errores del dominio a respuestas (Problem Details, RFC 9457).
//
// Es una API interna: la usan Ventas y el panel de administración a través del
// gateway; no es la API pública de la capacidad compartida.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/adaptadores"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/aplicacion"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/dominio"
)

const maxCuerpo = 1 << 20 // 1 MB

// Handler arma las rutas del servicio.
type Handler struct {
	svc *aplicacion.Servicio
	log *slog.Logger
}

func NuevoHandler(svc *aplicacion.Servicio, log *slog.Logger) http.Handler {
	h := &Handler{svc: svc, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.salud)
	mux.HandleFunc("GET /v1/stock", h.consultarStock)
	mux.HandleFunc("POST /v1/reservas", h.reservar)
	mux.HandleFunc("GET /v1/reservas/{id}", h.obtenerReserva)
	mux.HandleFunc("POST /v1/reservas/{id}/confirmar", h.confirmar)
	mux.HandleFunc("POST /v1/reservas/{id}/liberar", h.liberar)
	return conCorrelacion(log, mux)
}

// ---------- Rutas ----------

func (h *Handler) salud(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if err := h.svc.Disponible(ctx); err != nil {
		escribirJSON(w, http.StatusServiceUnavailable, map[string]string{"estado": "sin base de datos"})
		return
	}
	escribirJSON(w, http.StatusOK, map[string]string{"estado": "ok"})
}

func (h *Handler) consultarStock(w http.ResponseWriter, r *http.Request) {
	stock, err := h.svc.ConsultarStock(r.Context(), r.URL.Query()["varianteId"])
	if err != nil {
		h.error(w, r, err)
		return
	}
	resp := make([]stockJSON, len(stock))
	for i, s := range stock {
		resp[i] = stockJSON{VarianteID: s.VarianteID, Canal: string(s.Canal), Cantidad: s.Cantidad, Reservado: s.Reservado, Disponible: s.Disponible()}
	}
	escribirJSON(w, http.StatusOK, map[string]any{"items": resp})
}

func (h *Handler) reservar(w http.ResponseWriter, r *http.Request) {
	var p pedidoReservaJSON
	if !leerJSON(w, r, &p, h) {
		return
	}
	items := make([]dominio.Item, len(p.Items))
	for i, it := range p.Items {
		items[i] = dominio.Item{VarianteID: it.VarianteID, Cantidad: it.Cantidad}
	}
	res, err := h.svc.Reservar(r.Context(), aplicacion.PedidoReserva{
		ClaveIdempotencia:  r.Header.Get("Idempotency-Key"),
		Referencia:         p.Referencia,
		VencimientoMinutos: p.VencimientoMinutos,
		Items:              items,
		CorrelacionID:      correlacion(r.Context()),
	})
	if err != nil {
		h.error(w, r, err)
		return
	}
	escribirJSON(w, http.StatusCreated, aReservaJSON(res))
}

func (h *Handler) obtenerReserva(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.ObtenerReserva(r.Context(), r.PathValue("id"))
	if err != nil {
		h.error(w, r, err)
		return
	}
	escribirJSON(w, http.StatusOK, aReservaJSON(res))
}

func (h *Handler) confirmar(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.Confirmar(r.Context(), r.PathValue("id"))
	if err != nil {
		h.error(w, r, err)
		return
	}
	escribirJSON(w, http.StatusOK, aReservaJSON(res))
}

func (h *Handler) liberar(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.Liberar(r.Context(), r.PathValue("id"))
	if err != nil {
		h.error(w, r, err)
		return
	}
	escribirJSON(w, http.StatusOK, aReservaJSON(res))
}

// ---------- Formato de los datos ----------

type itemJSON struct {
	VarianteID string `json:"varianteId"`
	Cantidad   int    `json:"cantidad"`
}

type pedidoReservaJSON struct {
	Referencia         string     `json:"referencia"`
	VencimientoMinutos int        `json:"vencimientoMinutos"`
	Items              []itemJSON `json:"items"`
}

type reservaJSON struct {
	ID         string     `json:"id"`
	Estado     string     `json:"estado"`
	Referencia string     `json:"referencia,omitempty"`
	Items      []itemJSON `json:"items"`
	CreadaEn   time.Time  `json:"creadaEn"`
	VenceEn    time.Time  `json:"venceEn"`
}

type stockJSON struct {
	VarianteID string `json:"varianteId"`
	Canal      string `json:"canal"`
	Cantidad   int    `json:"cantidad"`
	Reservado  int    `json:"reservado"`
	Disponible int    `json:"disponible"`
}

func aReservaJSON(r dominio.Reserva) reservaJSON {
	items := make([]itemJSON, len(r.Items))
	for i, it := range r.Items {
		items[i] = itemJSON{VarianteID: it.VarianteID, Cantidad: it.Cantidad}
	}
	return reservaJSON{ID: r.ID, Estado: string(r.Estado), Referencia: r.Referencia, Items: items,
		CreadaEn: r.CreadaEn.UTC(), VenceEn: r.VenceEn.UTC()}
}

// ---------- Errores (Problem Details) ----------

type problema struct {
	Type      string                  `json:"type"`
	Title     string                  `json:"title"`
	Status    int                     `json:"status"`
	Detail    string                  `json:"detail,omitempty"`
	Codigo    string                  `json:"codigo"`
	Errores   []dominio.CampoInvalido `json:"errores,omitempty"`
	Faltantes []dominio.Faltante      `json:"faltantes,omitempty"`
	Variantes []string                `json:"variantes,omitempty"`
}

func (h *Handler) error(w http.ResponseWriter, r *http.Request, err error) {
	var (
		verr  *dominio.ErrorValidacion
		stock *dominio.ErrStockInsuficiente
		inex  *dominio.ErrVarianteInexistente
		p     problema
	)
	switch {
	case errors.As(err, &verr):
		p = problema{Status: 400, Codigo: "PEDIDO_INVALIDO", Title: "Pedido inválido", Errores: verr.Errores}
	case errors.As(err, &stock):
		p = problema{Status: 409, Codigo: "STOCK_INSUFICIENTE", Title: "Stock insuficiente",
			Detail: "Al menos una variante no alcanza. No se reservó ninguna unidad.", Faltantes: stock.Faltantes}
	case errors.As(err, &inex):
		p = problema{Status: 422, Codigo: "VARIANTE_INEXISTENTE", Title: "Variante inexistente", Variantes: inex.IDs}
	case errors.Is(err, dominio.ErrClaveReutilizada):
		p = problema{Status: 409, Codigo: "CLAVE_IDEMPOTENCIA_REUTILIZADA", Title: "Clave de idempotencia reutilizada"}
	case errors.Is(err, dominio.ErrReservaInexistente):
		p = problema{Status: 404, Codigo: "RESERVA_INEXISTENTE", Title: "Reserva inexistente"}
	case errors.Is(err, dominio.ErrReservaVencida):
		p = problema{Status: 409, Codigo: "RESERVA_VENCIDA", Title: "Reserva vencida"}
	case errors.Is(err, dominio.ErrReservaLiberada):
		p = problema{Status: 409, Codigo: "RESERVA_LIBERADA", Title: "Reserva liberada"}
	case errors.Is(err, dominio.ErrReservaConfirmada):
		p = problema{Status: 409, Codigo: "RESERVA_CONFIRMADA", Title: "Reserva ya confirmada"}
	default:
		h.log.ErrorContext(r.Context(), "error inesperado", "error", err.Error(), "correlation_id", correlacion(r.Context()))
		p = problema{Status: 500, Codigo: "ERROR_INTERNO", Title: "Error interno"}
	}
	p.Type = "https://fav.example/errores/" + tipo(p.Codigo)
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	json.NewEncoder(w).Encode(p) //nolint:errcheck
}

func tipo(codigo string) string {
	b := []byte(codigo)
	for i, c := range b {
		switch {
		case c == '_':
			b[i] = '-'
		case c >= 'A' && c <= 'Z':
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// ---------- Utilidades ----------

// leerJSON ignora los campos desconocidos: agregar un campo al pedido es un cambio
// compatible (política de versionado del contrato), así que no puede dar 400.
func leerJSON(w http.ResponseWriter, r *http.Request, destino any, h *Handler) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCuerpo))
	if err := dec.Decode(destino); err != nil {
		h.error(w, r, &dominio.ErrorValidacion{Errores: []dominio.CampoInvalido{{Campo: "cuerpo", Mensaje: "JSON inválido: " + err.Error()}}})
		return false
	}
	return true
}

func escribirJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

// ---------- Correlación y logs ----------

type claveCorrelacion struct{}

func correlacion(ctx context.Context) string {
	id, _ := ctx.Value(claveCorrelacion{}).(string)
	return id
}

type grabador struct {
	http.ResponseWriter
	status int
}

func (g *grabador) WriteHeader(s int) { g.status = s; g.ResponseWriter.WriteHeader(s) }

// conCorrelacion toma X-Correlation-Id (o genera uno), lo devuelve en la respuesta
// y registra cada pedido en un log JSON con ese identificador (ADR-005, D11).
func conCorrelacion(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Correlation-Id")
		if id == "" || len(id) > 100 {
			id = adaptadores.NuevoUUID()
		}
		w.Header().Set("X-Correlation-Id", id)
		ctx := context.WithValue(r.Context(), claveCorrelacion{}, id)
		g := &grabador{ResponseWriter: w, status: http.StatusOK}
		inicio := time.Now()
		next.ServeHTTP(g, r.WithContext(ctx))
		if r.URL.Path == "/health" && g.status == http.StatusOK {
			return // no llenar los logs con los chequeos de salud
		}
		log.InfoContext(ctx, "pedido",
			"metodo", r.Method, "ruta", r.URL.Path, "status", g.status,
			"duracion_ms", time.Since(inicio).Milliseconds(), "correlation_id", id)
	})
}
