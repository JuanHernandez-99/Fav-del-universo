package dominio

import (
	"errors"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

func reservaValida(t *testing.T) Reserva {
	t.Helper()
	r, err := NuevaReserva("r1", "venta-1", []Item{{"var-b", 1}, {"var-a", 2}}, 20*time.Minute, t0)
	if err != nil {
		t.Fatalf("reserva válida rechazada: %v", err)
	}
	return r
}

func TestNuevaReservaOrdenaItemsYCalculaVencimiento(t *testing.T) {
	r := reservaValida(t)
	if r.Estado != EstadoActiva {
		t.Errorf("estado = %s, se esperaba ACTIVA", r.Estado)
	}
	if r.Items[0].VarianteID != "var-a" || r.Items[1].VarianteID != "var-b" {
		t.Errorf("los ítems deben quedar ordenados por variante: %+v", r.Items)
	}
	if !r.VenceEn.Equal(t0.Add(20 * time.Minute)) {
		t.Errorf("venceEn = %v", r.VenceEn)
	}
}

func TestNuevaReservaValida(t *testing.T) {
	casos := []struct {
		nombre   string
		items    []Item
		duracion time.Duration
		campo    string
	}{
		{"sin ítems", nil, time.Hour, "items"},
		{"cantidad cero", []Item{{"var-a", 0}}, time.Hour, "items[0].cantidad"},
		{"variante vacía", []Item{{" ", 1}}, time.Hour, "items[0].varianteId"},
		{"variante repetida", []Item{{"var-a", 1}, {"var-a", 2}}, time.Hour, "items[1].varianteId"},
		{"duración corta", []Item{{"var-a", 1}}, 30 * time.Second, "vencimientoMinutos"},
		{"duración larga", []Item{{"var-a", 1}}, 49 * time.Hour, "vencimientoMinutos"},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			_, err := NuevaReserva("r", "", c.items, c.duracion, t0)
			var verr *ErrorValidacion
			if !errors.As(err, &verr) {
				t.Fatalf("se esperaba ErrorValidacion, vino %v", err)
			}
			for _, e := range verr.Errores {
				if e.Campo == c.campo {
					return
				}
			}
			t.Errorf("no se informó el campo %s: %+v", c.campo, verr.Errores)
		})
	}
}

func TestConfirmar(t *testing.T) {
	r := reservaValida(t)

	if cambia, err := r.Confirmar(t0.Add(10 * time.Minute)); !cambia || err != nil {
		t.Errorf("reserva vigente: cambia=%v err=%v", cambia, err)
	}
	if _, err := r.Confirmar(t0.Add(20 * time.Minute)); !errors.Is(err, ErrReservaVencida) {
		t.Errorf("en el instante del vencimiento gana el vencimiento: err=%v", err)
	}

	r.Estado = EstadoConfirmada
	if cambia, err := r.Confirmar(t0); cambia || err != nil {
		t.Errorf("confirmar dos veces debe ser idempotente: cambia=%v err=%v", cambia, err)
	}
	r.Estado = EstadoLiberada
	if _, err := r.Confirmar(t0); !errors.Is(err, ErrReservaLiberada) {
		t.Errorf("liberada: err=%v", err)
	}
	r.Estado = EstadoVencida
	if _, err := r.Confirmar(t0); !errors.Is(err, ErrReservaVencida) {
		t.Errorf("vencida: err=%v", err)
	}
}

func TestLiberar(t *testing.T) {
	r := reservaValida(t)
	if cambia, err := r.Liberar(); !cambia || err != nil {
		t.Errorf("activa: cambia=%v err=%v", cambia, err)
	}
	for _, e := range []EstadoReserva{EstadoLiberada, EstadoVencida} {
		r.Estado = e
		if cambia, err := r.Liberar(); cambia || err != nil {
			t.Errorf("%s: liberar debe ser idempotente: cambia=%v err=%v", e, cambia, err)
		}
	}
	r.Estado = EstadoConfirmada
	if _, err := r.Liberar(); !errors.Is(err, ErrReservaConfirmada) {
		t.Errorf("confirmada: err=%v", err)
	}
}

func TestDebeVencer(t *testing.T) {
	r := reservaValida(t)
	if r.DebeVencer(t0.Add(19 * time.Minute)) {
		t.Error("no debe vencer antes del plazo")
	}
	if !r.DebeVencer(t0.Add(20 * time.Minute)) {
		t.Error("debe vencer al cumplirse el plazo")
	}
}
