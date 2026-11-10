package service

import (
	"testing"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
)

var hoy = time.Date(2026, 11, 3, 15, 0, 0, 0, time.UTC)

// promo arma una promoción vigente hoy; cada caso cambia lo que necesita.
func promo(id string, porcentaje int, alcance string, ids ...string) modelo.Promocion {
	return modelo.Promocion{
		ID: id, Nombre: id, Tipo: "PORCENTAJE", Porcentaje: porcentaje,
		Vigencia: modelo.Vigencia{Desde: hoy.AddDate(0, 0, -1), Hasta: hoy.AddDate(0, 0, 1)},
		Alcance:  modelo.Alcance{Tipo: alcance, IDs: ids},
	}
}

func deMedioDePago(p modelo.Promocion, medio string) modelo.Promocion {
	p.Tipo, p.MedioPago = "MEDIO_PAGO", &medio
	return p
}

func conVigencia(p modelo.Promocion, desde, hasta time.Time) modelo.Promocion {
	p.Vigencia = modelo.Vigencia{Desde: desde, Hasta: hasta}
	return p
}

func TestPrecioVigente(t *testing.T) {
	const jean, base = "prod-jean-wide-leg", int64(4549000) // $45.490
	casos := []struct {
		nombre      string
		promociones []modelo.Promocion
		medioPago   string
		precio      int64
		ganadora    string // "" si no se aplica ninguna
	}{
		{"sin promociones", nil, "TRANSFERENCIA", base, ""},
		{"por producto", []modelo.Promocion{promo("liquidacion", 30, "PRODUCTO", jean)}, "TRANSFERENCIA", 3184300, "liquidacion"},
		{"por categoría", []modelo.Promocion{promo("semana-del-jean", 20, "CATEGORIA", "jeans")}, "TRANSFERENCIA", 3639200, "semana-del-jean"},
		{"a toda la tienda", []modelo.Promocion{promo("black-friday", 25, "TIENDA")}, "TRANSFERENCIA", 3411750, "black-friday"},
		{"de otro producto o categoría", []modelo.Promocion{promo("otra", 30, "PRODUCTO", "prod-otro"), promo("remeras", 20, "CATEGORIA", "remeras")}, "TRANSFERENCIA", base, ""},
		{"no se acumulan: gana la mayor (D3)", []modelo.Promocion{promo("semana-del-jean", 20, "CATEGORIA", "jeans"), promo("liquidacion", 30, "PRODUCTO", jean)}, "TRANSFERENCIA", 3184300, "liquidacion"},
		{"empate: la de menor id", []modelo.Promocion{promo("b", 20, "TIENDA"), promo("a", 20, "TIENDA")}, "TRANSFERENCIA", 3639200, "a"},
		{"de medio de pago, con ese medio", []modelo.Promocion{deMedioDePago(promo("efectivo", 10, "TIENDA"), "EFECTIVO")}, "EFECTIVO", 4094100, "efectivo"},
		{"de medio de pago, con otro medio", []modelo.Promocion{deMedioDePago(promo("efectivo", 10, "TIENDA"), "EFECTIVO")}, "TRANSFERENCIA", base, ""},
		{"vencida", []modelo.Promocion{conVigencia(promo("hot-sale", 25, "TIENDA"), hoy.AddDate(0, -6, 0), hoy.AddDate(0, 0, -1))}, "TRANSFERENCIA", base, ""},
		{"futura", []modelo.Promocion{conVigencia(promo("navidad", 15, "TIENDA"), hoy.AddDate(0, 0, 1), hoy.AddDate(0, 1, 0))}, "TRANSFERENCIA", base, ""},
		{"el primer instante de la vigencia cuenta", []modelo.Promocion{conVigencia(promo("hoy", 10, "TIENDA"), hoy, hoy.AddDate(0, 0, 1))}, "TRANSFERENCIA", 4094100, "hoy"},
		{"el último instante de la vigencia cuenta", []modelo.Promocion{conVigencia(promo("hoy", 10, "TIENDA"), hoy.AddDate(0, 0, -1), hoy)}, "TRANSFERENCIA", 4094100, "hoy"},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			precio, ganadora := PrecioVigente(base, jean, "jeans", c.medioPago, c.promociones, hoy)
			id := ""
			if ganadora != nil {
				id = ganadora.ID
			}
			if precio != c.precio || id != c.ganadora {
				t.Errorf("precio %d con %q; se esperaba %d con %q", precio, id, c.precio, c.ganadora)
			}
		})
	}
}

// El precio se trunca en centavos enteros, hacia abajo (D5): $9,99 con 15% da $8,49, no $8,4915 ni $8,50.
func TestPrecioVigenteTrunca(t *testing.T) {
	precio, _ := PrecioVigente(999, "prod-x", "accesorios", "TRANSFERENCIA", []modelo.Promocion{promo("p", 15, "TIENDA")}, hoy)
	if precio != 849 {
		t.Errorf("999 centavos con 15%% = %d; se esperaba 849", precio)
	}
}
