package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/service"
)

type repoPublico struct {
	repoFalso
	productos []modelo.Producto
	promos    []modelo.Promocion
}

func (r repoPublico) ProductosPublicados(context.Context) ([]modelo.Producto, error) {
	return r.productos, nil
}

func (r repoPublico) PromocionesVigentes(context.Context, time.Time) ([]modelo.Promocion, error) {
	return r.promos, nil
}

func remeraYJean() []modelo.Producto {
	return []modelo.Producto{
		{ID: "prod-remera-basica", Nombre: "Remera básica", Categoria: "remeras", Descripcion: "De algodón.",
			SistemaTalles: "LETRAS", PrecioBase: modelo.Precio{Monto: 1599990, Moneda: "ARS"}, Publicado: true,
			Atributos: map[string]any{"material": "algodón"},
			Fotos:     []modelo.Foto{{URL: "https://fav.example/principal.jpg"}, {URL: "https://fav.example/otra.jpg"}},
			Variantes: []modelo.Variante{{ID: "var-remera-basica-blanca-m", Talle: "M", Color: "Blanco"}, {ID: "var-remera-basica-negra-l", Talle: "L", Color: "Negro"}},
			Version:   3},
		{ID: "prod-jean-mom", Nombre: "Jean mom", Categoria: "jeans", PrecioBase: modelo.Precio{Monto: 4200000, Moneda: "ARS"}, Publicado: true,
			Variantes: []modelo.Variante{{ID: "var-jean-mom-celeste-38", Talle: "38", Color: "Celeste"}}},
	}
}

func pedirPublico(t *testing.T, h http.Handler) (int, []byte) {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/productos", nil))
	return w.Code, w.Body.Bytes()
}

// La forma exacta del contrato: ni stock, ni datos internos como versión o sistema de talles.
func TestCatalogoPublicoSigueElContrato(t *testing.T) {
	codigo, cuerpo := pedirPublico(t, NuevoRouter(service.Nuevo(repoPublico{productos: remeraYJean()})))
	if codigo != 200 {
		t.Fatalf("código %d: %s", codigo, cuerpo)
	}
	var obtenido, esperado any
	if err := json.Unmarshal(cuerpo, &obtenido); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"items":[
		{"productoId":"prod-remera-basica","nombre":"Remera básica","categoria":"remeras","descripcion":"De algodón.",
		 "fotoUrl":"https://fav.example/principal.jpg","variantes":[
			{"varianteId":"var-remera-basica-blanca-m","talle":"M","color":"Blanco","precio":15999.9},
			{"varianteId":"var-remera-basica-negra-l","talle":"L","color":"Negro","precio":15999.9}]},
		{"productoId":"prod-jean-mom","nombre":"Jean mom","categoria":"jeans","variantes":[
			{"varianteId":"var-jean-mom-celeste-38","talle":"38","color":"Celeste","precio":42000}]}
	]}`), &esperado); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(obtenido, esperado) {
		t.Errorf("respuesta distinta del contrato:\n%s", cuerpo)
	}
}

func TestCatalogoPublicoVacioEsListaVacia(t *testing.T) {
	_, cuerpo := pedirPublico(t, NuevoRouter(service.Nuevo(repoPublico{})))
	if string(cuerpo) != "{\"items\":[]}\n" {
		t.Errorf("sin publicados: %s", cuerpo)
	}
}

// ADR-008: la capacidad publicada se puede apagar sin tocar la API de administración.
func TestCatalogoPublicoSePuedeApagar(t *testing.T) {
	h := NuevoRouter(service.Nuevo(repoPublico{}), SinCapacidadPublica())
	if codigo, _ := pedirPublico(t, h); codigo != 404 {
		t.Errorf("apagada, código %d", codigo)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if w.Code != 200 {
		t.Errorf("el resto sigue: código %d", w.Code)
	}
}
