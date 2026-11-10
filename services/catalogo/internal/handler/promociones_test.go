package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/service"
)

type repoDePromociones struct {
	repoFalso
	guardada *modelo.Promocion
}

func (repoDePromociones) ExisteCategoria(context.Context, string) (bool, error) { return true, nil }

func (r repoDePromociones) InsertarPromocion(_ context.Context, p modelo.Promocion) error {
	*r.guardada = p
	return nil
}

func (repoDePromociones) ReemplazarPromocion(context.Context, modelo.Promocion) error {
	return modelo.ErrPromocionNoEncontrada
}

func (repoDePromociones) ListarPromociones(context.Context) ([]modelo.Promocion, error) {
	return nil, nil
}

func pedirPromocion(t *testing.T, metodo, ruta, cuerpo string) (*httptest.ResponseRecorder, *modelo.Promocion, map[string]any) {
	t.Helper()
	guardada := &modelo.Promocion{}
	w := httptest.NewRecorder()
	NuevoRouter(service.Nuevo(repoDePromociones{guardada: guardada})).ServeHTTP(w, httptest.NewRequest(metodo, ruta, strings.NewReader(cuerpo)))
	var respuesta map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &respuesta); err != nil {
		t.Fatalf("no es JSON (%d): %s", w.Code, w.Body)
	}
	return w, guardada, respuesta
}

const promocionValida = `{"nombre":"Verano en remeras","tipo":"PORCENTAJE","porcentaje":20,"medioPago":null,
	"vigencia":{"desde":"2026-12-01T00:00:00-03:00","hasta":"2026-12-31T23:59:59-03:00"},
	"alcance":{"tipo":"CATEGORIA","ids":["remeras"]}}`

func TestCrearPromocionHTTP(t *testing.T) {
	w, guardada, r := pedirPromocion(t, http.MethodPost, "/v1/promociones", promocionValida)
	if w.Code != 201 || w.Header().Get("Location") != "/v1/promociones/promo-verano-en-remeras" {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if r["promocionId"] != "promo-verano-en-remeras" || r["porcentaje"] != 20.0 || guardada.Porcentaje != 20 {
		t.Errorf("respuesta %v, guardada %+v", r, guardada)
	}
	if hasta := r["vigencia"].(map[string]any)["hasta"]; hasta != "2027-01-01T02:59:59Z" {
		t.Errorf("hasta en UTC: %v", hasta)
	}
}

func TestPromocionSinVigenciaEs400(t *testing.T) {
	w, _, r := pedirPromocion(t, http.MethodPost, "/v1/promociones",
		`{"nombre":"Sin fechas","tipo":"PORCENTAJE","porcentaje":20,"alcance":{"tipo":"TIENDA","ids":[]}}`)
	if w.Code != 400 || r["codigo"] != "PEDIDO_INVALIDO" || len(r["errores"].([]any)) != 2 {
		t.Errorf("%d %v (HU-05.2)", w.Code, r)
	}
}

func TestNoSePuedeForzarElID(t *testing.T) {
	w, _, r := pedirPromocion(t, http.MethodPost, "/v1/promociones", `{"promocionId":"promo-trucha","nombre":"x"}`)
	if w.Code != 400 || r["codigo"] != "PEDIDO_INVALIDO" {
		t.Errorf("%d %v", w.Code, r)
	}
}

func TestActualizarPromocionInexistenteEs404(t *testing.T) {
	w, _, r := pedirPromocion(t, http.MethodPut, "/v1/promociones/promo-nada", promocionValida)
	if w.Code != 404 || r["codigo"] != "PROMOCION_NO_ENCONTRADA" {
		t.Errorf("%d %v", w.Code, r)
	}
}

func TestListarPromocionesVacioYParametroDesconocido(t *testing.T) {
	w, _, r := pedirPromocion(t, http.MethodGet, "/v1/promociones", "")
	if w.Code != 200 || len(r["items"].([]any)) != 0 {
		t.Errorf("%d %v", w.Code, r)
	}
	w, _, _ = pedirPromocion(t, http.MethodGet, "/v1/promociones?vigentes=true", "")
	if w.Code != 400 {
		t.Errorf("parámetro desconocido: %d", w.Code)
	}
}
