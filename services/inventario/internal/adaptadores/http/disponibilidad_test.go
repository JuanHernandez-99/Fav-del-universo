package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/aplicacion"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/dominio"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/puertos"
)

// repoStock responde solo la consulta de stock; el resto de la interfaz queda nil.
type repoStock struct {
	puertos.Repositorio
	stocks []dominio.Stock
	err    error
}

func (r repoStock) ConsultarStock(context.Context, []string) ([]dominio.Stock, error) {
	return r.stocks, r.err
}

func pedirDisponibilidad(repo repoStock, ruta string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	NuevoRouter(aplicacion.NuevoServicio(repo, nil)).ServeHTTP(w, httptest.NewRequest(http.MethodGet, ruta, nil))
	return w
}

// Las reservadas no cuentan y la respuesta no deja ver ninguna cantidad (HU-01.3). Respeta el orden pedido,
// sin repetidos, y una variante sin fila de stock figura sin stock.
func TestDisponibilidadSoloDiceSiHay(t *testing.T) {
	repo := repoStock{stocks: []dominio.Stock{
		{VarianteID: "var-jean-celeste-38", Online: 0, Fisico: 40},
		{VarianteID: "var-remera-blanca-m", Online: 3, Fisico: 40, Reservado: 1},
		{VarianteID: "var-remera-negra-l", Online: 2, Fisico: 40, Reservado: 2},
	}}
	w := pedirDisponibilidad(repo, "/v1/disponibilidad?variantes=var-remera-negra-l,var-remera-blanca-m,var-jean-celeste-38,var-sin-fila,var-remera-blanca-m")
	if w.Code != 200 {
		t.Fatalf("código %d: %s", w.Code, w.Body)
	}
	esperado := `{"items":[{"varianteId":"var-remera-negra-l","hayStock":false},{"varianteId":"var-remera-blanca-m","hayStock":true},` +
		`{"varianteId":"var-jean-celeste-38","hayStock":false},{"varianteId":"var-sin-fila","hayStock":false}]}`
	if obtenido := strings.TrimSpace(w.Body.String()); obtenido != esperado {
		t.Errorf("respuesta:\n%s", obtenido)
	}
}

func TestDisponibilidadValidaLosIds(t *testing.T) {
	var demasiados []string
	for i := range 201 {
		demasiados = append(demasiados, fmt.Sprintf("var-%d", i))
	}
	for _, ruta := range []string{"/v1/disponibilidad", "/v1/disponibilidad?variantes=,,", "/v1/disponibilidad?variantes=" + strings.Join(demasiados, ",")} {
		if w := pedirDisponibilidad(repoStock{}, ruta); w.Code != 400 {
			t.Errorf("%.40s: código %d", ruta, w.Code)
		}
	}
}

// Si no se puede leer el stock no se responde "no hay": sería un dato falso.
func TestDisponibilidadInformaLaFalla(t *testing.T) {
	if w := pedirDisponibilidad(repoStock{err: errors.New("mysql caído")}, "/v1/disponibilidad?variantes=var-a"); w.Code != 500 {
		t.Errorf("código %d: %s", w.Code, w.Body)
	}
}
