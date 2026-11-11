// Package http es el adaptador de entrada HTTP de Ventas. Llega siempre por el gateway, que valida la
// identidad e informa quién llama en los encabezados X-User-* (ADR-001).
package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/aplicacion"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/dominio"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/observabilidad"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/puertos"
)

const rolAdministrador = "administrador"

// DatosTransferencia son los datos bancarios de FAV. **[?]** Los define el cliente (SPEC §12).
type DatosTransferencia struct {
	Alias   string `json:"alias"`
	Titular string `json:"titular"`
}

type Handler struct {
	s             *aplicacion.Servicio
	transferencia DatosTransferencia
}

// Opcion ajusta qué rutas expone el router.
type Opcion func(*opciones)

type opciones struct{ sinCapacidadPublica bool }

// SinCapacidadPublica apaga POST /v1/pedidos y GET /v1/pedidos/{id}, para cuando el sistema pase al cliente (ADR-008).
func SinCapacidadPublica() Opcion { return func(o *opciones) { o.sinCapacidadPublica = true } }

func NuevoRouter(s *aplicacion.Servicio, transferencia DatosTransferencia, opcionesRouter ...Opcion) http.Handler {
	var o opciones
	for _, aplicar := range opcionesRouter {
		aplicar(&o)
	}
	h := &Handler{s, transferencia}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", h.listo)
	mux.Handle("GET /metrics", promhttp.Handler())

	// Comprador
	mux.HandleFunc("POST /v1/compras", h.comprar)
	mux.HandleFunc("POST /v1/compras/{id}/cancelar", h.cancelar)
	mux.HandleFunc("GET /v1/mis-compras", h.misCompras)
	mux.HandleFunc("GET /v1/mis-compras/{id}", h.miCompra)

	// Mercado Pago
	mux.HandleFunc("POST /v1/pagos/aviso", h.avisoPago)

	// Administrador
	mux.HandleFunc("GET /v1/admin/ventas", soloAdmin(h.listarVentas))
	mux.HandleFunc("GET /v1/admin/ventas/{id}", soloAdmin(h.obtenerVenta))
	mux.HandleFunc("POST /v1/admin/ventas/{id}/confirmar-transferencia", soloAdmin(h.confirmarTransferencia))
	mux.HandleFunc("POST /v1/admin/ventas/{id}/avanzar", soloAdmin(h.avanzar))
	mux.HandleFunc("POST /v1/admin/ventas-local", soloAdmin(h.registrarVentaLocal))
	mux.HandleFunc("GET /v1/admin/ventas-local/sugerencia", soloAdmin(h.sugerirMonto))

	// Capacidad publicada (contrato catalogo-pedidos-v1, ADR-008): adaptador aparte, en pedidos.go.
	if !o.sinCapacidadPublica {
		mux.HandleFunc("POST /v1/pedidos", h.crearPedido)
		mux.HandleFunc("GET /v1/pedidos/{id}", h.consultarPedido) // desde el contrato 1.3.0
	}

	return observabilidad.Middleware(mux)
}

// soloAdmin aplica HU-19. El gateway ya rechaza sin credencial; esta es la segunda barrera.
func soloAdmin(siguiente http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-User-Role") != rolAdministrador {
			escribirError(w, r, dominio.ErrNoAutorizado)
			return
		}
		siguiente(w, r)
	}
}

// ---------- Formato JSON ----------

type lineaJSON struct {
	VarianteID string `json:"varianteId"`
	Cantidad   int    `json:"cantidad"`
}

func lineasDe(items []lineaJSON) []dominio.LineaPedido {
	lineas := make([]dominio.LineaPedido, len(items))
	for i, it := range items {
		lineas[i] = dominio.LineaPedido{VarianteID: it.VarianteID, Cantidad: it.Cantidad}
	}
	return lineas
}

type itemJSON struct {
	VarianteID     string  `json:"varianteId"`
	Nombre         string  `json:"nombre,omitempty"`
	Talle          string  `json:"talle,omitempty"`
	Color          string  `json:"color,omitempty"`
	Cantidad       int     `json:"cantidad"`
	PrecioUnitario float64 `json:"precioUnitario"`
	Subtotal       float64 `json:"subtotal"`
}

type pagoJSON struct {
	Medio              string              `json:"medio"`
	VenceEn            *time.Time          `json:"venceEn,omitempty"`
	UrlPago            string              `json:"urlPago,omitempty"`
	DatosTransferencia *DatosTransferencia `json:"datosTransferencia,omitempty"`
}

type ventaJSON struct {
	VentaID           string     `json:"ventaId"`
	Canal             string     `json:"canal"`
	Estado            string     `json:"estado"`
	Email             string     `json:"email,omitempty"`
	ReferenciaExterna string     `json:"referenciaExterna,omitempty"`
	Items             []itemJSON `json:"items"`
	Total             float64    `json:"total"`
	Moneda            string     `json:"moneda"`
	Pago              pagoJSON   `json:"pago"`
	CreadaEn          time.Time  `json:"creadaEn"`
	ActualizadaEn     time.Time  `json:"actualizadaEn"`
}

func (h *Handler) aJSON(v dominio.Venta) ventaJSON {
	pago := pagoJSON{Medio: string(v.Medio), VenceEn: v.VenceEn, UrlPago: v.UrlPago}
	if v.Medio == dominio.MedioTransferencia && v.Estado == dominio.EstadoPendiente {
		pago.DatosTransferencia = &h.transferencia
	}
	return ventaJSON{
		VentaID: v.ID, Canal: string(v.Canal), Estado: string(v.Estado), Email: v.Email, ReferenciaExterna: v.ReferenciaExterna,
		Items: itemsJSON(v.Items), Total: pesos(v.Total), Moneda: "ARS", Pago: pago, CreadaEn: v.CreadaEn.UTC(), ActualizadaEn: v.ActualizadaEn.UTC(),
	}
}

func itemsJSON(items []dominio.Item) []itemJSON {
	salida := make([]itemJSON, len(items))
	for i, it := range items {
		salida[i] = itemJSON{it.VarianteID, it.Nombre, it.Talle, it.Color, it.Cantidad, pesos(it.Precio), pesos(it.Subtotal())}
	}
	return salida
}

func pesos(centavos int64) float64 { return float64(centavos) / 100 }

func centavos(p float64) int64 { return int64(math.Round(p * 100)) }

// ---------- Comprador ----------

type pedidoCompra struct {
	Email     string      `json:"email"`
	MedioPago string      `json:"medioPago"`
	Items     []lineaJSON `json:"items"`
}

func (h *Handler) comprar(w http.ResponseWriter, r *http.Request) {
	var p pedidoCompra
	if !leerJSON(w, r, &p) {
		return
	}
	// El comprador registrado usa el email de su cuenta, que llega con el token (RN-11).
	email := p.Email
	if deToken := r.Header.Get("X-User-Email"); deToken != "" {
		email = deToken
	}
	v, err := h.s.Comprar(r.Context(), aplicacion.PedidoCompra{
		Clave: r.Header.Get("Idempotency-Key"), Email: email, CompradorID: r.Header.Get("X-User-Id"),
		Medio: dominio.Medio(p.MedioPago), Lineas: lineasDe(p.Items),
	})
	if err != nil {
		escribirError(w, r, err)
		return
	}
	escribirJSON(w, http.StatusCreated, h.aJSON(v))
}

func (h *Handler) cancelar(w http.ResponseWriter, r *http.Request) {
	v, err := h.s.Cancelar(r.Context(), r.PathValue("id"), r.Header.Get("X-User-Id"))
	h.responderVenta(w, r, v, err)
}

func (h *Handler) misCompras(w http.ResponseWriter, r *http.Request) {
	comprador := r.Header.Get("X-User-Id")
	if comprador == "" {
		escribirError(w, r, dominio.ErrNoAutenticado)
		return
	}
	h.listar(w, r, puertos.FiltroVentas{CompradorID: comprador})
}

func (h *Handler) miCompra(w http.ResponseWriter, r *http.Request) {
	comprador := r.Header.Get("X-User-Id")
	if comprador == "" {
		escribirError(w, r, dominio.ErrNoAutenticado)
		return
	}
	v, err := h.s.CompraDe(r.Context(), r.PathValue("id"), comprador)
	h.responderVenta(w, r, v, err)
}

// ---------- Mercado Pago ----------

type avisoPago struct {
	Type string `json:"type"`
	Data struct {
		ID json.Number `json:"id"`
	} `json:"data"`
}

// avisoPago responde 200 si el aviso quedó procesado. Con 401 o 503, Mercado Pago lo reintenta.
func (h *Handler) avisoPago(w http.ResponseWriter, r *http.Request) {
	var a avisoPago
	decodificador := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	if err := decodificador.Decode(&a); err != nil {
		escribirError(w, r, &dominio.ErrorValidacion{Campos: []dominio.CampoInvalido{{Campo: "cuerpo", Mensaje: "JSON inválido"}}})
		return
	}
	if a.Type != "payment" || a.Data.ID == "" {
		w.WriteHeader(http.StatusOK) // otros avisos no le importan a Ventas
		return
	}
	if err := h.s.AvisoPago(r.Context(), r.Header.Get("X-Signature"), r.Header.Get("X-Request-Id"), a.Data.ID.String()); err != nil {
		escribirError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ---------- Administrador ----------

func (h *Handler) listarVentas(w http.ResponseWriter, r *http.Request) {
	estado := dominio.Estado(r.URL.Query().Get("estado"))
	if estado != "" && !estado.Valido() {
		escribirError(w, r, &dominio.ErrorValidacion{Campos: []dominio.CampoInvalido{{Campo: "estado", Mensaje: "estado desconocido"}}})
		return
	}
	h.listar(w, r, puertos.FiltroVentas{Estado: estado})
}

func (h *Handler) listar(w http.ResponseWriter, r *http.Request, f puertos.FiltroVentas) {
	f.Limite = min(entero(r.URL.Query().Get("limite"), 50), 200)
	f.Desde = entero(r.URL.Query().Get("desde"), 0)
	ventas, err := h.s.ListarVentas(r.Context(), f)
	if err != nil {
		escribirError(w, r, err)
		return
	}
	items := make([]ventaJSON, len(ventas))
	for i, v := range ventas {
		items[i] = h.aJSON(v)
	}
	escribirJSON(w, http.StatusOK, map[string]any{"items": items})
}

func entero(texto string, porDefecto int) int {
	if n, err := strconv.Atoi(texto); err == nil && n >= 0 {
		return n
	}
	return porDefecto
}

func (h *Handler) obtenerVenta(w http.ResponseWriter, r *http.Request) {
	v, err := h.s.ObtenerVenta(r.Context(), r.PathValue("id"))
	h.responderVenta(w, r, v, err)
}

func (h *Handler) confirmarTransferencia(w http.ResponseWriter, r *http.Request) {
	v, err := h.s.ConfirmarTransferencia(r.Context(), r.PathValue("id"))
	if err != nil {
		escribirError(w, r, err)
		return
	}
	// 202 mientras Inventario confirma: la venta pasa a Pagada en segundo plano (ADR-005, punto 4).
	codigo := http.StatusOK
	if v.Estado == dominio.EstadoPendiente {
		codigo = http.StatusAccepted
	}
	escribirJSON(w, codigo, h.aJSON(v))
}

func (h *Handler) avanzar(w http.ResponseWriter, r *http.Request) {
	v, err := h.s.Avanzar(r.Context(), r.PathValue("id"))
	h.responderVenta(w, r, v, err)
}

type pedidoLocal struct {
	MedioPago string      `json:"medioPago"`
	Monto     *float64    `json:"monto"`
	Items     []lineaJSON `json:"items"`
}

func (h *Handler) registrarVentaLocal(w http.ResponseWriter, r *http.Request) {
	var p pedidoLocal
	if !leerJSON(w, r, &p) {
		return
	}
	var monto *int64
	if p.Monto != nil {
		c := centavos(*p.Monto)
		monto = &c
	}
	v, err := h.s.RegistrarVentaLocal(r.Context(), aplicacion.PedidoLocal{
		Clave: r.Header.Get("Idempotency-Key"), Medio: dominio.Medio(p.MedioPago), Monto: monto,
		Lineas: lineasDe(p.Items), Usuario: r.Header.Get("X-User-Id"),
	})
	if err != nil {
		escribirError(w, r, err)
		return
	}
	escribirJSON(w, http.StatusCreated, h.aJSON(v))
}

// sugerirMonto recibe variantes=var-remera:2,var-jean:1 y medioPago.
func (h *Handler) sugerirMonto(w http.ResponseWriter, r *http.Request) {
	medio := dominio.Medio(r.URL.Query().Get("medioPago"))
	lineas, errValidacion := leerLineas(r.URL.Query().Get("variantes"))
	if !medio.ValidoEn(dominio.CanalLocal) {
		errValidacion = append(errValidacion, dominio.CampoInvalido{Campo: "medioPago", Mensaje: "debe ser EFECTIVO, TRANSFERENCIA o MERCADO_PAGO"})
	}
	if len(errValidacion) > 0 {
		escribirError(w, r, &dominio.ErrorValidacion{Campos: errValidacion})
		return
	}
	monto, err := h.s.SugerirMonto(r.Context(), medio, lineas)
	if err != nil {
		escribirError(w, r, err)
		return
	}
	escribirJSON(w, http.StatusOK, map[string]any{"montoSugerido": pesos(monto), "moneda": "ARS"})
}

func leerLineas(texto string) ([]dominio.LineaPedido, []dominio.CampoInvalido) {
	var lineas []dominio.LineaPedido
	for _, parte := range strings.Split(texto, ",") {
		id, cantidadTexto, _ := strings.Cut(strings.TrimSpace(parte), ":")
		cantidad, err := strconv.Atoi(cantidadTexto)
		if id == "" || err != nil || cantidad < 1 {
			return nil, []dominio.CampoInvalido{{Campo: "variantes", Mensaje: "usar el formato variante:cantidad separado por coma"}}
		}
		lineas = append(lineas, dominio.LineaPedido{VarianteID: id, Cantidad: cantidad})
	}
	if len(lineas) > dominio.MaxItems {
		return nil, []dominio.CampoInvalido{{Campo: "variantes", Mensaje: "demasiadas variantes"}}
	}
	return lineas, nil
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

func (h *Handler) responderVenta(w http.ResponseWriter, r *http.Request, v dominio.Venta, err error) {
	if err != nil {
		escribirError(w, r, err)
		return
	}
	escribirJSON(w, http.StatusOK, h.aJSON(v))
}

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
	case errors.As(err, &inexistente):
		p.Status, p.Codigo, p.Title, p.Variantes = 400, "VARIANTE_INEXISTENTE", "Variante inexistente", inexistente.Variantes
	case errors.As(err, &stock):
		p.Status, p.Codigo, p.Title, p.Faltantes = 409, "STOCK_INSUFICIENTE", "Stock insuficiente", stock.Faltantes
		p.Detail = "Al menos una variante no alcanza. No se apartó ninguna unidad."
	case errors.Is(err, dominio.ErrClaveReutilizada):
		p.Status, p.Codigo, p.Title = 409, "CLAVE_IDEMPOTENCIA_REUTILIZADA", "Clave de idempotencia reutilizada"
	case errors.Is(err, dominio.ErrVentaNoEncontrada):
		p.Status, p.Codigo, p.Title = 404, "VENTA_NO_ENCONTRADA", "Venta no encontrada"
	case errors.Is(err, dominio.ErrTransicionInvalida):
		p.Status, p.Codigo, p.Title = 409, "TRANSICION_INVALIDA", "La venta no admite ese cambio"
	case errors.Is(err, dominio.ErrNoAutenticado), errors.Is(err, dominio.ErrFirmaInvalida):
		p.Status, p.Codigo, p.Title = 401, "NO_AUTENTICADO", "No autenticado"
	case errors.Is(err, dominio.ErrNoAutorizado):
		p.Status, p.Codigo, p.Title = 403, "NO_AUTORIZADO", "No autorizado"
	case errors.Is(err, dominio.ErrPagoNoDisponible):
		w.Header().Set("Retry-After", "5")
		p.Status, p.Codigo, p.Title, p.Detail = 503, "PAGO_NO_DISPONIBLE", "Mercado Pago no está disponible", "Reintentar con la misma Idempotency-Key."
	case errors.Is(err, dominio.ErrInventarioNoDisponible), errors.Is(err, dominio.ErrCatalogoNoDisponible),
		errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		slog.WarnContext(r.Context(), "dependencia no disponible", "error", err)
		w.Header().Set("Retry-After", "2")
		p.Status, p.Codigo, p.Title, p.Detail = 503, "NO_DISPONIBLE", "Servicio no disponible", "Reintentar con la misma Idempotency-Key."
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
