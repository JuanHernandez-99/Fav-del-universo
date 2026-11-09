package cache

// Tests de integración contra un Memcached real. Necesitan CATALOGO_TEST_MEMCACHED; sin ella se saltean:
//
//	docker run -d --rm --name memcached-pruebas -p 11299:11211 memcached:1.6-alpine
//	CATALOGO_TEST_MEMCACHED=localhost:11299 go test ./internal/cache/

import (
	"bytes"
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

var ctx = context.Background()

func clienteDePrueba(t *testing.T) *Cliente {
	t.Helper()
	direccion := os.Getenv("CATALOGO_TEST_MEMCACHED")
	if direccion == "" {
		t.Skip("CATALOGO_TEST_MEMCACHED no definida: se saltea el test de integración")
	}
	c := Nuevo(direccion)
	if err := c.memcached.FlushAll(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Cerrar() })
	return c
}

func TestLeerYGuardar(t *testing.T) {
	c := clienteDePrueba(t)
	if _, ok, err := c.Leer(ctx, "remera"); ok || err != nil {
		t.Fatalf("una clave que no está no es un error: %v, %v", ok, err)
	}
	if err := c.Guardar(ctx, "remera", []byte("blanca"), time.Minute); err != nil {
		t.Fatal(err)
	}
	valor, ok, err := c.Leer(ctx, "remera")
	if !ok || err != nil || string(valor) != "blanca" {
		t.Fatalf("%q, %v, %v", valor, ok, err)
	}
}

func TestLaClaveVence(t *testing.T) {
	c := clienteDePrueba(t)
	if err := c.Guardar(ctx, "remera", []byte("blanca"), 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := c.Leer(ctx, "remera"); !ok {
		t.Fatal("un vencimiento menor a 1 s no puede significar que no se guarde")
	}
	time.Sleep(2200 * time.Millisecond)
	if _, ok, _ := c.Leer(ctx, "remera"); ok {
		t.Fatal("tenía que vencer")
	}
}

func TestGeneracion(t *testing.T) {
	c := clienteDePrueba(t)
	if g, err := c.Generacion(ctx); g != 0 || err != nil {
		t.Fatalf("sin generación vale 0: %d, %v", g, err)
	}
	for esperada := int64(1); esperada <= 2; esperada++ {
		if err := c.SubirGeneracion(ctx); err != nil {
			t.Fatal(err)
		}
		if g, err := c.Generacion(ctx); g != esperada || err != nil {
			t.Fatalf("generación %d, %v", g, err)
		}
	}
}

// Con la clave sin crear, dos réplicas pueden querer crearla a la vez: ningún aumento se puede perder.
func TestGeneracionConSubidasSimultaneas(t *testing.T) {
	c := clienteDePrueba(t)
	var grupo sync.WaitGroup
	for range 20 {
		grupo.Go(func() {
			if err := c.SubirGeneracion(ctx); err != nil {
				t.Error(err)
			}
		})
	}
	grupo.Wait()
	if g, err := c.Generacion(ctx); g != 20 || err != nil {
		t.Fatalf("generación %d, %v", g, err)
	}
}

func TestEvicciones(t *testing.T) {
	c := clienteDePrueba(t)
	if _, err := c.Evicciones(ctx); err != nil {
		t.Fatal(err)
	}
}

// Un id raro en la URL no puede dar un error: pausaría la caché de la réplica.
func TestClavesQueMemcachedNoAcepta(t *testing.T) {
	c := clienteDePrueba(t)
	for _, clave := range []string{"producto:remera blanca", "producto:" + strings.Repeat("a", 300), "producto:remera\nblanca"} {
		if err := c.Guardar(ctx, clave, []byte("x"), time.Minute); err != nil {
			t.Errorf("%.30q: %v", clave, err)
		}
		if valor, ok, err := c.Leer(ctx, clave); !ok || err != nil || string(valor) != "x" {
			t.Errorf("%.30q: %q, %v, %v", clave, valor, ok, err)
		}
	}
}

// Un valor que Memcached rechazaría no se guarda, y eso no es un error: pausaría la caché de la réplica.
func TestUnValorMuyGrandeNoSeGuarda(t *testing.T) {
	c := clienteDePrueba(t)
	if err := c.Guardar(ctx, "listado", bytes.Repeat([]byte("x"), 2<<20), time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := c.Leer(ctx, "listado"); ok || err != nil {
		t.Fatalf("%v, %v", ok, err)
	}
}

func TestClaveValida(t *testing.T) {
	if clave := "catalogo:v1:g3:producto:abc"; claveValida(clave) != clave {
		t.Fatal("una clave aceptada no se cambia")
	}
	if claveValida("a b") == claveValida("a c") {
		t.Fatal("dos claves distintas no pueden dar el mismo resumen")
	}
	if clave := claveValida(strings.Repeat("a", 251)); len(clave) > 250 || strings.ContainsAny(clave, " \n") {
		t.Fatalf("resumen inválido: %q", clave)
	}
}

func TestSegundos(t *testing.T) {
	casos := map[time.Duration]int32{time.Millisecond: 1, time.Second: 1, 1500 * time.Millisecond: 2, 5 * time.Minute: 300}
	for vencimiento, esperado := range casos {
		if s := segundos(vencimiento); s != esperado {
			t.Errorf("%v: %d", vencimiento, s)
		}
	}
}

// Con Memcached caído falla rápido: el comprador no paga el tiempo de espera completo en cada operación.
func TestSinMemcachedFallaRapido(t *testing.T) {
	c := Nuevo("127.0.0.1:1")
	defer c.Cerrar()
	inicio := time.Now()
	if _, _, err := c.Leer(ctx, "remera"); err == nil {
		t.Fatal("tenía que fallar")
	}
	if demora := time.Since(inicio); demora > 500*time.Millisecond {
		t.Fatalf("tardó %v", demora)
	}
}
