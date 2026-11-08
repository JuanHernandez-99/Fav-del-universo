package http

// Comprueba que GET /v1/disponibilidad cumple el contrato publicado (versión 1.1.0, ADR-008). La clave y el límite
// de uso los aplica el gateway: acá se valida lo que responde Inventario.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/dominio"
)

const contratoPublicado = "../../../../../docs/contracts/catalogo-pedidos-v1.openapi.yaml"

func TestDisponibilidadCumpleElContrato(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromFile(contratoPublicado)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("contrato inválido: %v", err)
	}
	doc.Servers = nil
	router, err := legacy.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}
	blanca := dominio.Stock{VarianteID: "var-remera-basica-blanca-m", Online: 5, Reservado: 2}
	var ids []string
	for i := range 201 {
		ids = append(ids, fmt.Sprintf("var-%d", i))
	}
	muchos := strings.Join(ids, ",")

	casos := []struct {
		nombre string
		ruta   string
		repo   repoStock
		estado int
	}{
		{"con stock y sin fila de stock", "/v1/disponibilidad?variantes=var-remera-basica-blanca-m,var-jean-mom-celeste-38", repoStock{stocks: []dominio.Stock{blanca}}, 200},
		{"sin variantes", "/v1/disponibilidad?variantes=", repoStock{}, 400},
		{"más de 200", "/v1/disponibilidad?variantes=" + muchos, repoStock{}, 400},
		{"la base no responde", "/v1/disponibilidad?variantes=a", repoStock{err: context.DeadlineExceeded}, 503},
		{"error inesperado", "/v1/disponibilidad?variantes=a", repoStock{err: errors.New("falla")}, 500},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			w := pedirDisponibilidad(c.repo, c.ruta)
			if w.Code != c.estado {
				t.Fatalf("estado %d, se esperaba %d: %s", w.Code, c.estado, w.Body)
			}
			validarRespuesta(t, router, c.ruta, w)
		})
	}
}

func validarRespuesta(t *testing.T, router routers.Router, ruta string, w *httptest.ResponseRecorder) {
	t.Helper()
	pedido := httptest.NewRequest(http.MethodGet, ruta, nil)
	encontrada, parametros, err := router.FindRoute(pedido)
	if err != nil {
		t.Fatal(err)
	}
	entrada := &openapi3filter.RequestValidationInput{Request: pedido, PathParams: parametros, Route: encontrada,
		Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc}}
	salida := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: entrada, Status: w.Code, Header: w.Header(),
		Body: nopCloser{bytes.NewReader(w.Body.Bytes())},
	}
	if err := openapi3filter.ValidateResponse(context.Background(), salida); err != nil {
		t.Fatalf("la respuesta no cumple el contrato: %v\n%s", err, w.Body)
	}
}

type nopCloser struct{ *bytes.Reader }

func (nopCloser) Close() error { return nil }
