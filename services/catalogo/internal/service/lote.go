package service

import (
	"context"
	"slices"
	"strings"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
)

// ConsultarVariantes responde la consulta por lote de Ventas y del panel (RF-09): por cada id pedido,
// los datos de la variante y de su producto, con el precio que se cobra (PrecioVigente). Los ids repetidos se
// responden una sola vez y el resultado respeta el orden del pedido.
//
// Si algún id no existe devuelve *modelo.ErrVariantesInexistentes con todos los que faltan, y no
// devuelve nada de lo demás: Ventas no puede armar una venta con un ítem desconocido.
// No filtra por publicado (D6): una variante no publicada se puede vender en el local.
func (s *Servicio) ConsultarVariantes(ctx context.Context, ids []string, medioPago string) ([]modelo.DetalleVariante, error) {
	v := &modelo.ErrorValidacion{}
	if medioPago == "" {
		medioPago = modelo.MedioPagoPorDefecto
	}
	if !slices.Contains(modelo.MediosDePago, medioPago) {
		v.Agregar("medioPago", "tiene que ser uno de: %s", strings.Join(modelo.MediosDePago, ", "))
	}

	pedidos := make([]string, 0, len(ids))
	vistos := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			v.Agregar("variantes", "no puede tener ids vacíos")
			break
		}
		if !vistos[id] {
			vistos[id] = true
			pedidos = append(pedidos, id)
		}
	}
	if len(ids) == 0 {
		v.Agregar("variantes", "es obligatorio: de 1 a %d ids separados por coma", modelo.MaximoVariantesPorLote)
	} else if len(pedidos) > modelo.MaximoVariantesPorLote {
		v.Agregar("variantes", "se pueden pedir hasta %d ids por vez", modelo.MaximoVariantesPorLote)
	}
	if err := v.ComoError(); err != nil {
		return nil, err
	}

	encontradas, err := s.repo.ObtenerVariantes(ctx, pedidos)
	if err != nil {
		return nil, err
	}
	// Una sola consulta de promociones para todo el lote. Si falla, no se responde con el precio base:
	// Ventas cobraría de más.
	momento := ahora()
	promociones, err := s.repo.PromocionesVigentes(ctx, momento)
	if err != nil {
		return nil, err
	}
	porID := make(map[string]modelo.DetalleVariante, len(encontradas))
	for _, d := range encontradas {
		porID[d.VarianteID] = d
	}

	resultado := make([]modelo.DetalleVariante, 0, len(pedidos))
	faltan := []string{}
	for _, id := range pedidos {
		d, existe := porID[id]
		if !existe {
			faltan = append(faltan, id)
			continue
		}
		d.Precio, d.Promocion = PrecioVigente(d.PrecioBase.Monto, d.ProductoID, d.Categoria, medioPago, promociones, momento)
		resultado = append(resultado, d)
	}
	if len(faltan) > 0 {
		return nil, &modelo.ErrVariantesInexistentes{IDs: faltan}
	}
	return resultado, nil
}
