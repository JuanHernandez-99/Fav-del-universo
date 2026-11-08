package busqueda

// Tests de integración contra un Elasticsearch real (ADR-006). Necesitan ELASTICSEARCH_TEST_URL; sin ella se
// saltean. Usan un alias propio, así no tocan el índice del servicio:
//
//	ELASTICSEARCH_TEST_URL=http://localhost:9200 go test ./internal/busqueda/

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
)

var ctx = context.Background()

func clienteDePrueba(t *testing.T) *Cliente {
	t.Helper()
	urlBase := os.Getenv("ELASTICSEARCH_TEST_URL")
	if urlBase == "" {
		t.Skip("ELASTICSEARCH_TEST_URL no definida: se saltea el test de integración")
	}
	c := ConAlias(urlBase, fmt.Sprintf("prueba-%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		indices, _ := c.indicesDelAlias(ctx)
		for _, indice := range indices {
			_, _, _ = c.pedir(ctx, http.MethodDelete, "/"+indice, nil)
		}
	})
	creado, err := c.AsegurarIndice(ctx)
	if err != nil || !creado {
		t.Fatalf("creado %v, error %v", creado, err)
	}
	return c
}

func producto(id, nombre, categoria string, precio int64, version int, variantes ...modelo.Variante) modelo.Producto {
	return modelo.Producto{
		ID: id, Nombre: nombre, Categoria: categoria, PrecioBase: modelo.Precio{Monto: precio, Moneda: "ARS"},
		Publicado: true, Variantes: variantes, Version: version,
		CreadoEn: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(precio) * time.Second),
	}
}

func v(talle, color string) modelo.Variante {
	return modelo.Variante{ID: talle + color, Talle: talle, Color: color}
}

func catalogoDePrueba() []modelo.Producto {
	remera := producto("remera-basica", "Remera básica", "remeras", 150000, 1, v("M", "Blanco"), v("L", "Negro"))
	remera.Descripcion = "Remera de algodón, corte recto."
	return []modelo.Producto{
		remera,
		producto("remera-rayada", "Remera rayada", "remeras", 200000, 1, v("M", "Negro"), v("L", "Blanco")),
		producto("jean-mom", "Jean mom", "jeans", 500000, 1, v("38", "Celeste")),
		producto("buzo-algodon", "Buzo de algodón", "buzos", 300000, 1, v("M", "Gris")),
	}
}

func cargado(t *testing.T) *Cliente {
	t.Helper()
	c := clienteDePrueba(t)
	if err := c.Reconstruir(ctx, catalogoDePrueba()); err != nil {
		t.Fatal(err)
	}
	refrescar(t, c)
	return c
}

func refrescar(t *testing.T, c *Cliente) {
	t.Helper()
	if _, _, err := c.pedir(ctx, http.MethodPost, "/"+c.alias+"/_refresh", nil); err != nil {
		t.Fatal(err)
	}
}

func filtro(texto string) modelo.FiltroProductos {
	return modelo.FiltroProductos{Texto: texto, Orden: modelo.OrdenRelevancia, Pagina: 1, Tamano: 10}
}

func buscar(t *testing.T, c *Cliente, f modelo.FiltroProductos) []string {
	t.Helper()
	ids, total, err := c.Buscar(ctx, f)
	if err != nil {
		t.Fatal(err)
	}
	if f.Pagina == 1 && int(total) != len(ids) && len(ids) < f.Tamano {
		t.Fatalf("total %d con %d resultados", total, len(ids))
	}
	return ids
}

func TestBuscaConRelevanciaAcentosYErroresDeTipeo(t *testing.T) {
	c := cargado(t)
	casos := []struct {
		texto  string
		espera []string
	}{
		{"remera", []string{"remera-basica", "remera-rayada"}},
		{"BASICA", []string{"remera-basica"}},                        // sin acento y en mayúsculas
		{"remeras", []string{"remera-basica", "remera-rayada"}},      // plural
		{"remra", []string{"remera-basica", "remera-rayada"}},        // error de tipeo
		{"algodon", []string{"buzo-algodon", "remera-basica"}},       // en la descripción
		{"remera negro", []string{"remera-basica", "remera-rayada"}}, // entre el nombre y el color
		{"zapatilla", nil},
	}
	for _, caso := range casos {
		obtenido := buscar(t, c, filtro(caso.texto))
		esperado, ordenado := slices.Clone(caso.espera), slices.Clone(obtenido)
		slices.Sort(esperado)
		slices.Sort(ordenado)
		if !slices.Equal(ordenado, esperado) {
			t.Errorf("%q: %v, se esperaba %v", caso.texto, obtenido, caso.espera)
		}
	}
	// Coincidir en el nombre pesa más que en la descripción.
	if ids := buscar(t, c, filtro("algodon")); ids[0] != "buzo-algodon" {
		t.Errorf("relevancia: %v", ids)
	}
}

func TestFiltros(t *testing.T) {
	c := cargado(t)
	minimo, maximo := int64(160000), int64(250000)
	casos := []struct {
		nombre string
		cambio func(*modelo.FiltroProductos)
		espera []string
	}{
		{"categoría", func(f *modelo.FiltroProductos) { f.Texto, f.Categoria = "algodon", "buzos" }, []string{"buzo-algodon"}},
		{"talle", func(f *modelo.FiltroProductos) { f.Talle = "L" }, []string{"remera-basica", "remera-rayada"}},
		// Talle y color tienen que estar en la misma variante: la básica es M blanco y L negro.
		{"talle y color", func(f *modelo.FiltroProductos) { f.Talle, f.Color = "M", "Blanco" }, []string{"remera-basica"}},
		{"color sin mayúscula", func(f *modelo.FiltroProductos) { f.Color = "negro" }, []string{"remera-basica", "remera-rayada"}},
		{"precio", func(f *modelo.FiltroProductos) { f.PrecioMin, f.PrecioMax = &minimo, &maximo }, []string{"remera-rayada"}},
	}
	for _, caso := range casos {
		f := filtro("remera")
		caso.cambio(&f)
		obtenido := buscar(t, c, f)
		slices.Sort(obtenido)
		if !slices.Equal(obtenido, caso.espera) {
			t.Errorf("%s: %v, se esperaba %v", caso.nombre, obtenido, caso.espera)
		}
	}
}

func TestOrdenYPaginacionSinRepetidos(t *testing.T) {
	c := cargado(t)
	f := filtro("remera")
	for orden, espera := range map[modelo.Orden][]string{
		modelo.OrdenPrecioAsc:  {"remera-basica", "remera-rayada"},
		modelo.OrdenPrecioDesc: {"remera-rayada", "remera-basica"},
		modelo.OrdenNuevos:     {"remera-rayada", "remera-basica"},
		modelo.OrdenNombre:     {"remera-basica", "remera-rayada"},
	} {
		f.Orden = orden
		if ids := buscar(t, c, f); !slices.Equal(ids, espera) {
			t.Errorf("%s: %v", orden, ids)
		}
	}
	f.Orden, f.Tamano = modelo.OrdenPrecioAsc, 1
	primera := buscar(t, c, f)
	f.Pagina = 2
	segunda := buscar(t, c, f)
	if len(primera) != 1 || len(segunda) != 1 || primera[0] == segunda[0] {
		t.Errorf("páginas: %v y %v", primera, segunda)
	}
}

func TestPaginaFueraDeLaVentanaEsUnErrorDeValidacion(t *testing.T) {
	c := clienteDePrueba(t)
	f := filtro("remera")
	f.Pagina, f.Tamano = 1000, 100
	var validacion *modelo.ErrorValidacion
	if _, _, err := c.Buscar(ctx, f); !errors.As(err, &validacion) {
		t.Fatalf("error: %v", err)
	}
}

// Un evento repetido o viejo no pisa un dato más nuevo.
func TestVersionadoExterno(t *testing.T) {
	c := clienteDePrueba(t)
	nueva := producto("remera-x", "Remera nueva", "remeras", 100, 5, v("M", "Blanco"))
	vieja := producto("remera-x", "Remera vieja", "remeras", 100, 3, v("M", "Blanco"))
	for _, p := range []modelo.Producto{nueva, nueva, vieja} {
		if err := c.Indexar(ctx, p); err != nil {
			t.Fatalf("una versión repetida o vieja no es un error: %v", err)
		}
	}
	refrescar(t, c)
	if ids := buscar(t, c, filtro("nueva")); len(ids) != 1 {
		t.Fatalf("se perdió la versión nueva: %v", ids)
	}
	if ids := buscar(t, c, filtro("vieja")); len(ids) != 0 {
		t.Fatalf("una versión vieja pisó a la nueva: %v", ids)
	}

	if err := c.Quitar(ctx, "remera-x", 4); err != nil {
		t.Fatal(err)
	}
	refrescar(t, c)
	if ids := buscar(t, c, filtro("nueva")); len(ids) != 1 {
		t.Fatalf("un borrado viejo quitó el producto: %v", ids)
	}
	if err := c.Quitar(ctx, "remera-x", 6); err != nil {
		t.Fatal(err)
	}
	if err := c.Quitar(ctx, "no-existe", 1); err != nil {
		t.Fatalf("quitar lo que no está no es un error: %v", err)
	}
	refrescar(t, c)
	if ids := buscar(t, c, filtro("nueva")); len(ids) != 0 {
		t.Fatalf("no se quitó: %v", ids)
	}
}

func TestReconstruirCambiaElAliasYBorraElAnterior(t *testing.T) {
	c := cargado(t)
	antes, err := c.indicesDelAlias(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Reconstruir(ctx, []modelo.Producto{producto("solo-esta", "Campera", "camperas", 100, 1, v("M", "Azul"))}); err != nil {
		t.Fatal(err)
	}
	despues, err := c.indicesDelAlias(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(despues) != 1 || slices.Contains(antes, despues[0]) {
		t.Fatalf("alias antes %v, después %v", antes, despues)
	}
	// Sin refrescar a mano: al cambiar el alias, el índice nuevo ya tiene que responder (búsqueda sin corte).
	if ids := buscar(t, c, filtro("remera")); len(ids) != 0 {
		t.Fatalf("quedaron datos del índice anterior: %v", ids)
	}
	if ids := buscar(t, c, filtro("campera")); len(ids) != 1 {
		t.Fatalf("no está el dato nuevo: %v", ids)
	}
	estado, _, _ := c.pedir(ctx, http.MethodHead, "/"+antes[0], nil)
	if estado != http.StatusNotFound {
		t.Fatalf("el índice anterior sigue: %d", estado)
	}
}

func TestSegundaReplicaNoCreaElIndice(t *testing.T) {
	c := clienteDePrueba(t)
	if creado, err := c.AsegurarIndice(ctx); err != nil || creado {
		t.Fatalf("creado %v, error %v", creado, err)
	}
}

func TestSinElasticsearchEsNoDisponible(t *testing.T) {
	c := Nuevo("http://127.0.0.1:1")
	if _, _, err := c.Buscar(ctx, filtro("remera")); !errors.Is(err, ErrNoDisponible) {
		t.Fatalf("error: %v", err)
	}
}
