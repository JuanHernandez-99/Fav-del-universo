// Package observabilidad configura los logs estructurados de Catálogo y el track id (ADR-020), igual que en Ventas
// e Inventario. Cada línea de log lleva el correlation_id (el track id) del pedido; las de INFO o más también van a
// app_logs (logs_mysql.go). Las métricas están en el paquete metricas.
package observabilidad

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
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

// ConfigurarLogs deja slog escribiendo JSON en stdout, con el track id del contexto en cada línea.
func ConfigurarLogs(servicio, instancia string) {
	base := slog.NewJSONHandler(os.Stdout, nil)
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

// ---------- HTTP ----------

// Middleware toma el track id del pedido (o genera uno), lo devuelve en la respuesta y escribe una línea de log
// por pedido.
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

		// El router completa Pattern con la ruta registrada: se loguea esa y no la URL, que lleva ids.
		ruta := pedido.Pattern
		if ruta == "" {
			ruta = "sin_ruta"
		} else if _, sinMetodo, ok := strings.Cut(ruta, " "); ok {
			ruta = sinMetodo
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
}

type respuestaRegistrada struct {
	http.ResponseWriter
	codigo int
}

func (r *respuestaRegistrada) WriteHeader(codigo int) {
	r.codigo = codigo
	r.ResponseWriter.WriteHeader(codigo)
}

// Transporte propaga el track id en cada llamada saliente (a Elasticsearch, por ejemplo), como encabezado
// X-Correlation-Id.
func Transporte(base http.RoundTripper) http.RoundTripper {
	return transporteConCorrelacion{base}
}

type transporteConCorrelacion struct{ base http.RoundTripper }

func (t transporteConCorrelacion) RoundTrip(r *http.Request) (*http.Response, error) {
	if id := CorrelationID(r.Context()); id != "" && r.Header.Get(HeaderCorrelacion) == "" {
		r = r.Clone(r.Context())
		r.Header.Set(HeaderCorrelacion, id)
	}
	return t.base.RoundTrip(r)
}
