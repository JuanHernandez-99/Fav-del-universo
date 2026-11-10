package service

import (
	"context"
	"errors"
	"testing"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
)

type repoTienda struct {
	repoPublico
	listado []modelo.Producto
	filtro  *modelo.FiltroProductos
}

func (r repoTienda) ListarProductos(_ context.Context, f modelo.FiltroProductos) ([]modelo.Producto, int64, error) {
	*r.filtro = f
	return r.listado, int64(len(r.listado)), nil
}

func (r repoTienda) ObtenerProducto(context.Context, string) (modelo.Producto, error) {
	return r.listado[0], nil
}

func filtroTienda(publicado bool) modelo.FiltroProductos {
	return modelo.FiltroProductos{Publicado: new(publicado), Orden: modelo.OrdenNuevos, Pagina: 1, Tamano: 20}
}

// El comprador ve solo lo publicado aunque el filtro pida otra cosa, y una promoción de otro medio no cambia el precio.
func TestListarTiendaFuerzaPublicadosYCalculaElPrecio(t *testing.T) {
	efectivo := "EFECTIVO"
	var filtro modelo.FiltroProductos
	repo := repoTienda{filtro: &filtro, listado: []modelo.Producto{{ID: "prod-remera-basica", Categoria: "remeras", PrecioBase: modelo.Precio{Monto: 1500000}}}}
	repo.promociones = []modelo.Promocion{
		promoPublica("promo-remeras", "PORCENTAJE", 20, nil, modelo.Alcance{Tipo: "CATEGORIA", IDs: []string{"remeras"}}),
		promoPublica("promo-efectivo", "MEDIO_PAGO", 50, &efectivo, modelo.Alcance{Tipo: "TIENDA"}),
	}
	tienda, total, err := Nuevo(repo).ListarTienda(context.Background(), filtroTienda(false))
	if err != nil {
		t.Fatal(err)
	}
	if filtro.Publicado == nil || !*filtro.Publicado {
		t.Error("listó sin filtrar por publicados")
	}
	if total != 1 || tienda[0].Precio != 1200000 || tienda[0].Promocion == nil || tienda[0].Promocion.ID != "promo-remeras" {
		t.Errorf("total %d, resultado %+v", total, tienda)
	}
}

func TestObtenerDeTiendaNoMuestraUnNoPublicado(t *testing.T) {
	repo := repoTienda{listado: []modelo.Producto{{ID: "prod-jean-mom"}}}
	if _, err := Nuevo(repo).ObtenerDeTienda(context.Background(), "prod-jean-mom"); !errors.Is(err, modelo.ErrProductoNoEncontrado) {
		t.Errorf("error: %v", err)
	}
}

// Sin promociones no se sabe el precio real: mejor un error que mostrar uno que no se va a cobrar.
func TestTiendaFallaSiNoPuedeLeerLasPromociones(t *testing.T) {
	falla := errors.New("mongo caído")
	var filtro modelo.FiltroProductos
	repo := repoTienda{filtro: &filtro, listado: []modelo.Producto{{ID: "prod-remera-basica", Publicado: true}}}
	repo.errPromocion = falla
	if _, _, err := Nuevo(repo).ListarTienda(context.Background(), filtroTienda(true)); !errors.Is(err, falla) {
		t.Errorf("listado: %v", err)
	}
	if _, err := Nuevo(repo).ObtenerDeTienda(context.Background(), "prod-remera-basica"); !errors.Is(err, falla) {
		t.Errorf("ficha: %v", err)
	}
}
