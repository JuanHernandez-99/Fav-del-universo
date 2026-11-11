// Package observabilidad configura los logs estructurados, el track id (ADR-020) y las métricas (ADR-011).
// Cada línea de log lleva el servicio y el correlation_id (el track id) del pedido, para seguir una operación
// entre servicios; las de INFO o más también van a app_logs (logs_mysql.go).
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
)

const HeaderCorrelacion = "X-Correlation-Id"

// HeaderTrackID es el nombre del modelo de la cátedra (ADR-020). Se acepta de entrada si no viene X-Correlation-Id.
const HeaderTrackID = "X-Track-ID"

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
	return m.Handler.Handle(ctx, r)
}

func (m manejadorConContexto) WithAttrs(attrs []slog.Attr) slog.Handler {
	return manejadorConContexto{m.Handler.WithAttrs(attrs)}
}

func (m manejadorConContexto) WithGroup(nombre string) slog.Handler {
	return manejadorConContexto{m.Handler.WithGroup(nombre)}
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
)

// ---------- Middleware HTTP ----------

// Middleware toma el track id del pedido (o genera uno), lo devuelve en la respuesta, registra métricas
// y escribe una línea de log por pedido.
func Middleware(siguiente http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(HeaderCorrelacion)
		if id == "" {
			id = r.Header.Get(HeaderTrackID)
		}
		if id == "" || len(id) > 100 {
			id = uuid.NewString()
		}
		ctx := ConCorrelationID(r.Context(), id)
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
		} else if _, sinMetodo, ok := strings.Cut(ruta, " "); ok {
			ruta = sinMetodo
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
}

type respuestaRegistrada struct {
	http.ResponseWriter
	codigo int
}

func (r *respuestaRegistrada) WriteHeader(codigo int) {
	r.codigo = codigo
	r.ResponseWriter.WriteHeader(codigo)
}
