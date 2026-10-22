// Package http es el adaptador de entrada HTTP de Inventario. Lo usan Ventas (reservas) y el panel
// de administración (stock, transferencias y reposiciones), siempre a través del gateway.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/aplicacion"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/dominio"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/observabilidad"
)

type Handler struct {
	s *aplicacion.Servicio
}

func NuevoRouter(s *aplicacion.Servicio) http.Handler {
	h := &Handler{s: s}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", h.listo)
	mux.Handle("GET /metrics", promhttp.Handler())

	mux.HandleFunc("POST /v1/reservas", h.reservar)
	mux.HandleFunc("GET /v1/reservas/{id}", h.obtenerReserva)
	mux.HandleFunc("POST /v1/reservas/{id}/confirmar", h.confirmar)
	mux.HandleFunc("POST /v1/reservas/{id}/liberar", h.liberar)

	mux.HandleFunc("GET /v1/stock", h.consultarStock)
	mux.HandleFunc("POST /v1/ingresos", h.ingresar)
	mux.HandleFunc("POST /v1/transferencias", h.transferir)

	mux.HandleFunc("POST /v1/reposiciones", h.reponer)
	mux.HandleFunc("GET /v1/consignacion", h.stockEnGimnasio)

	return observabilidad.Middleware(mux)
}

// ---------- Reservas ----------

type pedidoReserva struct {
	Referencia      string `json:"referencia"`
	VigenciaMinutos int    `json:"vigenciaMinutos"`
	Items           []struct {
		VarianteID string `json:"varianteId"`
		Cantidad   int    `json:"cantidad"`
	} `json:"items"`
}

type reservaJSON struct {
	ReservaID  string     `json:"reservaId"`
	Referencia string     `json:"referencia,omitempty"`
	Estado     string     `json:"estado"`
	Items      []itemJSON `json:"items"`
	VenceEn    time.Time  `json:"venceEn"`
	CreadaEn   time.Time  `json:"creadaEn"`
}

type itemJSON struct {
	VarianteID string `json:"varianteId"`
	Cantidad   int    `json:"cantidad"`
}

func (h *Handler) reservar(w http.ResponseWriter, r *http.Request) {
	var p pedidoReserva
	if !leerJSON(w, r, &p) {
		return
	}
	items := make([]dominio.Item, len(p.Items))
	for i, it := range p.Items {
		items[i] = dominio.Item{VarianteID: it.VarianteID, Cantidad: it.Cantidad}
	}
	res, repetida, err := h.s.Reservar(r.Context(), aplicacion.PedidoReserva{
		ClaveIdempotencia: r.Header.Get("Idempotency-Key"),
		Referencia:        p.Referencia,
		Items:             items,
		Vigencia:          time.Duration(p.VigenciaMinutos) * time.Minute,
	})
	if err != nil {
		escribirError(w, r, err)
		return
	}
	trace.SpanFromContext(r.Context()).SetAttributes(attribute.String("reserva_id", res.ID), attribute.Bool("idempotente_repetida", repetida))
	if repetida {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	escribirJSON(w, http.StatusCreated, aJSON(res))
}

func (h *Handler) obtenerReserva(w http.ResponseWriter, r *http.Request) {
	res, err := h.s.ObtenerReserva(r.Context(), r.PathValue("id"))
	responderReserva(w, r, res, err)
}

func (h *Handler) confirmar(w http.ResponseWriter, r *http.Request) {
	res, err := h.s.Confirmar(r.Context(), r.PathValue("id"))
	responderReserva(w, r, res, err)
}

func (h *Handler) liberar(w http.ResponseWriter, r *http.Request) {
	res, err := h.s.Liberar(r.Context(), r.PathValue("id"))
	responderReserva(w, r, res, err)
}

func responderReserva(w http.ResponseWriter, r *http.Request, res dominio.Reserva, err error) {
	if err != nil {
		escribirError(w, r, err)
		return
	}
	escribirJSON(w, http.StatusOK, aJSON(res))
}

func aJSON(res dominio.Reserva) reservaJSON {
	items := make([]itemJSON, len(res.Items))
	for i, it := range res.Items {
		items[i] = itemJSON{it.VarianteID, it.Cantidad}
	}
	return reservaJSON{res.ID, res.Referencia, string(res.Estado), items, res.VenceEn.UTC(), res.CreadaEn.UTC()}
}

// ---------- Stock ----------

type stockJSON struct {
	VarianteID       string    `json:"varianteId"`
	Online           int       `json:"online"`
	Fisico           int       `json:"fisico"`
	Reservado        int       `json:"reservado"`
	DisponibleOnline int       `json:"disponibleOnline"`
	StockACorregir   bool      `json:"stockACorregir"`
	ActualizadoEn    time.Time `json:"actualizadoEn"`
}

func stockAJSON(s dominio.Stock) stockJSON {
	return stockJSON{s.VarianteID, s.Online, s.Fisico, s.Reservado, s.DisponibleOnline(), s.AConfirmar(), s.ActualizadoEn.UTC()}
}

// consultarStock recibe los ids separados por coma: el panel los pide por lote (ADR-001, decisión 3).
func (h *Handler) consultarStock(w http.ResponseWriter, r *http.Request) {
	var ids []string
	for _, id := range strings.Split(r.URL.Query().Get("variantes"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 || len(ids) > 200 {
		escribirError(w, r, &dominio.ErrorValidacion{Campos: []dominio.CampoInvalido{{Campo: "variantes", Mensaje: "indicar entre 1 y 200 ids separados por coma"}}})
		return
	}
	stocks, err := h.s.ConsultarStock(r.Context(), ids)
	if err != nil {
		escribirError(w, r, err)
		return
	}
	items := make([]stockJSON, len(stocks))
	for i, s := range stocks {
		items[i] = stockAJSON(s)
	}
	escribirJSON(w, http.StatusOK, map[string]any{"items": items})
}

type pedidoMovimiento struct {
	VarianteID string        `json:"varianteId"`
	Canal      dominio.Canal `json:"canal"`  // ingresos
	Origen     dominio.Canal `json:"origen"` // transferencias
	Cantidad   int           `json:"cantidad"`
}

func (h *Handler) ingresar(w http.ResponseWriter, r *http.Request) {
	var p pedidoMovimiento
	if !leerJSON(w, r, &p) {
		return
	}
	st, err := h.s.Ingresar(r.Context(), r.Header.Get("Idempotency-Key"), p.VarianteID, p.Canal, p.Cantidad, usuario(r))
	responderStock(w, r, st, err)
}

func (h *Handler) transferir(w http.ResponseWriter, r *http.Request) {
	var p pedidoMovimiento
	if !leerJSON(w, r, &p) {
		return
	}
	st, err := h.s.Transferir(r.Context(), r.Header.Get("Idempotency-Key"), p.VarianteID, p.Origen, p.Cantidad, usuario(r))
	responderStock(w, r, st, err)
}

func responderStock(w http.ResponseWriter, r *http.Request, st dominio.Stock, err error) {
	if err != nil {
		escribirError(w, r, err)
		return
	}
	escribirJSON(w, http.StatusOK, stockAJSON(st))
}

// ---------- Consignación en el gimnasio ----------

func (h *Handler) reponer(w http.ResponseWriter, r *http.Request) {
	var p pedidoMovimiento
	if !leerJSON(w, r, &p) {
		return
	}
	rep, repetida, err := h.s.Reponer(r.Context(), r.Header.Get("Idempotency-Key"), p.VarianteID, p.Cantidad, usuario(r))
	if err != nil {
		escribirError(w, r, err)
		return
	}
	if repetida {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	// 202: el stock físico ya se descontó; el envío al gimnasio sigue en segundo plano.
	escribirJSON(w, http.StatusAccepted, map[string]any{
		"reposicionId": rep.ID, "varianteId": rep.VarianteID, "cantidad": rep.Cantidad,
		"estado": rep.Estado, "creadaEn": rep.CreadaEn.UTC(),
	})
}

func (h *Handler) stockEnGimnasio(w http.ResponseWriter, r *http.Request) {
	stock, err := h.s.StockEnGimnasio(r.Context())
	if err != nil {
		escribirError(w, r, err)
		return
	}
	items := make([]map[string]any, len(stock))
	for i, s := range stock {
		items[i] = map[string]any{"varianteId": s.VarianteID, "cantidad": s.Cantidad, "vendidas": s.Vendidas}
	}
	escribirJSON(w, http.StatusOK, map[string]any{"items": items})
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

// ---------- Auxiliares ----------

// usuario lo informa el gateway después de validar el token (ADR-001). Se usa para el registro de movimientos.
func usuario(r *http.Request) string { return r.Header.Get("X-User-Id") }

func leerJSON(w http.ResponseWriter, r *http.Request, destino any) bool {
	decodificador := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decodificador.DisallowUnknownFields()
	if err := decodificador.Decode(destino); err != nil {
		escribirError(w, r, &dominio.ErrorValidacion{Campos: []dominio.CampoInvalido{{Campo: "cuerpo", Mensaje: "JSON inválido: " + err.Error()}}})
		return false
	}
	return true
}

func escribirJSON(w http.ResponseWriter, codigo int, cuerpo any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(codigo)
	_ = json.NewEncoder(w).Encode(cuerpo)
}

// problema sigue RFC 9457 con un campo `codigo` estable, igual que el contrato publicado (ADR-008).
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

func escribirError(w http.ResponseWriter, r *http.Request, err error) {
	p := problema{Detail: err.Error()}
	var validacion *dominio.ErrorValidacion
	var stock *dominio.ErrorStockInsuficiente
	var inexistente *dominio.ErrorVarianteInexistente

	switch {
	case errors.As(err, &validacion):
		p.Status, p.Codigo, p.Title, p.Errores = 400, "PEDIDO_INVALIDO", "Pedido inválido", validacion.Campos
	case errors.As(err, &stock):
		p.Status, p.Codigo, p.Title, p.Faltantes = 409, "STOCK_INSUFICIENTE", "Stock insuficiente", stock.Faltantes
	case errors.As(err, &inexistente):
		p.Status, p.Codigo, p.Title, p.Variantes = 404, "VARIANTE_INEXISTENTE", "Variante inexistente", inexistente.Variantes
	case errors.Is(err, dominio.ErrClaveReutilizada):
		p.Status, p.Codigo, p.Title = 409, "CLAVE_IDEMPOTENCIA_REUTILIZADA", "Clave de idempotencia reutilizada"
	case errors.Is(err, dominio.ErrReservaNoEncontrada):
		p.Status, p.Codigo, p.Title = 404, "RESERVA_NO_ENCONTRADA", "Reserva no encontrada"
	case errors.Is(err, dominio.ErrReservaVencida):
		p.Status, p.Codigo, p.Title = 409, "RESERVA_VENCIDA", "La reserva venció"
	case errors.Is(err, dominio.ErrReservaYaConfirmada):
		p.Status, p.Codigo, p.Title = 409, "RESERVA_CONFIRMADA", "La reserva ya está confirmada"
	case errors.Is(err, dominio.ErrReservaYaLiberada):
		p.Status, p.Codigo, p.Title = 409, "RESERVA_LIBERADA", "La reserva ya fue liberada"
	case errors.Is(err, dominio.ErrProveedorNoDisponible):
		w.Header().Set("Retry-After", "30")
		p.Status, p.Codigo, p.Title = 503, "GIMNASIO_NO_DISPONIBLE", "El gimnasio no está disponible"
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
