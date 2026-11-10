package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/service"
)

type repoTienda struct {
	repoFalso
	productos []modelo.Producto
	promos    []modelo.Promocion
	filtro    *modelo.FiltroProductos
}

func (r repoTienda) ListarProductos(_ context.Context, f modelo.FiltroProductos) ([]modelo.Producto, int64, error) {
	if r.filtro != nil {
		*r.filtro = f
	}
	return r.productos, int64(len(r.productos)), nil
}

func (r repoTienda) ObtenerProducto(_ context.Context, id string) (modelo.Producto, error) {
	for _, p := range r.productos {
		if p.ID == id {
			return p, nil
		}
	}
	return modelo.Producto{}, modelo.ErrProductoNoEncontrado
}

func (r repoTienda) PromocionesVigentes(context.Context, time.Time) ([]modelo.Promocion, error) {
	return r.promos, nil
}

func pedirTienda(t *testing.T, repo service.Repositorio, ruta string, opciones ...Opcion) (int, []byte) {
	t.Helper()
	w := httptest.NewRecorder()
	NuevoRouter(service.Nuevo(repo), opciones...).ServeHTTP(w, httptest.NewRequest(http.MethodGet, ruta, nil))
	return w.Code, w.Body.Bytes()
}

// El comprador ve fotos por color, precio original y con promoción (HU-01), sin datos internos.
func TestTiendaMuestraLoQueVeElComprador(t *testing.T) {
	blanco := "Blanco"
	remera := remeraYJean()[0]
	remera.Fotos = []modelo.Foto{{URL: "https://fav.example/general.jpg"}, {URL: "https://fav.example/blanca.jpg", Color: &blanco}}
	remera.Variantes = remera.Variantes[:1]
	repo := repoTienda{productos: []modelo.Producto{remera}, promos: []modelo.Promocion{{
		ID: "promo-remeras", Nombre: "Verano", Tipo: "PORCENTAJE", Porcentaje: 10,
		Alcance:  modelo.Alcance{Tipo: "CATEGORIA", IDs: []string{"remeras"}},
		Vigencia: modelo.Vigencia{Desde: time.Now().Add(-time.Hour), Hasta: time.Now().Add(time.Hour)},
	}}}
	codigo, cuerpo := pedirTienda(t, repo, "/v1/tienda/productos/prod-remera-basica")
	if codigo != 200 {
		t.Fatalf("código %d: %s", codigo, cuerpo)
	}
	var obtenido, esperado any
	if err := json.Unmarshal(cuerpo, &obtenido); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"productoId":"prod-remera-basica","nombre":"Remera básica","descripcion":"De algodón.","categoria":"remeras",
		"precio":14399.91,"precioBase":15999.9,"promocion":{"promocionId":"promo-remeras","nombre":"Verano","porcentaje":10},
		"fotos":[{"url":"https://fav.example/general.jpg","color":null},{"url":"https://fav.example/blanca.jpg","color":"Blanco"}],
		"variantes":[{"varianteId":"var-remera-basica-blanca-m","talle":"M","color":"Blanco"}]}`), &esperado); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(obtenido, esperado) {
		t.Errorf("respuesta inesperada:\n%s", cuerpo)
	}
}

func TestTiendaListaSoloPublicadosConLosFiltrosRecibidos(t *testing.T) {
	var filtro modelo.FiltroProductos
	repo := repoTienda{productos: remeraYJean(), filtro: &filtro}
	codigo, cuerpo := pedirTienda(t, repo, "/v1/tienda/productos?categoria=remeras&talle=m&orden=precio_asc&pagina=2&tamano=1")
	if codigo != 200 {
		t.Fatalf("código %d: %s", codigo, cuerpo)
	}
	if filtro.Publicado == nil || !*filtro.Publicado {
		t.Errorf("no filtró por publicados: %+v", filtro.Publicado)
	}
	if filtro.Categoria != "remeras" || filtro.Talle != "M" || filtro.Orden != modelo.OrdenPrecioAsc || filtro.Pagina != 2 || filtro.Tamano != 1 {
		t.Errorf("filtro: %+v", filtro)
	}
	var pagina struct {
		Items                          []productoTiendaJSON
		Pagina, Tamano, Total, Paginas int
	}
	if err := json.Unmarshal(cuerpo, &pagina); err != nil {
		t.Fatal(err)
	}
	if len(pagina.Items) != 2 || pagina.Pagina != 2 || pagina.Total != 2 || pagina.Paginas != 2 {
		t.Errorf("página: %s", cuerpo)
	}
}

// Pedir los no publicados no es una opción del comprador (HU-01.2).
func TestTiendaRechazaElParametroPublicado(t *testing.T) {
	codigo, cuerpo := pedirTienda(t, repoTienda{}, "/v1/tienda/productos?publicado=false")
	if codigo != 400 || !strings.Contains(string(cuerpo), `"campo":"publicado"`) {
		t.Errorf("código %d: %s", codigo, cuerpo)
	}
}

func TestTiendaNoMuestraLaFichaDeUnNoPublicado(t *testing.T) {
	jean := remeraYJean()[1]
	jean.Publicado = false
	codigo, cuerpo := pedirTienda(t, repoTienda{productos: []modelo.Producto{jean}}, "/v1/tienda/productos/prod-jean-mom")
	if codigo != 404 || !strings.Contains(string(cuerpo), "PRODUCTO_NO_ENCONTRADO") {
		t.Errorf("código %d: %s", codigo, cuerpo)
	}
}

func TestTiendaVaciaEsListaVacia(t *testing.T) {
	_, cuerpo := pedirTienda(t, repoTienda{}, "/v1/tienda/productos")
	if !strings.Contains(string(cuerpo), `"items":[]`) {
		t.Errorf("sin resultados: %s", cuerpo)
	}
}

// Apagar lo que se le ofrece al otro grupo no apaga la tienda.
func TestTiendaSigueConLaCapacidadPublicaApagada(t *testing.T) {
	if codigo, cuerpo := pedirTienda(t, repoTienda{}, "/v1/tienda/productos", SinCapacidadPublica()); codigo != 200 {
		t.Errorf("código %d: %s", codigo, cuerpo)
	}
}
