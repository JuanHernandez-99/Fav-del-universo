// Package cache es el único lugar que habla con Memcached (ADR-019). Los tiempos de espera son cortos y sin reintentos:
// si Memcached falla, conviene ir a MongoDB enseguida antes que hacer esperar al comprador.
package cache

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
)

const (
	claveGeneracion = "catalogo:v1:generacion"
	tiempoMaximo    = 100 * time.Millisecond
	// conexionesEnEspera tiene que superar los pedidos simultáneos: si no, en un pico se abre una conexión por lectura.
	conexionesEnEspera = 256
	// tamanoMaximo queda debajo del límite de 1 MB por valor de Memcached. Lo que lo supera no se guarda.
	tamanoMaximo = 1000 * 1024
)

// servidor se resuelve en cada conexión, porque la biblioteca conecta usando este texto: Catálogo arranca aunque
// Memcached esté caído, y lo vuelve a encontrar si cambia de IP. memcache.New, en cambio, lo resuelve una sola vez.
type servidor string

func (s servidor) Network() string { return "tcp" }
func (s servidor) String() string  { return string(s) }

func (s servidor) PickServer(string) (net.Addr, error)     { return s, nil }
func (s servidor) Each(aplicar func(net.Addr) error) error { return aplicar(s) }

type Cliente struct {
	memcached *memcache.Client
	direccion string
}

// Nuevo arma el cliente. No abre la conexión: Catálogo arranca y atiende aunque Memcached esté caído.
func Nuevo(direccion string) *Cliente {
	memcached := memcache.NewFromSelector(servidor(direccion))
	memcached.Timeout = tiempoMaximo
	memcached.MaxIdleConns = conexionesEnEspera
	return &Cliente{memcached: memcached, direccion: direccion}
}

func (c *Cliente) Cerrar() error { return c.memcached.Close() }

// Generacion es el número que forma parte de cada clave. Si todavía no existe, vale 0.
func (c *Cliente) Generacion(context.Context) (int64, error) {
	item, err := c.memcached.Get(claveGeneracion)
	if errors.Is(err, memcache.ErrCacheMiss) {
		return 0, nil
	}
	if err != nil {
		return 0, c.tras(err)
	}
	return strconv.ParseInt(strings.TrimSpace(string(item.Value)), 10, 64)
}

// SubirGeneracion invalida todo de una vez: las claves anteriores dejan de leerse y vencen solas. Memcached no incrementa
// una clave que no existe: la primera vez se crea. Si otra réplica la crea justo antes, se vuelve a incrementar.
func (c *Cliente) SubirGeneracion(context.Context) error {
	_, err := c.memcached.Increment(claveGeneracion, 1)
	if !errors.Is(err, memcache.ErrCacheMiss) {
		return c.tras(err)
	}
	err = c.memcached.Add(&memcache.Item{Key: claveGeneracion, Value: []byte("1")})
	if errors.Is(err, memcache.ErrNotStored) {
		_, err = c.memcached.Increment(claveGeneracion, 1)
	}
	return c.tras(err)
}

// tras vacía las conexiones en espera ante un error. Si Memcached se reinició, todas están muertas: sin esto, cada una
// daría un error por turno y, con la pausa de 5 s tras cada error, la réplica tardaría minutos en volver a usar la caché.
func (c *Cliente) tras(err error) error {
	if err != nil {
		_ = c.memcached.Close()
	}
	return err
}

// Leer devuelve false si la clave no está. Un error de Memcached no es lo mismo que un fallo de caché.
func (c *Cliente) Leer(_ context.Context, clave string) ([]byte, bool, error) {
	item, err := c.memcached.Get(claveValida(clave))
	if errors.Is(err, memcache.ErrCacheMiss) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, c.tras(err)
	}
	return item.Value, true, nil
}

func (c *Cliente) Guardar(_ context.Context, clave string, valor []byte, vencimiento time.Duration) error {
	if len(valor) > tamanoMaximo {
		return nil
	}
	return c.tras(c.memcached.Set(&memcache.Item{Key: claveValida(clave), Value: valor, Expiration: segundos(vencimiento)}))
}

// Evicciones devuelve cuántas claves expulsó Memcached por falta de memoria. La biblioteca no expone las estadísticas.
func (c *Cliente) Evicciones(ctx context.Context) (int64, error) {
	ctx, cancelar := context.WithTimeout(ctx, tiempoMaximo)
	defer cancelar()
	var marcador net.Dialer
	conexion, err := marcador.DialContext(ctx, "tcp", c.direccion)
	if err != nil {
		return 0, err
	}
	defer conexion.Close()
	if limite, ok := ctx.Deadline(); ok {
		_ = conexion.SetDeadline(limite)
	}
	if _, err := fmt.Fprint(conexion, "stats\r\n"); err != nil {
		return 0, err
	}
	lector := bufio.NewScanner(conexion)
	for lector.Scan() {
		if valor, ok := strings.CutPrefix(lector.Text(), "STAT evictions "); ok {
			return strconv.ParseInt(valor, 10, 64)
		}
		if lector.Text() == "END" {
			break
		}
	}
	if err := lector.Err(); err != nil {
		return 0, err
	}
	return 0, errors.New("memcached no informó evictions")
}

// claveValida resume las claves que Memcached no acepta (más de 250 bytes, espacios o caracteres de control). Sin esto,
// una URL con un id raro daría un error y pausaría la caché de la réplica.
func claveValida(clave string) string {
	valida := len(clave) <= 250
	for i := 0; valida && i < len(clave); i++ {
		valida = clave[i] > ' ' && clave[i] != 0x7f
	}
	if valida {
		return clave
	}
	resumen := sha256.Sum256([]byte(clave))
	return "catalogo:resumen:" + hex.EncodeToString(resumen[:])
}

// segundos redondea hacia arriba: Memcached vence por segundos, y 0 significaría que no vence nunca.
func segundos(vencimiento time.Duration) int32 {
	return max(1, int32(math.Ceil(vencimiento.Seconds())))
}
