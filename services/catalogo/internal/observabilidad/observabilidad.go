// Package observabilidad configura logs estructurados y trazas de Catálogo (ADR-011), igual que en Ventas e Inventario.
// Cada línea de log lleva el correlation_id y el trace_id del pedido, para saltar del log a la traza en Jaeger.
// Las métricas están en el paquete metricas.
package observabilidad

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/embedded"
)

const HeaderCorrelacion = "X-Correlation-Id"

type claveCorrelacion struct{}

func ConCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, claveCorrelacion{}, id)
}

func CorrelationID(ctx context.Context) string {
	id, _ := ctx.Value(claveCorrelacion{}).(string)
	return id
}

// ---------- Logs ----------

// ConfigurarLogs deja slog escribiendo JSON en stdout, con los datos del contexto en cada línea.
func ConfigurarLogs(servicio, instancia string) {
	base := slog.NewJSONHandler(os.Stdout, nil)
	slog.SetDefault(slog.New(manejadorConContexto{base}).With("servicio", servicio, "instancia", instancia))
}

type manejadorConContexto struct{ slog.Handler }

func (m manejadorConContexto) Handle(ctx context.Context, r slog.Record) error {
	if id := CorrelationID(ctx); id != "" {
		r.AddAttrs(slog.String("correlation_id", id))
	}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(slog.String("trace_id", sc.TraceID().String()), slog.String("span_id", sc.SpanID().String()))
	}
	return m.Handler.Handle(ctx, r)
}

func (m manejadorConContexto) WithAttrs(attrs []slog.Attr) slog.Handler {
	return manejadorConContexto{m.Handler.WithAttrs(attrs)}
}

func (m manejadorConContexto) WithGroup(nombre string) slog.Handler {
	return manejadorConContexto{m.Handler.WithGroup(nombre)}
}

// ---------- Trazas ----------

// ConfigurarTrazas exporta las trazas por OTLP/HTTP (a Jaeger en Docker Compose).
// Sin OTEL_EXPORTER_OTLP_ENDPOINT no exporta, pero igual propaga el contexto: los tests no necesitan Jaeger.
func ConfigurarTrazas(ctx context.Context, servicio, instancia string) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	recurso := resource.NewWithAttributes(semconv.SchemaURL,
		semconv.ServiceName(servicio), semconv.ServiceInstanceID(instancia))
	opciones := []sdktrace.TracerProviderOption{sdktrace.WithResource(recurso)}

	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		exportador, err := otlptracehttp.New(ctx) // toma el endpoint de la variable de entorno estándar
		if err != nil {
			return nil, err
		}
		opciones = append(opciones, sdktrace.WithBatcher(exportador))
	}
	proveedor := sdktrace.NewTracerProvider(opciones...)
	otel.SetTracerProvider(proveedor)
	return proveedor.Shutdown, nil
}

// tracer devuelve un tracer que solo abre spans dentro de una operación que ya tiene traza. Así las consultas de los
// procesos periódicos (avisar cambios cada segundo, preparar el índice) no llenan Jaeger de trazas sueltas (ADR-011).
func tracer() trace.Tracer {
	return tracerConPadre{base: otel.Tracer("catalogo")}
}

type tracerConPadre struct {
	embedded.Tracer
	base trace.Tracer
}

func (t tracerConPadre) Start(ctx context.Context, nombre string, opciones ...trace.SpanStartOption) (context.Context, trace.Span) {
	if !trace.SpanContextFromContext(ctx).IsValid() {
		return ctx, trace.SpanFromContext(ctx) // span vacío: no registra nada
	}
	return t.base.Start(ctx, nombre, opciones...)
}

type proveedorConPadre struct{ embedded.TracerProvider }

func (proveedorConPadre) Tracer(string, ...trace.TracerOption) trace.Tracer { return tracer() }

// Operacion abre la raíz de una traza para trabajo que no llega por HTTP, como indexar un producto al consumir un
// evento. fin registra el error, si hubo, y cierra el span.
func Operacion(ctx context.Context, nombre string, atributos ...attribute.KeyValue) (context.Context, func(error)) {
	ctx, span := otel.Tracer("catalogo").Start(ctx, nombre, trace.WithAttributes(atributos...))
	return ctx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}
}

// ---------- HTTP ----------

// Middleware toma o genera el correlation id, abre el span del pedido y escribe una línea de log por pedido.
func Middleware(siguiente http.Handler) http.Handler {
	conLog := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(HeaderCorrelacion)
		if id == "" || len(id) > 100 {
			id = uuid.NewString()
		}
		ctx := ConCorrelationID(r.Context(), id)
		span := trace.SpanFromContext(ctx)
		span.SetAttributes(attribute.String("correlation_id", id))
		w.Header().Set(HeaderCorrelacion, id)

		rw := &respuestaRegistrada{ResponseWriter: w, codigo: http.StatusOK}
		inicio := time.Now()
		pedido := r.WithContext(ctx)
		siguiente.ServeHTTP(rw, pedido)

		// El router completa Pattern con la ruta registrada: el span se nombra con ella y no con la URL, que lleva ids.
		ruta := pedido.Pattern
		if ruta == "" {
			ruta = "sin_ruta"
		} else {
			span.SetName(ruta)
			if _, sinMetodo, ok := strings.Cut(ruta, " "); ok {
				ruta = sinMetodo
			}
		}
		if ruta == "/metrics" || ruta == "/healthz" || ruta == "/readyz" {
			return // las sondas no ensucian los logs
		}
		nivel := slog.LevelInfo
		if rw.codigo >= 500 {
			nivel = slog.LevelError
		}
		slog.Log(ctx, nivel, "pedido http", "metodo", r.Method, "ruta", ruta, "codigo", rw.codigo,
			"duracion_ms", time.Since(inicio).Milliseconds())
	})
	return otelhttp.NewHandler(conLog, "http",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string { return r.Method + " " + r.URL.Path }),
		// Las sondas de Docker y el scrape de Prometheus no abren trazas.
		otelhttp.WithFilter(func(r *http.Request) bool {
			return r.URL.Path != "/healthz" && r.URL.Path != "/readyz" && r.URL.Path != "/metrics"
		}))
}

type respuestaRegistrada struct {
	http.ResponseWriter
	codigo int
}

func (r *respuestaRegistrada) WriteHeader(codigo int) {
	r.codigo = codigo
	r.ResponseWriter.WriteHeader(codigo)
}

// Transporte agrega un span por llamada saliente y propaga la traza y el correlation id. destino nombra el span
// ("elasticsearch POST /productos/_search"). Solo traza si la llamada es parte de una operación.
func Transporte(base http.RoundTripper, destino string) http.RoundTripper {
	return otelhttp.NewTransport(transporteConCorrelacion{base},
		otelhttp.WithTracerProvider(proveedorConPadre{}),
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return destino + " " + r.Method + " " + r.URL.Path
		}))
}

type transporteConCorrelacion struct{ base http.RoundTripper }

func (t transporteConCorrelacion) RoundTrip(r *http.Request) (*http.Response, error) {
	if id := CorrelationID(r.Context()); id != "" && r.Header.Get(HeaderCorrelacion) == "" {
		r = r.Clone(r.Context())
		r.Header.Set(HeaderCorrelacion, id)
	}
	return t.base.RoundTrip(r)
}
