package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
)

type repoPublico struct {
	Repositorio
	productos    []modelo.Producto
	promociones  []modelo.Promocion
	errPromocion error
}

func (r repoPublico) ProductosPublicados(context.Context) ([]modelo.Producto, error) {
	return r.productos, nil
}

func (r repoPublico) PromocionesVigentes(context.Context, time.Time) ([]modelo.Promocion, error) {
	return r.promociones, r.errPromocion
}

func promoPublica(id, tipo string, porcentaje int, medio *string, alcance modelo.Alcance) modelo.Promocion {
	return modelo.Promocion{ID: id, Nombre: id, Tipo: tipo, Porcentaje: porcentaje, MedioPago: medio, Alcance: alcance,
		Vigencia: modelo.Vigencia{Desde: time.Now().Add(-time.Hour), Hasta: time.Now().Add(time.Hour)}}
}

func TestCatalogoPublicoAplicaElPrecioDeTransferencia(t *testing.T) {
	efectivo, transferencia := "EFECTIVO", "TRANSFERENCIA"
	repo := repoPublico{
		productos: []modelo.Producto{
			{ID: "prod-remera-basica", Categoria: "remeras", PrecioBase: modelo.Precio{Monto: 1500000}},
			{ID: "prod-jean-mom", Categoria: "jeans", PrecioBase: modelo.Precio{Monto: 4200000}},
		},
		promociones: []modelo.Promocion{
			promoPublica("promo-remeras", "PORCENTAJE", 20, nil, modelo.Alcance{Tipo: "CATEGORIA", IDs: []string{"remeras"}}),
			promoPublica("promo-efectivo", "MEDIO_PAGO", 50, &efectivo, modelo.Alcance{Tipo: "TIENDA"}),
			promoPublica("promo-transferencia", "MEDIO_PAGO", 5, &transferencia, modelo.Alcance{Tipo: "PRODUCTO", IDs: []string{"prod-jean-mom"}}),
		},
	}
	catalogo, err := Nuevo(repo).CatalogoPublico(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if catalogo[0].Precio != 1200000 {
		t.Errorf("remera con 20 %%: %d", catalogo[0].Precio)
	}
	if catalogo[1].Precio != 3990000 {
		t.Errorf("jean con 5 %% por transferencia, sin el 50 %% de efectivo: %d", catalogo[1].Precio)
	}
}

// Sin promociones no se sabe el precio real: mejor un error que mostrar uno que el pedido no va a cobrar.
func TestCatalogoPublicoFallaSiNoPuedeLeerLasPromociones(t *testing.T) {
	falla := errors.New("mongo caído")
	repo := repoPublico{productos: []modelo.Producto{{ID: "prod-remera-basica"}}, errPromocion: falla}
	if _, err := Nuevo(repo).CatalogoPublico(context.Background()); !errors.Is(err, falla) {
		t.Errorf("error: %v", err)
	}
}
