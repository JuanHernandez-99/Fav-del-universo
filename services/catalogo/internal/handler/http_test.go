package handler

import (
	"errors"
	"net/url"
	"slices"
	"testing"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
)

func TestACentavos(t *testing.T) {
	casos := []struct {
		pesos    float64
		centavos int64
		ok       bool
	}{
		{15000, 1500000, true},
		{15999.9, 1599990, true},
		{0.29, 29, true}, // en coma flotante 0.29 * 100 da 28.999999999999996
		{-5, -500, true}, // el rango lo valida el servicio
		{10.555, 0, false},
		{1e30, 0, false}, // más que un int64 de centavos
	}
	for _, c := range casos {
		centavos, ok := aCentavos(c.pesos)
		if ok != c.ok || (ok && centavos != c.centavos) {
			t.Errorf("aCentavos(%v) = %d, %v; se esperaba %d, %v", c.pesos, centavos, ok, c.centavos, c.ok)
		}
	}
}

func TestParsearFiltro(t *testing.T) {
	q, _ := url.ParseQuery("categoria=jeans&talle=m&color=Celeste&precioMin=20000&precioMax=40000.5&publicado=true&pagina=2&tamano=5&orden=precio_asc&q=lino")
	f, err := parsearFiltro(q)
	if err != nil {
		t.Fatal(err)
	}
	if f.Categoria != "jeans" || f.Talle != "M" || f.Color != "Celeste" || f.Texto != "lino" ||
		f.Pagina != 2 || f.Tamano != 5 || f.Orden != modelo.OrdenPrecioAsc {
		t.Errorf("filtro mal armado: %+v", f)
	}
	if f.PrecioMin == nil || *f.PrecioMin != 2000000 || f.PrecioMax == nil || *f.PrecioMax != 4000050 {
		t.Errorf("los precios tienen que pasar de pesos a centavos: %v %v", f.PrecioMin, f.PrecioMax)
	}
	if f.Publicado == nil || !*f.Publicado {
		t.Error("publicado=true no se interpretó")
	}
}

func TestParsearFiltroPorDefecto(t *testing.T) {
	f, err := parsearFiltro(url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	if f.Pagina != 1 || f.Tamano != modelo.TamanoPorDefecto || f.Orden != modelo.OrdenNuevos || f.Publicado != nil {
		t.Errorf("valores por defecto incorrectos: %+v", f)
	}
}

func TestParsearFiltroErrores(t *testing.T) {
	casos := map[string][]string{
		"precioMin=abc":       {"precioMin"},
		"precioMax=10.555":    {"precioMax"},
		"precioMin=-1":        {"precioMin"},
		"precioMin=NaN":       {"precioMin"},
		"publicado=quizas":    {"publicado"},
		"pagina=x&tamano=y":   {"pagina", "tamano"},
		"categorias=jeans":    {"categorias"},
		"precioMin=a&talla=M": {"precioMin", "talla"},
	}
	for consulta, esperados := range casos {
		q, _ := url.ParseQuery(consulta)
		_, err := parsearFiltro(q)
		var validacion *modelo.ErrorValidacion
		if !errors.As(err, &validacion) {
			t.Errorf("%q: se esperaba un error de validación, llegó %v", consulta, err)
			continue
		}
		var campos []string
		for _, c := range validacion.Campos {
			campos = append(campos, c.Campo)
		}
		slices.Sort(campos) // el orden en que se reportan los errores no importa
		slices.Sort(esperados)
		if !slices.Equal(campos, esperados) {
			t.Errorf("%q: campos con error %v, se esperaba %v", consulta, campos, esperados)
		}
	}
}
