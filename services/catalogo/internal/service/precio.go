package service

import (
	"slices"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
)

// PrecioVigente es el precio que se cobra, en centavos, y la promoción que lo explica (nil si no hay).
// Es la única función que decide la política de precios (DISENO.md 5.7):
//
//   - Candidatas: vigentes en ahora (los dos extremos incluidos), que alcanzan al producto por id, por
//     categoría o a toda la tienda, y que son de porcentaje o del medio de pago indicado.
//   - No se acumulan: gana la de mayor porcentaje (D3). Si empatan, la de menor id, para que el
//     resultado no dependa del orden en que llegan.
//   - Se calcula en centavos enteros y se trunca: 649000 × 75 / 100 (D5). Nunca en coma flotante.
//
// medioPago tiene que venir ya resuelto (el servicio usa TRANSFERENCIA si el pedido no lo indica, D4).
func PrecioVigente(base int64, productoID, categoria, medioPago string, promociones []modelo.Promocion, ahora time.Time) (int64, *modelo.Promocion) {
	var ganadora *modelo.Promocion
	for i := range promociones {
		p := &promociones[i]
		if !aplica(p, productoID, categoria, medioPago, ahora) {
			continue
		}
		if ganadora == nil || p.Porcentaje > ganadora.Porcentaje || (p.Porcentaje == ganadora.Porcentaje && p.ID < ganadora.ID) {
			ganadora = p
		}
	}
	if ganadora == nil {
		return base, nil
	}
	return base * int64(100-ganadora.Porcentaje) / 100, ganadora
}

func aplica(p *modelo.Promocion, productoID, categoria, medioPago string, ahora time.Time) bool {
	if ahora.Before(p.Vigencia.Desde) || ahora.After(p.Vigencia.Hasta) {
		return false
	}
	switch p.Tipo {
	case "PORCENTAJE":
	case "MEDIO_PAGO":
		if p.MedioPago == nil || *p.MedioPago != medioPago {
			return false
		}
	default:
		return false
	}
	switch p.Alcance.Tipo {
	case "TIENDA":
		return true
	case "CATEGORIA":
		return slices.Contains(p.Alcance.IDs, categoria)
	case "PRODUCTO":
		return slices.Contains(p.Alcance.IDs, productoID)
	}
	return false
}
