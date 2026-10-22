// Mock de Catálogo y Ventas para la Entrega 2. Atiende la capacidad publicada (contrato
// catalogo-pedidos-v1) mientras esos servicios no existen:
//
//   - GET  /v1/productos: catálogo fijo, el mismo del ejemplo del contrato (simula a Catálogo).
//   - POST /v1/pedidos:   valida el pedido, calcula el total con esos precios y aparta el stock
//     llamando DE VERDAD a Inventario (simula a Ventas). Respeta los tiempos y reintentos del ADR-005.
//
// No guarda ventas: la venta pendiente de pago se representa con la reserva de Inventario.
// La idempotencia la garantiza Inventario con la clave del pedido; el mock solo guarda en memoria
// la huella de cada clave para responder CLAVE_IDEMPOTENCIA_REUTILIZADA.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	servicio          = "ventas-mock"
	headerCorrelacion = "X-Correlation-Id"
	vigenciaPedido    = 48 * time.Hour // RN-09: pago por transferencia
)

// ---------- Catálogo fijo (ejemplo del contrato) ----------

type variante struct {
	VarianteID string  `json:"varianteId"`
	Talle      string  `json:"talle"`
	Color      string  `json:"color"`
	Precio     float64 `json:"precio"`
}

type producto struct {
	ProductoID  string     `json:"productoId"`
	Nombre      string     `json:"nombre"`
	Categoria   string     `json:"categoria"`
	Descripcion string     `json:"descripcion,omitempty"`
	FotoURL     string     `json:"fotoUrl,omitempty"`
	Variantes   []variante `json:"variantes"`
}

var catalogo = []producto{
	{"prod-remera-basica", "Remera básica", "remeras", "Remera de algodón, corte recto.", "https://fav.example/fotos/remera-basica.jpg",
		[]variante{{"var-remera-basica-blanca-m", "M", "Blanco", 15000}, {"var-remera-basica-negra-l", "L", "Negro", 15000}}},
	{"prod-jean-mom", "Jean mom", "jeans", "", "",
		[]variante{{"var-jean-mom-celeste-38", "38", "Celeste", 42000}}},
}

func precios() map[string]float64 {
	p := map[string]float64{}
	for _, prod := range catalogo {
		for _, v := range prod.Variantes {
			p[v.VarianteID] = v.Precio
		}
	}
	return p
}

// ---------- Pedidos ----------

type itemPedido struct {
	VarianteID string `json:"varianteId"`
	Cantidad   int    `json:"cantidad"`
}

type pedidoNuevo struct {
	ReferenciaExterna string       `json:"referenciaExterna,omitempty"`
	Items             []itemPedido `json:"items"`
}

type itemConPrecio struct {
	VarianteID     string  `json:"varianteId"`
	Cantidad       int     `json:"cantidad"`
	PrecioUnitario float64 `json:"precioUnitario"`
	Subtotal       float64 `json:"subtotal"`
}

type pedido struct {
	PedidoID          string          `json:"pedidoId"`
	ReferenciaExterna string          `json:"referenciaExterna,omitempty"`
	Estado            string          `json:"estado"`
	Items             []itemConPrecio `json:"items"`
	Total             float64         `json:"total"`
	Moneda            string          `json:"moneda"`
	Pago              pago            `json:"pago"`
	CreadoEn          time.Time       `json:"creadoEn"`
}

type pago struct {
	Medio              string            `json:"medio"`
	VenceEn            time.Time         `json:"venceEn"`
	DatosTransferencia map[string]string `json:"datosTransferencia"`
}

type problema struct {
	Type      string           `json:"type"`
	Title     string           `json:"title"`
	Status    int              `json:"status"`
	Detail    string           `json:"detail,omitempty"`
	Codigo    string           `json:"codigo"`
	Errores   []map[string]any `json:"errores,omitempty"`
	Faltantes []map[string]any `json:"faltantes,omitempty"`
	Variantes []string         `json:"variantes,omitempty"`
}

type mock struct {
	inventarioURL string
	cliente       *http.Client
	precios       map[string]float64

	mu      sync.Mutex
	huellas map[string]string // Idempotency-Key -> huella del cuerpo
}

func (m *mock) crearPedido(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	clave := r.Header.Get("Idempotency-Key")
	var p pedidoNuevo
	decodificador := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decodificador.DisallowUnknownFields()
	if err := decodificador.Decode(&p); err != nil {
		escribirProblema(w, problema{Status: 400, Codigo: "PEDIDO_INVALIDO", Title: "Pedido inválido",
			Errores: []map[string]any{{"campo": "cuerpo", "mensaje": err.Error()}}})
		return
	}
	if errores := validar(clave, p); len(errores) > 0 {
		escribirProblema(w, problema{Status: 400, Codigo: "PEDIDO_INVALIDO", Title: "Pedido inválido", Errores: errores})
		return
	}

	// Precio vigente: en el sistema real, Ventas se lo pide a Catálogo.
	var inexistentes []string
	for _, it := range p.Items {
		if _, ok := m.precios[it.VarianteID]; !ok {
			inexistentes = append(inexistentes, it.VarianteID)
		}
	}
	if len(inexistentes) > 0 {
		escribirProblema(w, problema{Status: 400, Codigo: "VARIANTE_INEXISTENTE", Title: "Variante inexistente",
			Detail: "Variantes que no existen o no están publicadas.", Variantes: inexistentes})
		return
	}

	huella := huellaPedido(p)
	m.mu.Lock()
	anterior, usada := m.huellas[clave]
	if !usada {
		m.huellas[clave] = huella
	}
	m.mu.Unlock()
	if usada && anterior != huella {
		escribirProblema(w, problema{Status: 409, Codigo: "CLAVE_IDEMPOTENCIA_REUTILIZADA", Title: "Clave de idempotencia reutilizada",
			Detail: "La Idempotency-Key ya se usó con un pedido distinto."})
		return
	}

	reserva, prob := m.reservar(ctx, clave, p)
	if prob != nil {
		escribirProblema(w, *prob)
		return
	}

	respuesta := pedido{
		PedidoID:          reserva.ReservaID, // la venta y su reserva comparten id en el mock
		ReferenciaExterna: p.ReferenciaExterna,
		Estado:            "PENDIENTE_DE_PAGO",
		Moneda:            "ARS",
		Pago: pago{Medio: "TRANSFERENCIA", VenceEn: reserva.VenceEn,
			DatosTransferencia: map[string]string{"alias": "FAV.EJEMPLO.ALIAS", "titular": "FAV (ejemplo)"}},
		CreadoEn: reserva.CreadaEn,
	}
	for _, it := range p.Items {
		precio := m.precios[it.VarianteID]
		subtotal := precio * float64(it.Cantidad)
		respuesta.Items = append(respuesta.Items, itemConPrecio{it.VarianteID, it.Cantidad, precio, subtotal})
		respuesta.Total += subtotal
	}
	slog.InfoContext(ctx, "pedido creado", "pedido_id", respuesta.PedidoID, "total", respuesta.Total, "items", len(respuesta.Items))
	escribirJSON(w, http.StatusCreated, respuesta)
}

type reservaInventario struct {
	ReservaID string    `json:"reservaId"`
	VenceEn   time.Time `json:"venceEn"`
	CreadaEn  time.Time `json:"creadaEn"`
}

// reservar llama a Inventario con timeout de 2 s y hasta 2 reintentos (200 y 400 ms más un
// componente aleatorio), solo ante fallas transitorias. Todos los intentos usan la misma clave
// de idempotencia, así que un reintento nunca aparta dos veces (ADR-005, puntos 2 y 3).
func (m *mock) reservar(ctx context.Context, clave string, p pedidoNuevo) (reservaInventario, *problema) {
	cuerpo, _ := json.Marshal(map[string]any{
		"referencia": "pedido-externo:" + p.ReferenciaExterna, "vigenciaMinutos": int(vigenciaPedido.Minutes()), "items": p.Items,
	})
	esperas := []time.Duration{0, 200 * time.Millisecond, 400 * time.Millisecond}
	var ultimoError error
	for intento, espera := range esperas {
		if espera > 0 {
			espera += time.Duration(rand.IntN(100)) * time.Millisecond
			slog.WarnContext(ctx, "reintentando la reserva en Inventario", "intento", intento+1, "espera_ms", espera.Milliseconds(), "error", ultimoError)
			select {
			case <-time.After(espera):
			case <-ctx.Done():
				return reservaInventario{}, noDisponible()
			}
		}
		codigo, contenido, err := m.llamarInventario(ctx, clave, cuerpo)
		if err != nil {
			ultimoError = err
			continue
		}
		switch {
		case codigo == http.StatusCreated:
			var r reservaInventario
			if err := json.Unmarshal(contenido, &r); err != nil {
				return reservaInventario{}, &problema{Status: 500, Codigo: "ERROR_INTERNO", Title: "Error interno"}
			}
			return r, nil
		case codigo == http.StatusBadGateway || codigo == http.StatusServiceUnavailable || codigo == http.StatusGatewayTimeout:
			ultimoError = fmt.Errorf("inventario respondió %d", codigo)
			continue
		default:
			// Error de negocio: definitivo, no se reintenta.
			return reservaInventario{}, traducirError(codigo, contenido)
		}
	}
	slog.ErrorContext(ctx, "inventario no disponible, se agotaron los reintentos", "error", ultimoError)
	return reservaInventario{}, noDisponible()
}

func (m *mock) llamarInventario(ctx context.Context, clave string, cuerpo []byte) (int, []byte, error) {
	ctx, cancelar := context.WithTimeout(ctx, 2*time.Second)
	defer cancelar()
	pedido, err := http.NewRequestWithContext(ctx, http.MethodPost, m.inventarioURL+"/v1/reservas", bytes.NewReader(cuerpo))
	if err != nil {
		return 0, nil, err
	}
	pedido.Header.Set("Content-Type", "application/json")
	pedido.Header.Set("Idempotency-Key", "pedido-externo:"+clave)
	pedido.Header.Set(headerCorrelacion, correlationID(ctx))
	res, err := m.cliente.Do(pedido)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	contenido, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return res.StatusCode, contenido, err
}

// traducirError pasa los errores de Inventario a los del contrato publicado (ADR-008).
func traducirError(codigo int, contenido []byte) *problema {
	var p problema
	_ = json.Unmarshal(contenido, &p)
	switch p.Codigo {
	case "STOCK_INSUFICIENTE":
		faltantes := make([]map[string]any, len(p.Faltantes))
		for i, f := range p.Faltantes {
			// El contrato no informa cuánto stock hay: solo qué variante y cuánto se pidió.
			faltantes[i] = map[string]any{"varianteId": f["varianteId"], "solicitada": f["solicitada"]}
		}
		return &problema{Status: 409, Codigo: p.Codigo, Title: "Stock insuficiente",
			Detail: "Al menos una variante no alcanza. No se apartó ninguna unidad.", Faltantes: faltantes}
	case "VARIANTE_INEXISTENTE":
		return &problema{Status: 400, Codigo: p.Codigo, Title: "Variante inexistente", Variantes: p.Variantes}
	case "CLAVE_IDEMPOTENCIA_REUTILIZADA":
		return &problema{Status: 409, Codigo: p.Codigo, Title: "Clave de idempotencia reutilizada"}
	}
	slog.Error("respuesta inesperada de inventario", "codigo", codigo, "cuerpo", string(contenido))
	return &problema{Status: 500, Codigo: "ERROR_INTERNO", Title: "Error interno"}
}

func noDisponible() *problema {
	return &problema{Status: 503, Codigo: "NO_DISPONIBLE", Title: "Servicio no disponible",
		Detail: "Reintentar con la misma Idempotency-Key."}
}

func validar(clave string, p pedidoNuevo) []map[string]any {
	var errores []map[string]any
	agregar := func(campo, mensaje string) { errores = append(errores, map[string]any{"campo": campo, "mensaje": mensaje}) }
	if len(clave) < 8 || len(clave) > 100 {
		agregar("Idempotency-Key", "es obligatoria, entre 8 y 100 caracteres")
	}
	if len(p.ReferenciaExterna) > 100 {
		agregar("referenciaExterna", "hasta 100 caracteres")
	}
	if len(p.Items) < 1 || len(p.Items) > 20 {
		agregar("items", "debe tener entre 1 y 20 variantes")
	}
	vistas := map[string]bool{}
	for i, it := range p.Items {
		if it.Cantidad < 1 || it.Cantidad > 10 {
			agregar(fmt.Sprintf("items[%d].cantidad", i), "debe estar entre 1 y 10")
		}
		if vistas[it.VarianteID] {
			agregar(fmt.Sprintf("items[%d].varianteId", i), "variante repetida")
		}
		vistas[it.VarianteID] = true
	}
	return errores
}

func huellaPedido(p pedidoNuevo) string {
	items := append([]itemPedido(nil), p.Items...)
	sort.Slice(items, func(a, b int) bool { return items[a].VarianteID < items[b].VarianteID })
	var b strings.Builder
	b.WriteString(p.ReferenciaExterna)
	for _, it := range items {
		fmt.Fprintf(&b, "|%s:%d", it.VarianteID, it.Cantidad)
	}
	suma := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(suma[:])
}

// ---------- HTTP y observabilidad ----------

type claveCorrelacion struct{}

func correlationID(ctx context.Context) string {
	id, _ := ctx.Value(claveCorrelacion{}).(string)
	return id
}

type manejadorConContexto struct{ slog.Handler }

func (m manejadorConContexto) Handle(ctx context.Context, r slog.Record) error {
	if id := correlationID(ctx); id != "" {
		r.AddAttrs(slog.String("correlation_id", id))
	}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()))
	}
	return m.Handler.Handle(ctx, r)
}

func (m manejadorConContexto) WithAttrs(a []slog.Attr) slog.Handler {
	return manejadorConContexto{m.Handler.WithAttrs(a)}
}
func (m manejadorConContexto) WithGroup(n string) slog.Handler {
	return manejadorConContexto{m.Handler.WithGroup(n)}
}

func conCorrelacion(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(headerCorrelacion)
		if id == "" || len(id) > 100 {
			id = uuid.NewString()
		}
		w.Header().Set(headerCorrelacion, id)
		ctx := context.WithValue(r.Context(), claveCorrelacion{}, id)
		inicio := time.Now()
		rw := &respuesta{ResponseWriter: w, codigo: 200}
		h.ServeHTTP(rw, r.WithContext(ctx))
		slog.InfoContext(ctx, "pedido http", "metodo", r.Method, "ruta", r.URL.Path, "codigo", rw.codigo, "duracion_ms", time.Since(inicio).Milliseconds())
	})
}

type respuesta struct {
	http.ResponseWriter
	codigo int
}

func (r *respuesta) WriteHeader(c int) { r.codigo = c; r.ResponseWriter.WriteHeader(c) }

func escribirJSON(w http.ResponseWriter, codigo int, cuerpo any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(codigo)
	_ = json.NewEncoder(w).Encode(cuerpo)
}

func escribirProblema(w http.ResponseWriter, p problema) {
	p.Type = "https://fav.example/errores/" + strings.ToLower(strings.ReplaceAll(p.Codigo, "_", "-"))
	if p.Status == http.StatusServiceUnavailable {
		w.Header().Set("Retry-After", "2")
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

func main() {
	instancia, _ := os.Hostname()
	slog.SetDefault(slog.New(manejadorConContexto{slog.NewJSONHandler(os.Stdout, nil)}).With("servicio", servicio, "instancia", instancia))

	ctx, detener := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer detener()

	otel.SetTextMapPropagator(propagation.TraceContext{})
	opciones := []sdktrace.TracerProviderOption{sdktrace.WithResource(resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(servicio)))}
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		if exp, err := otlptracehttp.New(ctx); err == nil {
			opciones = append(opciones, sdktrace.WithBatcher(exp))
		}
	}
	proveedor := sdktrace.NewTracerProvider(opciones...)
	otel.SetTracerProvider(proveedor)
	defer proveedor.Shutdown(context.Background())

	m := &mock{
		inventarioURL: os.Getenv("INVENTARIO_URL"),
		cliente:       &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)},
		precios:       precios(),
		huellas:       map[string]string{},
	}
	if m.inventarioURL == "" {
		m.inventarioURL = "http://gateway:8081"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /v1/productos", func(w http.ResponseWriter, _ *http.Request) {
		escribirJSON(w, http.StatusOK, map[string]any{"items": catalogo})
	})
	mux.HandleFunc("POST /v1/pedidos", m.crearPedido)

	servidor := &http.Server{
		Addr:              ":8080",
		Handler:           otelhttp.NewHandler(conCorrelacion(mux), "ventas-mock", otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string { return r.Method + " " + r.URL.Path })),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		<-ctx.Done()
		cierre, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelar()
		_ = servidor.Shutdown(cierre)
	}()
	slog.Info("mock de catálogo y ventas escuchando", "direccion", servidor.Addr, "inventario", m.inventarioURL)
	if err := servidor.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("el mock terminó con error", "error", err)
		os.Exit(1)
	}
}
