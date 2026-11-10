package service

import (
	"context"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
)

// ProductoPublico es un producto publicado con el precio que se le cobra a un grupo externo.
type ProductoPublico struct {
	Producto modelo.Producto
	Precio   int64 // centavos; el precio es del producto, las variantes no tienen uno propio (5.3)
}

// CatalogoPublico arma la capacidad publicada: todos los productos publicados con su precio vigente.
// Usa el medio por defecto (D4), que es con el que se pagan los pedidos del contrato. Si fallan las
// promociones no se responde con el precio base: el catálogo mostraría un precio que no es el del pedido.
func (s *Servicio) CatalogoPublico(ctx context.Context) ([]ProductoPublico, error) {
	productos, err := s.repo.ProductosPublicados(ctx)
	if err != nil {
		return nil, err
	}
	momento := ahora()
	promociones, err := s.repo.PromocionesVigentes(ctx, momento)
	if err != nil {
		return nil, err
	}
	catalogo := make([]ProductoPublico, len(productos))
	for i, p := range productos {
		precio, _ := PrecioVigente(p.PrecioBase.Monto, p.ID, p.Categoria, modelo.MedioPagoPorDefecto, promociones, momento)
		catalogo[i] = ProductoPublico{p, precio}
	}
	return catalogo, nil
}
