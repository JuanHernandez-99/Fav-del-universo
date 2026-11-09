package cache

// Tests de integración contra un Redis real. Necesitan CATALOGO_TEST_REDIS; sin ella se saltean:
//
//	docker run -d --rm --name redis-pruebas -p 6399:6379 redis:7.4-alpine
//	CATALOGO_TEST_REDIS=localhost:6399 go test ./internal/cache/

import (
	"context"
	"os"
	"testing"
	"time"
)

var ctx = context.Background()

func clienteDePrueba(t *testing.T) *Cliente {
	t.Helper()
	direccion := os.Getenv("CATALOGO_TEST_REDIS")
	if direccion == "" {
		t.Skip("CATALOGO_TEST_REDIS no definida: se saltea el test de integración")
	}
	c := Nuevo(direccion)
	if err := c.redis.FlushAll(ctx).Err(); err != nil {
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
	time.Sleep(150 * time.Millisecond)
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

func TestEvicciones(t *testing.T) {
	c := clienteDePrueba(t)
	if _, err := c.Evicciones(ctx); err != nil {
		t.Fatal(err)
	}
}

// Con Redis caído falla rápido: el comprador no paga el tiempo de espera completo en cada operación.
func TestSinRedisFallaRapido(t *testing.T) {
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
