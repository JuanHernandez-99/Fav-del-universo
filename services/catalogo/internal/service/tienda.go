package service

import (
	"context"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
)

// ProductoTienda es un producto publicado con lo que ve el comprador: el precio vigente y la promoción
// que lo explica, si se aplicó alguna (HU-01.4). Usa el medio por defecto (D4).
type ProductoTienda struct {
	Producto  modelo.Producto
	Precio    int64 // centavos
	Promocion *modelo.Promocion
}

// ListarTienda es el listado del comprador (HU-01, HU-02): siempre solo publicados, sin importar el filtro
// recibido. Si fallan las promociones no se responde con el precio base: mostraría un precio que no se cobra.
func (s *Servicio) ListarTienda(ctx context.Context, f modelo.FiltroProductos) ([]ProductoTienda, int64, error) {
	f.Publicado = new(true)
	if err := validarFiltro(f); err != nil {
		return nil, 0, err
	}
	// Con texto va al motor de búsqueda. Sin texto es navegar: sale de MongoDB y funciona aunque el motor caiga (ADR-006).
	var productos []modelo.Producto
	var total int64
	var err error
	if f.Texto != "" && s.indice != nil {
		productos, total, err = s.buscarEnTienda(ctx, f)
	} else {
		productos, total, err = s.repo.ListarProductos(ctx, f)
	}
	if err != nil {
		return nil, 0, err
	}
	tienda, err := s.conPrecioVigente(ctx, productos)
	if err != nil {
		return nil, 0, err
	}
	return tienda, total, nil
}

// ObtenerDeTienda es la ficha del comprador. Un producto no publicado se responde igual que uno
// inexistente (HU-01.2).
func (s *Servicio) ObtenerDeTienda(ctx context.Context, id string) (ProductoTienda, error) {
	p, err := s.repo.ObtenerProducto(ctx, id)
	if err != nil {
		return ProductoTienda{}, err
	}
	if !p.Publicado {
		return ProductoTienda{}, modelo.ErrProductoNoEncontrado
	}
	tienda, err := s.conPrecioVigente(ctx, []modelo.Producto{p})
	if err != nil {
		return ProductoTienda{}, err
	}
	return tienda[0], nil
}

func (s *Servicio) conPrecioVigente(ctx context.Context, productos []modelo.Producto) ([]ProductoTienda, error) {
	momento := ahora()
	promociones, err := s.repo.PromocionesVigentes(ctx, momento)
	if err != nil {
		return nil, err
	}
	tienda := make([]ProductoTienda, len(productos))
	for i, p := range productos {
		precio, promocion := PrecioVigente(p.PrecioBase.Monto, p.ID, p.Categoria, modelo.MedioPagoPorDefecto, promociones, momento)
		tienda[i] = ProductoTienda{p, precio, promocion}
	}
	return tienda, nil
}
