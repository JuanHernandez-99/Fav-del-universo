// Package observabilidad configura logs estructurados, trazas y métricas (ADR-011).
// Cada línea de log lleva el servicio, el correlation_id y el trace_id del pedido,
// para seguir una operación entre servicios y saltar del log a la traza.
package observabilidad

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
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
func ConfigurarLogs(servicio, instancia string, nivel slog.Level) {
	base := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: nivel})
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

// ConfigurarTrazas exporta las trazas por OTLP/HTTP (a Jaeger en el entorno local).
// Sin endpoint configurado no exporta, pero igual propaga el contexto de la traza.
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

// ---------- Métricas ----------

var (
	pedidosHTTP = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_pedidos_total",
		Help: "Pedidos HTTP atendidos, por ruta, método y código de estado.",
	}, []string{"ruta", "metodo", "codigo"})

	duracionHTTP = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_duracion_segundos",
		Help:    "Duración de los pedidos HTTP atendidos.",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
	}, []string{"ruta", "metodo"})

	LlamadasDependencia = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "dependencia_llamadas_total",
		Help: "Llamadas a dependencias externas, por dependencia y resultado (ok, error, rechazada, circuito_abierto).",
	}, []string{"dependencia", "resultado"})

	EstadoCircuito = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "circuit_breaker_estado",
		Help: "Estado del circuit breaker por dependencia: 0 cerrado, 1 semiabierto, 2 abierto.",
	}, []string{"dependencia"})

	ProcesosEjecutados = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "proceso_elementos_total",
		Help: "Elementos procesados por los procesos en segundo plano (reservas vencidas, reposiciones enviadas).",
	}, []string{"proceso"})
)

// ---------- Middleware HTTP ----------

// Middleware toma o genera el correlation id, abre el span del pedido, registra métricas
// y escribe una línea de log por pedido.
func Middleware(siguiente http.Handler) http.Handler {
	conMetricas := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(HeaderCorrelacion)
		if id == "" || len(id) > 100 {
			id = uuid.NewString()
		}
		ctx := ConCorrelationID(r.Context(), id)
		trace.SpanFromContext(ctx).SetAttributes(attribute.String("correlation_id", id))
		w.Header().Set(HeaderCorrelacion, id)

		rw := &respuestaRegistrada{ResponseWriter: w, codigo: http.StatusOK}
		inicio := time.Now()
		pedido := r.WithContext(ctx)
		siguiente.ServeHTTP(rw, pedido)
		duracion := time.Since(inicio)

		// El router completa Pattern con la ruta registrada. Se usa esa y no la URL,
		// para no crear una serie de métricas por cada id.
		ruta := pedido.Pattern
		if ruta == "" {
			ruta = "sin_ruta"
		} else {
			trace.SpanFromContext(ctx).SetName(ruta)
			if _, sinMetodo, ok := strings.Cut(ruta, " "); ok {
				ruta = sinMetodo
			}
		}
		if ruta == "/metrics" || ruta == "/healthz" || ruta == "/readyz" {
			return // las sondas no ensucian los logs ni las métricas
		}
		pedidosHTTP.WithLabelValues(ruta, r.Method, strconv.Itoa(rw.codigo)).Inc()
		duracionHTTP.WithLabelValues(ruta, r.Method).Observe(duracion.Seconds())

		nivel := slog.LevelInfo
		if rw.codigo >= 500 {
			nivel = slog.LevelError
		}
		slog.Log(ctx, nivel, "pedido http", "metodo", r.Method, "ruta", ruta, "codigo", rw.codigo, "duracion_ms", duracion.Milliseconds())
	})
	return otelhttp.NewHandler(conMetricas, "http",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string { return r.Method + " " + r.URL.Path }))
}

type respuestaRegistrada struct {
	http.ResponseWriter
	codigo int
}

func (r *respuestaRegistrada) WriteHeader(codigo int) {
	r.codigo = codigo
	r.ResponseWriter.WriteHeader(codigo)
}

// ClienteHTTP devuelve un cliente que propaga la traza y el correlation id en cada llamada saliente.
func ClienteHTTP(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: otelhttp.NewTransport(transporteConCorrelacion{http.DefaultTransport}),
	}
}

type transporteConCorrelacion struct{ base http.RoundTripper }

func (t transporteConCorrelacion) RoundTrip(r *http.Request) (*http.Response, error) {
	if id := CorrelationID(r.Context()); id != "" && r.Header.Get(HeaderCorrelacion) == "" {
		r = r.Clone(r.Context())
		r.Header.Set(HeaderCorrelacion, id)
	}
	return t.base.RoundTrip(r)
}
