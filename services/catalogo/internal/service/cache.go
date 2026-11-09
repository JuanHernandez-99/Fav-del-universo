package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/metricas"
	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
)

// Cache es lo que el servicio necesita de la caché (ADR-007). La implementa cache.Cliente.
type Cache interface {
	Generacion(ctx context.Context) (int64, error)
	SubirGeneracion(ctx context.Context) error
	Leer(ctx context.Context, clave string) ([]byte, bool, error)
	Guardar(ctx context.Context, clave string, valor []byte, vencimiento time.Duration) error
}

// ConCache hace que las lecturas de la tienda pasen por la caché.
func ConCache(c Cache) Opcion {
	return func(s *Servicio) { s.cache = c }
}

const (
	vencimientoCache = 5 * time.Minute
	tiempoFuente     = 3 * time.Second
	tiempoInvalidar  = time.Second
	// pausaCache: tras un error de Redis no se lo vuelve a intentar por este tiempo. Sin esto, con Redis caído cada
	// lectura pagaría su tiempo de espera, y una caché caída empeoraría el servicio en lugar de solo no mejorarlo.
	pausaCache = 5 * time.Second
)

// listadoCacheado es lo que se guarda de una página de la tienda.
type listadoCacheado struct {
	Productos []modelo.Producto
	Total     int64
}

// leerConCache es cache-aside: busca en Redis y, si no está, lee la fuente y lo guarda. Si Redis falla, va a la fuente y
// no devuelve error. Cada clave lleva la generación actual: invalidar es subirla (ver invalidarCache).
//
// singleflight colapsa los pedidos iguales simultáneos de esta réplica en una sola lectura a MongoDB, para que un fallo
// de caché en un pico no se convierta en una avalancha sobre la base. La lectura no usa el contexto del pedido: si ese
// comprador cancela, no tiene que fallar para los demás que esperan el mismo resultado.
func leerConCache[T any](ctx context.Context, s *Servicio, recurso, clave string, fuente func(context.Context) (T, error)) (T, error) {
	var cero T
	if s.cache == nil {
		return fuente(ctx)
	}
	if time.Now().UnixNano() < s.cachePausadaHasta.Load() {
		metricas.CacheOperaciones.WithLabelValues(recurso, "omitida").Inc()
		return leerFuenteColapsada(ctx, s, recurso, "sin-cache:"+clave, fuente, nil)
	}
	generacion, err := s.cache.Generacion(ctx)
	if err != nil {
		s.pausarCache(ctx, recurso, err)
		return leerFuenteColapsada(ctx, s, recurso, "sin-cache:"+clave, fuente, nil)
	}
	claveCompleta := fmt.Sprintf("catalogo:v1:g%d:%s", generacion, clave)

	guardado, encontrado, err := s.cache.Leer(ctx, claveCompleta)
	switch {
	case err != nil:
		s.pausarCache(ctx, recurso, err)
		return leerFuenteColapsada(ctx, s, recurso, "sin-cache:"+clave, fuente, nil)
	case encontrado:
		var valor T
		if err := json.Unmarshal(guardado, &valor); err == nil {
			metricas.CacheOperaciones.WithLabelValues(recurso, "acierto").Inc()
			return valor, nil
		}
		// Un formato viejo o dañado cuenta como fallo: se reemplaza con el dato de la fuente.
		slog.WarnContext(ctx, "entrada de caché inválida", "clave", claveCompleta)
	}
	metricas.CacheOperaciones.WithLabelValues(recurso, "fallo").Inc()
	valor, err := leerFuenteColapsada(ctx, s, recurso, claveCompleta, fuente, func(ctx context.Context, v T) {
		datos, err := json.Marshal(v)
		if err == nil {
			err = s.cache.Guardar(ctx, claveCompleta, datos, vencimientoConVariacion())
		}
		if err != nil {
			slog.WarnContext(ctx, "no se pudo guardar en la caché", "clave", claveCompleta, "error", err)
		}
	})
	if err != nil {
		return cero, err
	}
	return valor, nil
}

func (s *Servicio) pausarCache(ctx context.Context, recurso string, err error) {
	metricas.CacheOperaciones.WithLabelValues(recurso, "error").Inc()
	s.cachePausadaHasta.Store(time.Now().Add(pausaCache).UnixNano())
	slog.WarnContext(ctx, "caché no disponible; se lee de MongoDB", "recurso", recurso, "pausa", pausaCache, "error", err)
}

func leerFuenteColapsada[T any](ctx context.Context, s *Servicio, recurso, clave string, fuente func(context.Context) (T, error), guardar func(context.Context, T)) (T, error) {
	var cero T
	resultado, err, _ := s.colapsar.Do(clave, func() (any, error) {
		ctxFuente, cancelar := context.WithTimeout(context.WithoutCancel(ctx), tiempoFuente)
		defer cancelar()
		metricas.CacheLecturasFuente.WithLabelValues(recurso).Inc()
		valor, err := fuente(ctxFuente)
		if err != nil {
			return nil, err
		}
		if guardar != nil {
			guardar(ctxFuente, valor)
		}
		return valor, nil
	})
	if err != nil {
		return cero, err
	}
	return resultado.(T), nil
}

// vencimientoConVariacion evita que las claves cargadas juntas venzan juntas.
func vencimientoConVariacion() time.Duration {
	return vencimientoCache + rand.N(vencimientoCache/5)
}

// invalidarCache sube la generación después de escribir en MongoDB. Si alguien estaba leyendo el dato viejo, lo guarda
// con la generación anterior, que ya nadie lee: esa es la ventaja frente a borrar la clave. Si Redis falla, el dato
// puede quedar viejo hasta su vencimiento.
func (s *Servicio) invalidarCache(ctx context.Context) {
	if s.cache == nil {
		return
	}
	ctx, cancelar := context.WithTimeout(context.WithoutCancel(ctx), tiempoInvalidar)
	defer cancelar()
	if err := s.cache.SubirGeneracion(ctx); err != nil {
		metricas.CacheInvalidaciones.WithLabelValues("error").Inc()
		slog.WarnContext(ctx, "no se pudo invalidar la caché; el dato puede quedar viejo hasta su vencimiento", "error", err)
		return
	}
	metricas.CacheInvalidaciones.WithLabelValues("ok").Inc()
}

// productoDeTienda devuelve un producto publicado. Lo no publicado o inexistente no se guarda: siempre lee la fuente,
// así publicarlo se ve enseguida.
func (s *Servicio) productoDeTienda(ctx context.Context, id string) (modelo.Producto, error) {
	return leerConCache(ctx, s, "ficha", "producto:"+id, func(ctx context.Context) (modelo.Producto, error) {
		p, err := s.repo.ObtenerProducto(ctx, id)
		if err != nil {
			return modelo.Producto{}, err
		}
		if !p.Publicado {
			return modelo.Producto{}, modelo.ErrProductoNoEncontrado
		}
		return p, nil
	})
}

func (s *Servicio) listadoDeTienda(ctx context.Context, f modelo.FiltroProductos) ([]modelo.Producto, int64, error) {
	listado, err := leerConCache(ctx, s, "listado", "listado:"+huellaFiltro(f), func(ctx context.Context) (listadoCacheado, error) {
		productos, total, err := s.repo.ListarProductos(ctx, f)
		return listadoCacheado{productos, total}, err
	})
	return listado.Productos, listado.Total, err
}

// huellaFiltro identifica la página pedida: dos filtros iguales dan la misma clave.
func huellaFiltro(f modelo.FiltroProductos) string {
	precio := func(p *int64) string {
		if p == nil {
			return "-"
		}
		return fmt.Sprint(*p)
	}
	texto := fmt.Sprintf("%q|%q|%q|%s|%s|%s|%d|%d", f.Categoria, f.Talle, f.Color, precio(f.PrecioMin), precio(f.PrecioMax), f.Orden, f.Pagina, f.Tamano)
	suma := sha256.Sum256([]byte(texto))
	return hex.EncodeToString(suma[:16])
}

// promocionesParaMostrar guarda todas las promociones y filtra la vigencia al leer: una promoción empieza y termina
// en su hora exacta aunque esté en la caché. El cobro no pasa por acá: Ventas consulta las vigentes en MongoDB.
func (s *Servicio) promocionesParaMostrar(ctx context.Context, momento time.Time) ([]modelo.Promocion, error) {
	if s.cache == nil {
		return s.repo.PromocionesVigentes(ctx, momento)
	}
	todas, err := leerConCache(ctx, s, "promociones", "promociones", s.repo.ListarPromociones)
	if err != nil {
		return nil, err
	}
	vigentes := make([]modelo.Promocion, 0, len(todas))
	for _, p := range todas {
		if !p.Vigencia.Desde.After(momento) && !p.Vigencia.Hasta.Before(momento) {
			vigentes = append(vigentes, p)
		}
	}
	return vigentes, nil
}

// VigilarEvicciones publica cuántas claves expulsó Redis, cada 15 s.
func VigilarEvicciones(ctx context.Context, leer func(context.Context) (int64, error)) {
	reloj := time.NewTicker(15 * time.Second)
	defer reloj.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-reloj.C:
			if n, err := leer(ctx); err == nil {
				metricas.CacheEvicciones.Set(float64(n))
			} else if !errors.Is(err, context.Canceled) {
				slog.DebugContext(ctx, "no se pudieron leer las evicciones", "error", err)
			}
		}
	}
}
