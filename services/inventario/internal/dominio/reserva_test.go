package dominio

import (
	"errors"
	"testing"
	"time"
)

var ahora = time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)

func reservaValida(t *testing.T) Reserva {
	t.Helper()
	r, err := NuevaReserva("r1", "clave-123456", "venta-1",
		[]Item{{"var-b", 2}, {"var-a", 1}}, 20*time.Minute, ahora)
	if err != nil {
		t.Fatalf("reserva válida rechazada: %v", err)
	}
	return r
}

func TestNuevaReservaOrdenaItemsYCalculaVencimiento(t *testing.T) {
	r := reservaValida(t)
	if r.Items[0].VarianteID != "var-a" || r.Items[1].VarianteID != "var-b" {
		t.Errorf("los ítems deben quedar ordenados por variante: %+v", r.Items)
	}
	if !r.VenceEn.Equal(ahora.Add(20 * time.Minute)) {
		t.Errorf("vence en %v", r.VenceEn)
	}
	if r.Estado != ReservaActiva {
		t.Errorf("estado %s", r.Estado)
	}
}

func TestNuevaReservaValida(t *testing.T) {
	casos := map[string]struct {
		clave    string
		items    []Item
		vigencia time.Duration
	}{
		"sin ítems":          {"clave-123456", nil, time.Hour},
		"cantidad cero":      {"clave-123456", []Item{{"var-a", 0}}, time.Hour},
		"variante repetida":  {"clave-123456", []Item{{"var-a", 1}, {"var-a", 2}}, time.Hour},
		"clave corta":        {"corta", []Item{{"var-a", 1}}, time.Hour},
		"vigencia excesiva":  {"clave-123456", []Item{{"var-a", 1}}, 100 * time.Hour},
		"variante sin id":    {"clave-123456", []Item{{"", 1}}, time.Hour},
	}
	for nombre, c := range casos {
		t.Run(nombre, func(t *testing.T) {
			_, err := NuevaReserva("r1", c.clave, "", c.items, c.vigencia, ahora)
			if !errors.Is(err, ErrPedidoInvalido) {
				t.Errorf("esperaba ErrPedidoInvalido, obtuve %v", err)
			}
		})
	}
}

func TestHuellaDistingueElPedido(t *testing.T) {
	a, _ := NuevaReserva("r1", "clave-123456", "", []Item{{"var-a", 1}, {"var-b", 2}}, time.Hour, ahora)
	b, _ := NuevaReserva("r2", "clave-123456", "", []Item{{"var-b", 2}, {"var-a", 1}}, time.Hour, ahora)
	c, _ := NuevaReserva("r3", "clave-123456", "", []Item{{"var-a", 1}, {"var-b", 3}}, time.Hour, ahora)
	if a.Huella != b.Huella {
		t.Error("el mismo pedido en otro orden debe tener la misma huella")
	}
	if a.Huella == c.Huella {
		t.Error("pedidos distintos deben tener huellas distintas")
	}
}

func TestConfirmar(t *testing.T) {
	r := reservaValida(t)
	if err := r.Confirmar(ahora.Add(time.Minute)); err != nil || r.Estado != ReservaConfirmada {
		t.Fatalf("confirmar a tiempo: estado %s, error %v", r.Estado, err)
	}
	if err := r.Confirmar(ahora.Add(2 * time.Minute)); err != nil {
		t.Errorf("confirmar dos veces debe ser idempotente: %v", err)
	}
	if err := r.Liberar(ahora); !errors.Is(err, ErrReservaYaConfirmada) {
		t.Errorf("liberar una confirmada: %v", err)
	}
}

// El pago llega después del vencimiento: Inventario decide y la reserva queda vencida (ADR-005, punto 4).
func TestConfirmarFueraDePlazoLaVence(t *testing.T) {
	r := reservaValida(t)
	err := r.Confirmar(ahora.Add(21 * time.Minute))
	if !errors.Is(err, ErrReservaVencida) || r.Estado != ReservaVencida {
		t.Errorf("estado %s, error %v", r.Estado, err)
	}
}

func TestLiberarYVencer(t *testing.T) {
	r := reservaValida(t)
	if r.Vencer(ahora.Add(time.Minute)) {
		t.Error("no debe vencer antes de plazo")
	}
	if err := r.Liberar(ahora); err != nil || r.Estado != ReservaLiberada {
		t.Fatalf("liberar: estado %s, error %v", r.Estado, err)
	}
	if err := r.Liberar(ahora); err != nil {
		t.Errorf("liberar dos veces debe ser idempotente: %v", err)
	}
	if r.Vencer(ahora.Add(time.Hour)) {
		t.Error("una reserva liberada no vence")
	}
	if err := r.Confirmar(ahora); !errors.Is(err, ErrReservaYaLiberada) {
		t.Errorf("confirmar una liberada: %v", err)
	}
}

func TestEsperaReintento(t *testing.T) {
	esperado := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second}
	for i, e := range esperado {
		if got := EsperaReintento(i + 1); got != e {
			t.Errorf("intento %d: %v, esperaba %v", i+1, got, e)
		}
	}
	if got := EsperaReintento(50); got != 5*time.Minute {
		t.Errorf("tope: %v", got)
	}
}
