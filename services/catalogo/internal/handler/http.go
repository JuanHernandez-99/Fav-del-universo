// Package handler es la capa HTTP: rutas, JSON de entrada y salida, y traducción de errores a
// códigos HTTP. No conoce MongoDB. Rutas de administración bajo /v1/admin (D14).
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/metricas"
	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/observabilidad"
	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/service"
)

type Handler struct {
	s *service.Servicio
}

// Opcion ajusta qué rutas expone el router.
type Opcion func(*opciones)

type opciones struct{ sinCapacidadPublica bool }

// SinCapacidadPublica apaga GET /v1/productos, para cuando el sistema pase al cliente (ADR-008).
func SinCapacidadPublica() Opcion { return func(o *opciones) { o.sinCapacidadPublica = true } }

func NuevoRouter(s *service.Servicio, opcionesRouter ...Opcion) http.Handler {
	var o opciones
	for _, aplicar := range opcionesRouter {
		aplicar(&o)
	}
	h := &Handler{s: s}
	mux := http.NewServeMux()

	// healthz: el proceso está vivo. readyz: además la base responde, así que puede recibir pedidos.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", h.listo)
	mux.Handle("GET /metrics", promhttp.Handler())

	mux.HandleFunc("GET /v1/admin/productos", h.listarProductos)
	mux.HandleFunc("POST /v1/admin/productos", h.crearProducto)
	mux.HandleFunc("GET /v1/admin/productos/{id}", h.obtenerProducto)
	mux.HandleFunc("PUT /v1/admin/productos/{id}", h.actualizarProducto)
	mux.HandleFunc("DELETE /v1/admin/productos/{id}", h.despublicarProducto)
	mux.HandleFunc("POST /v1/admin/productos/{id}/publicar", h.publicarProducto)
	mux.HandleFunc("POST /v1/admin/productos/{id}/variantes", h.agregarVariante)
	mux.HandleFunc("POST /v1/admin/busqueda/reconstruir", h.reconstruirBusqueda)

	// Consulta por lote: la usan Ventas (precio y datos de la variante) y el panel.
	mux.HandleFunc("GET /v1/variantes", h.consultarVariantes)

	mux.HandleFunc("GET /v1/promociones", h.listarPromociones)
	mux.HandleFunc("POST /v1/promociones", h.crearPromocion)
	mux.HandleFunc("PUT /v1/promociones/{id}", h.actualizarPromocion)

	mux.HandleFunc("GET /v1/tienda/productos", h.listarTienda)
	mux.HandleFunc("GET /v1/tienda/productos/{id}", h.obtenerDeTienda)

	if !o.sinCapacidadPublica {
		mux.HandleFunc("GET /v1/productos", h.listarCatalogoPublico)
	}

	// Track id y log por pedido (ADR-020), y adentro las métricas. Van pegados al router porque él
	// completa la ruta registrada (r.Pattern) en el mismo pedido que recibe: los dos la usan.
	return observabilidad.Middleware(metricas.Middleware(mux))
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
// También es false si el monto supera montoMaximoPesos: más allá, convertir a int64 desborda.
func aCentavos(pesos float64) (centavos int64, ok bool) {
	if math.Abs(pesos) > montoMaximoPesos {
		return 0, false
	}
	redondeado := math.Round(pesos * 100)
	return int64(redondeado), math.Abs(pesos*100-redondeado) < 1e-6
}

// datosJSON son los campos editables, comunes al POST y al PUT. Los pedidos no llevan ids ni
// publicado: los pone el servidor, y como se rechazan los campos desconocidos, no se pueden forzar.
type datosJSON struct {
	Nombre      string `json:"nombre"`
	Descripcion string `json:"descripcion"`
	Categoria   string `json:"categoria"`
	PrecioBase  struct {
		Monto  float64 `json:"monto"`
		Moneda string  `json:"moneda"`
	} `json:"precioBase"`
	Atributos map[string]any `json:"atributos"`
	Fotos     []fotoJSON     `json:"fotos"`
}

// aModelo convierte el monto de pesos a centavos; falla si tiene más de 2 decimales.
func (d datosJSON) aModelo() (modelo.DatosProducto, error) {
	monto, ok := aCentavos(d.PrecioBase.Monto)
	if !ok {
		v := &modelo.ErrorValidacion{}
		v.Agregar("precioBase.monto", "admite hasta 2 decimales y un máximo de %d pesos", montoMaximoPesos)
		return modelo.DatosProducto{}, v
	}
	datos := modelo.DatosProducto{
		Nombre:      d.Nombre,
		Descripcion: d.Descripcion,
		Categoria:   d.Categoria,
		PrecioBase:  modelo.Precio{Monto: monto, Moneda: d.PrecioBase.Moneda},
		Atributos:   d.Atributos,
	}
	for _, f := range d.Fotos {
		datos.Fotos = append(datos.Fotos, modelo.Foto{URL: f.URL, Color: f.Color})
	}
	return datos, nil
}

type pedidoProducto struct {
	datosJSON
	SistemaTalles string `json:"sistemaTalles"`
	Variantes     []struct {
		Talle string `json:"talle"`
		Color string `json:"color"`
	} `json:"variantes"`
}

// pedidoActualizacion lleva la version que se leyó (control optimista). No admite sistemaTalles,
// variantes ni publicado.
type pedidoActualizacion struct {
	datosJSON
	Version int `json:"version"`
}

func (h *Handler) crearProducto(w http.ResponseWriter, r *http.Request) {
	var pedido pedidoProducto
	if !leerJSON(w, r, &pedido) {
		return
	}
	datos, err := pedido.aModelo()
	if err != nil {
		escribirError(w, r, err)
		return
	}
	nuevo := service.NuevoProducto{DatosProducto: datos, SistemaTalles: pedido.SistemaTalles}
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

// agregarVariante es la operación atómica de DISENO.md 5.9: 201 si se agregó, 409 si el
// producto ya tenía ese talle y color, 404 si el producto no existe.
func (h *Handler) agregarVariante(w http.ResponseWriter, r *http.Request) {
	var pedido struct {
		Talle string `json:"talle"`
		Color string `json:"color"`
	}
	if !leerJSON(w, r, &pedido) {
		return
	}
	productoID := r.PathValue("id")
	v, err := h.s.AgregarVariante(r.Context(), productoID, service.NuevaVariante{Talle: pedido.Talle, Color: pedido.Color})
	if err != nil {
		escribirError(w, r, err)
		return
	}
	w.Header().Set("Location", "/v1/admin/productos/"+productoID)
	escribirJSON(w, http.StatusCreated, varianteJSON{v.ID, v.Talle, v.Color})
}

func (h *Handler) obtenerProducto(w http.ResponseWriter, r *http.Request) {
	p, err := h.s.ObtenerProducto(r.Context(), r.PathValue("id"))
	responderProducto(w, r, p, err)
}

// montoMaximoPesos acota los montos que se aceptan, para que en centavos entren en un int64.
const montoMaximoPesos = 1_000_000_000

// paginaJSON es la respuesta del listado: la lista va dentro de items, igual que en Inventario y
// en el contrato.
type paginaJSON struct {
	Items   []productoJSON `json:"items"`
	Pagina  int            `json:"pagina"`
	Tamano  int            `json:"tamano"`
	Total   int64          `json:"total"`
	Paginas int64          `json:"paginas"`
}

func (h *Handler) listarProductos(w http.ResponseWriter, r *http.Request) {
	f, err := parsearFiltro(r.URL.Query())
	if err != nil {
		escribirError(w, r, err)
		return
	}
	productos, total, err := h.s.ListarProductos(r.Context(), f)
	if err != nil {
		escribirError(w, r, err)
		return
	}
	items := make([]productoJSON, len(productos)) // vacío, no null, cuando no hay resultados
	for i, p := range productos {
		items[i] = productoAJSON(p)
	}
	escribirJSON(w, http.StatusOK, paginaJSON{
		Items: items, Pagina: f.Pagina, Tamano: f.Tamano, Total: total,
		Paginas: (total + int64(f.Tamano) - 1) / int64(f.Tamano),
	})
}

var parametrosListado = map[string]bool{
	"categoria": true, "talle": true, "color": true, "precioMin": true, "precioMax": true,
	"q": true, "publicado": true, "orden": true, "pagina": true, "tamano": true,
}

// parsearFiltro revisa el formato de cada parámetro y junta todos los errores. Rechaza los
// parámetros que no existen: un `categorias=jeans` con una letra de más, ignorado, devolvería todo
// el catálogo sin avisar. Los precios llegan en pesos y se pasan a centavos.
func parsearFiltro(q url.Values) (modelo.FiltroProductos, error) {
	v := &modelo.ErrorValidacion{}
	f := modelo.FiltroProductos{
		Categoria: strings.TrimSpace(q.Get("categoria")),
		Talle:     strings.ToUpper(strings.TrimSpace(q.Get("talle"))), // igual que al crear (punto 16)
		Color:     strings.TrimSpace(q.Get("color")),
		Texto:     strings.TrimSpace(q.Get("q")),
		Orden:     modelo.Orden(strings.TrimSpace(q.Get("orden"))),
		Pagina:    1,
		Tamano:    modelo.TamanoPorDefecto,
	}
	if f.Orden == "" {
		f.Orden = modelo.OrdenNuevos
		if f.Texto != "" {
			f.Orden = modelo.OrdenRelevancia
		}
	}

	for _, nombre := range slices.Sorted(maps.Keys(q)) {
		if !parametrosListado[nombre] {
			v.Agregar(nombre, "parámetro desconocido")
		}
	}

	precio := func(nombre string) *int64 {
		texto := strings.TrimSpace(q.Get(nombre))
		if texto == "" {
			return nil
		}
		pesos, err := strconv.ParseFloat(texto, 64)
		centavos, ok := aCentavos(pesos)
		if err != nil || !ok || centavos < 0 {
			v.Agregar(nombre, "tiene que ser un monto en pesos, de 0 a %d, con hasta 2 decimales", montoMaximoPesos)
			return nil
		}
		return &centavos
	}
	f.PrecioMin, f.PrecioMax = precio("precioMin"), precio("precioMax")

	switch strings.ToLower(strings.TrimSpace(q.Get("publicado"))) {
	case "":
	case "true":
		f.Publicado = new(true)
	case "false":
		f.Publicado = new(false)
	default:
		v.Agregar("publicado", "tiene que ser true o false")
	}

	entero := func(nombre string, destino *int) {
		texto := strings.TrimSpace(q.Get(nombre))
		if texto == "" {
			return
		}
		n, err := strconv.Atoi(texto)
		if err != nil {
			v.Agregar(nombre, "tiene que ser un número entero")
			return
		}
		*destino = n // el rango lo valida el servicio
	}
	entero("pagina", &f.Pagina)
	entero("tamano", &f.Tamano)

	return f, v.ComoError()
}

func (h *Handler) actualizarProducto(w http.ResponseWriter, r *http.Request) {
	var pedido pedidoActualizacion
	if !leerJSON(w, r, &pedido) {
		return
	}
	datos, err := pedido.aModelo()
	if err != nil {
		escribirError(w, r, err)
		return
	}
	p, err := h.s.ActualizarProducto(r.Context(), r.PathValue("id"), pedido.Version, datos)
	responderProducto(w, r, p, err)
}

func (h *Handler) publicarProducto(w http.ResponseWriter, r *http.Request) {
	p, err := h.s.Publicar(r.Context(), r.PathValue("id"))
	responderProducto(w, r, p, err)
}

// despublicarProducto es el DELETE: baja lógica, 204 e idempotente (D2).
func (h *Handler) despublicarProducto(w http.ResponseWriter, r *http.Request) {
	if err := h.s.Despublicar(r.Context(), r.PathValue("id")); err != nil {
		escribirError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func responderProducto(w http.ResponseWriter, r *http.Request, p modelo.Producto, err error) {
	if err != nil {
		escribirError(w, r, err)
		return
	}
	escribirJSON(w, http.StatusOK, productoAJSON(p))
}

// ---------- Consulta por lote ----------

// detalleVarianteJSON es una variante con los datos de su producto. Los precios van en pesos con 2
// decimales, como el contrato. precio es el que se cobra (service.PrecioVigente); promocion explica
// la diferencia con precioBase y no viene si no se aplicó ninguna.
type detalleVarianteJSON struct {
	VarianteID string         `json:"varianteId"`
	ProductoID string         `json:"productoId"`
	Nombre     string         `json:"nombre"`
	Categoria  string         `json:"categoria"`
	Talle      string         `json:"talle"`
	Color      string         `json:"color"`
	Publicado  bool           `json:"publicado"`
	Precio     float64        `json:"precio"`
	PrecioBase float64        `json:"precioBase"`
	Promocion  *promocionJSON `json:"promocion,omitempty"`
}

type promocionJSON struct {
	PromocionID string `json:"promocionId"`
	Nombre      string `json:"nombre"`
	Porcentaje  int    `json:"porcentaje"`
}

// consultarVariantes atiende GET /v1/variantes?variantes=a,b,c[&medioPago=EFECTIVO]. Rechaza los
// parámetros desconocidos, igual que el listado. El rango de ids y el medio de pago los valida el servicio.
func (h *Handler) consultarVariantes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	v := &modelo.ErrorValidacion{}
	for _, nombre := range slices.Sorted(maps.Keys(q)) {
		if nombre != "variantes" && nombre != "medioPago" {
			v.Agregar(nombre, "parámetro desconocido")
		}
	}
	if err := v.ComoError(); err != nil {
		escribirError(w, r, err)
		return
	}

	var ids []string
	if lista := q.Get("variantes"); lista != "" {
		ids = strings.Split(lista, ",")
	}
	detalles, err := h.s.ConsultarVariantes(r.Context(), ids, strings.ToUpper(strings.TrimSpace(q.Get("medioPago"))))
	if err != nil {
		escribirError(w, r, err)
		return
	}
	items := make([]detalleVarianteJSON, len(detalles))
	for i, d := range detalles {
		items[i] = detalleVarianteJSON{
			VarianteID: d.VarianteID, ProductoID: d.ProductoID, Nombre: d.Nombre, Categoria: d.Categoria,
			Talle: d.Talle, Color: d.Color, Publicado: d.Publicado,
			Precio: pesos(d.Precio), PrecioBase: pesos(d.PrecioBase.Monto),
		}
		if p := d.Promocion; p != nil {
			items[i].Promocion = &promocionJSON{p.ID, p.Nombre, p.Porcentaje}
		}
	}
	escribirJSON(w, http.StatusOK, struct {
		Items []detalleVarianteJSON `json:"items"`
	}{items})
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
	// Variantes lista los ids que no existen (VARIANTE_INEXISTENTE).
	Variantes []string `json:"variantes,omitempty"`
}

func escribirError(w http.ResponseWriter, r *http.Request, err error) {
	p := problema{Detail: err.Error()}
	var validacion *modelo.ErrorValidacion
	var inexistentes *modelo.ErrVariantesInexistentes
	switch {
	case errors.As(err, &validacion):
		p.Status, p.Codigo, p.Title, p.Errores = 400, "PEDIDO_INVALIDO", "Pedido inválido", validacion.Campos
		p.Detail = "Revisar los campos indicados en errores."
	case errors.As(err, &inexistentes):
		// 400 y no 404: así lo pide el contrato para el lote, que es lo que va a consumir Ventas (D15).
		p.Status, p.Codigo, p.Title, p.Variantes = 400, "VARIANTE_INEXISTENTE", "Variante inexistente", inexistentes.IDs
		p.Detail = "No existen las variantes indicadas en variantes."
	case errors.Is(err, modelo.ErrVarianteDuplicada):
		p.Status, p.Codigo, p.Title = 409, "VARIANTE_DUPLICADA", "Variante duplicada"
		p.Detail = "Ya existe una variante con ese talle y color, o con el mismo id."
	case errors.Is(err, modelo.ErrConflictoDeVersion):
		p.Status, p.Codigo, p.Title = 409, "CONFLICTO_DE_VERSION", "Conflicto de versión"
		p.Detail = "El producto cambió desde que se leyó. Volver a leerlo y repetir el cambio con la versión nueva."
	case errors.Is(err, modelo.ErrProductoNoEncontrado):
		p.Status, p.Codigo, p.Title = 404, "PRODUCTO_NO_ENCONTRADO", "Producto no encontrado"
		p.Detail = "No existe el producto " + r.PathValue("id") + "."
	case errors.Is(err, modelo.ErrPromocionNoEncontrada):
		p.Status, p.Codigo, p.Title = 404, "PROMOCION_NO_ENCONTRADA", "Promoción no encontrada"
		p.Detail = "No existe la promoción " + r.PathValue("id") + "."
	case errors.Is(err, modelo.ErrBusquedaNoDisponible):
		w.Header().Set("Retry-After", "5")
		p.Status, p.Codigo, p.Title = 503, "BUSQUEDA_NO_DISPONIBLE", "Búsqueda no disponible"
		p.Detail = "La búsqueda por texto no está disponible. Se puede navegar por categoría."
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
