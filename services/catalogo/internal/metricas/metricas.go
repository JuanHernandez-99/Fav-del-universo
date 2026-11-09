// Package metricas tiene las métricas Prometheus de Catálogo (ADR-011), con los mismos nombres que Inventario y Ventas.
package metricas

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	pedidosHTTP = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_pedidos_total",
		Help: "Pedidos HTTP atendidos, por ruta, método y código de estado.",
	}, []string{"ruta", "metodo", "codigo"})

	duracionHTTP = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_duracion_segundos",
		Help:    "Duración de los pedidos HTTP atendidos.",
		Buckets: []float64{.001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
	}, []string{"ruta", "metodo"})

	// CacheOperaciones permite calcular la tasa de aciertos de la caché (ADR-007).
	CacheOperaciones = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cache_operaciones_total",
		Help: "Lecturas de la caché por recurso (ficha, listado, promociones) y resultado (acierto, fallo, error, omitida).",
	}, []string{"recurso", "resultado"})

	// CacheLecturasFuente cuenta cuántas lecturas llegaron a MongoDB por un fallo: con pedidos iguales simultáneos,
	// una sola por réplica.
	CacheLecturasFuente = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cache_lecturas_fuente_total",
		Help: "Lecturas a MongoDB hechas para completar la caché, por recurso.",
	}, []string{"recurso"})

	CacheInvalidaciones = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cache_invalidaciones_total",
		Help: "Invalidaciones de la caché por resultado (ok, error).",
	}, []string{"resultado"})

	CacheEvicciones = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "cache_evicciones",
		Help: "Claves que Redis expulsó por falta de memoria desde que arrancó.",
	})
)

// Middleware registra cada pedido. El router completa Pattern con la ruta registrada: se usa esa y no la URL, para no
// crear una serie de métricas por cada id de producto.
func Middleware(siguiente http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		registro := &respuestaRegistrada{ResponseWriter: w, codigo: http.StatusOK}
		inicio := time.Now()
		siguiente.ServeHTTP(registro, r)
		_, ruta, ok := strings.Cut(r.Pattern, " ")
		if !ok {
			ruta = "sin_ruta"
		}
		if ruta == "/metrics" || ruta == "/healthz" || ruta == "/readyz" {
			return
		}
		pedidosHTTP.WithLabelValues(ruta, r.Method, strconv.Itoa(registro.codigo)).Inc()
		duracionHTTP.WithLabelValues(ruta, r.Method).Observe(time.Since(inicio).Seconds())
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
