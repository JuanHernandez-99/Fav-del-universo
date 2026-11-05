package http

import (
	"context"
	"encoding/json"
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

func (r repoStock) StockACorregir(_ context.Context, limite int) ([]dominio.Stock, error) {
	return r.stocks[:min(limite, len(r.stocks))], r.err
}

func TestStockACorregirInformaSiHayMas(t *testing.T) {
	var muchas []dominio.Stock
	for i := range aplicacion.MaximoACorregir + 1 {
		muchas = append(muchas, dominio.Stock{VarianteID: fmt.Sprintf("var-%d", i), Fisico: -1})
	}
	for _, c := range []struct {
		stocks []dominio.Stock
		items  int
		hayMas bool
	}{{muchas[:2], 2, false}, {muchas, aplicacion.MaximoACorregir, true}} {
		w := pedirDisponibilidad(repoStock{stocks: c.stocks}, "/v1/stock/a-corregir")
		var cuerpo struct {
			Items []struct {
				StockACorregir bool `json:"stockACorregir"`
			}
			HayMas bool
		}
		if err := json.Unmarshal(w.Body.Bytes(), &cuerpo); err != nil || w.Code != 200 {
			t.Fatalf("código %d: %v", w.Code, err)
		}
		if len(cuerpo.Items) != c.items || cuerpo.HayMas != c.hayMas || !cuerpo.Items[0].StockACorregir {
			t.Errorf("con %d negativas: %d items, hayMas %v", len(c.stocks), len(cuerpo.Items), cuerpo.HayMas)
		}
	}
}

func pedirDisponibilidad(repo repoStock, ruta string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	NuevoRouter(aplicacion.NuevoServicio(repo, nil)).ServeHTTP(w, httptest.NewRequest(http.MethodGet, ruta, nil))
	return w
}

// Las reservadas no cuentan y el físico no se informa (HU-01.5). Respeta el orden pedido, sin repetidos,
// y una variante sin fila de stock figura con 0.
func TestDisponibilidadInformaLasUnidadesOnline(t *testing.T) {
	repo := repoStock{stocks: []dominio.Stock{
		{VarianteID: "var-jean-celeste-38", Online: 0, Fisico: 40},
		{VarianteID: "var-remera-blanca-m", Online: 3, Fisico: 40, Reservado: 1},
		{VarianteID: "var-remera-negra-l", Online: 2, Fisico: 40, Reservado: 2},
	}}
	w := pedirDisponibilidad(repo, "/v1/disponibilidad?variantes=var-remera-negra-l,var-remera-blanca-m,var-jean-celeste-38,var-sin-fila,var-remera-blanca-m")
	if w.Code != 200 {
		t.Fatalf("código %d: %s", w.Code, w.Body)
	}
	esperado := `{"items":[{"varianteId":"var-remera-negra-l","disponible":0},{"varianteId":"var-remera-blanca-m","disponible":2},` +
		`{"varianteId":"var-jean-celeste-38","disponible":0},{"varianteId":"var-sin-fila","disponible":0}]}`
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

// Si no se puede leer el stock no se responde 0: sería un dato falso.
func TestDisponibilidadInformaLaFalla(t *testing.T) {
	if w := pedirDisponibilidad(repoStock{err: errors.New("mysql caído")}, "/v1/disponibilidad?variantes=var-a"); w.Code != 500 {
		t.Errorf("código %d: %s", w.Code, w.Body)
	}
}
