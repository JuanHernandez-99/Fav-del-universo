package dominio

import (
	"errors"
	"testing"
)

func TestDescuentoFisicoOrdenaItemsYLaHuellaNoDependeDelOrden(t *testing.T) {
	a, err := NuevoDescuentoFisico("venta-local-1", "v1", []Item{{"var-remera", 2}, {"var-jean", 1}}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	b, err := NuevoDescuentoFisico("venta-local-1", "v1", []Item{{"var-jean", 1}, {"var-remera", 2}}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if a.Items[0].VarianteID != "var-jean" {
		t.Errorf("ítems sin ordenar: %+v", a.Items)
	}
	if a.Huella() != b.Huella() {
		t.Error("el mismo pedido en otro orden debe dar la misma huella")
	}
}

func TestDescuentoFisicoRechazaPedidosInvalidos(t *testing.T) {
	casos := map[string]struct {
		clave string
		items []Item
	}{
		"clave corta":       {"corta", []Item{{"var-remera", 1}}},
		"sin ítems":         {"venta-local-1", nil},
		"cantidad cero":     {"venta-local-1", []Item{{"var-remera", 0}}},
		"variante repetida": {"venta-local-1", []Item{{"var-remera", 1}, {"var-remera", 1}}},
	}
	for nombre, c := range casos {
		if _, err := NuevoDescuentoFisico(c.clave, "v1", c.items, ""); !errors.Is(err, ErrPedidoInvalido) {
			t.Errorf("%s: se esperaba pedido inválido, llegó %v", nombre, err)
		}
	}
}
