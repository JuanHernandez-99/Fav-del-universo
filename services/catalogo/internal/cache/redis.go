// Package cache es el único lugar que habla con Redis (ADR-007). Los tiempos de espera son cortos y sin reintentos:
// si Redis falla, conviene ir a MongoDB enseguida antes que hacer esperar al comprador.
package cache

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	claveGeneracion = "catalogo:v1:generacion"
	tiempoMaximo    = 100 * time.Millisecond
)

type Cliente struct {
	redis *redis.Client
}

// Nuevo arma el cliente. No abre la conexión: Catálogo arranca y atiende aunque Redis esté caído.
func Nuevo(direccion string) *Cliente {
	return &Cliente{redis: redis.NewClient(&redis.Options{
		Addr:         direccion,
		DialTimeout:  tiempoMaximo,
		ReadTimeout:  tiempoMaximo,
		WriteTimeout: tiempoMaximo,
		PoolTimeout:  tiempoMaximo,
		MaxRetries:   -1,
	})}
}

func (c *Cliente) Cerrar() error { return c.redis.Close() }

// Generacion es el número que forma parte de cada clave. Si todavía no existe, vale 0.
func (c *Cliente) Generacion(ctx context.Context) (int64, error) {
	ctx, cancelar := context.WithTimeout(ctx, tiempoMaximo)
	defer cancelar()
	valor, err := c.redis.Get(ctx, claveGeneracion).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	return valor, err
}

// SubirGeneracion invalida todo de una vez: las claves anteriores dejan de leerse y vencen solas.
func (c *Cliente) SubirGeneracion(ctx context.Context) error {
	ctx, cancelar := context.WithTimeout(ctx, tiempoMaximo)
	defer cancelar()
	return c.redis.Incr(ctx, claveGeneracion).Err()
}

// Leer devuelve false si la clave no está. Un error de Redis no es lo mismo que un fallo de caché.
func (c *Cliente) Leer(ctx context.Context, clave string) ([]byte, bool, error) {
	ctx, cancelar := context.WithTimeout(ctx, tiempoMaximo)
	defer cancelar()
	valor, err := c.redis.Get(ctx, clave).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return valor, true, nil
}

func (c *Cliente) Guardar(ctx context.Context, clave string, valor []byte, vencimiento time.Duration) error {
	ctx, cancelar := context.WithTimeout(ctx, tiempoMaximo)
	defer cancelar()
	return c.redis.Set(ctx, clave, valor, vencimiento).Err()
}

// Evicciones devuelve cuántas claves expulsó Redis por falta de memoria.
func (c *Cliente) Evicciones(ctx context.Context) (int64, error) {
	ctx, cancelar := context.WithTimeout(ctx, tiempoMaximo)
	defer cancelar()
	estadisticas, err := c.redis.Info(ctx, "stats").Result()
	if err != nil {
		return 0, err
	}
	for linea := range strings.SplitSeq(estadisticas, "\r\n") {
		if valor, ok := strings.CutPrefix(linea, "evicted_keys:"); ok {
			return strconv.ParseInt(valor, 10, 64)
		}
	}
	return 0, errors.New("redis no informó evicted_keys")
}
