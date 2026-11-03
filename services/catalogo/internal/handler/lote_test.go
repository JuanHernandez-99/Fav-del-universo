package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/service"
)

// repoFalso responde solo la consulta por lote; el resto de la interfaz queda nil.
type repoFalso struct{ service.Repositorio }

func (repoFalso) ObtenerVariantes(_ context.Context, ids []string) ([]modelo.DetalleVariante, error) {
	var encontradas []modelo.DetalleVariante
	for _, id := range ids {
		if id == "var-remera-basica-blanca-m" {
			encontradas = append(encontradas, modelo.DetalleVariante{
				VarianteID: id, ProductoID: "prod-remera-basica", Nombre: "Remera básica", Categoria: "remeras",
				Talle: "M", Color: "Blanco", Publicado: true, PrecioBase: modelo.Precio{Monto: 1599990, Moneda: "ARS"},
			})
		}
	}
	return encontradas, nil
}

func pedir(t *testing.T, ruta string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	NuevoRouter(service.Nuevo(repoFalso{})).ServeHTTP(w, httptest.NewRequest(http.MethodGet, ruta, nil))
	var cuerpo map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &cuerpo); err != nil {
		t.Fatalf("la respuesta no es JSON (%d): %s", w.Code, w.Body)
	}
	return w.Code, cuerpo
}

func TestConsultarVariantesHTTP(t *testing.T) {
	codigo, cuerpo := pedir(t, "/v1/variantes?variantes=var-remera-basica-blanca-m")
	if codigo != 200 {
		t.Fatalf("código %d: %v", codigo, cuerpo)
	}
	item := cuerpo["items"].([]any)[0].(map[string]any)
	// El precio sale en pesos con decimales (el monto interno son 1.599.990 centavos).
	esperado := map[string]any{"varianteId": "var-remera-basica-blanca-m", "productoId": "prod-remera-basica", "nombre": "Remera básica",
		"categoria": "remeras", "talle": "M", "color": "Blanco", "publicado": true, "precio": 15999.9}
	for campo, valor := range esperado {
		if item[campo] != valor {
			t.Errorf("%s = %v, se esperaba %v", campo, item[campo], valor)
		}
	}
}

func TestConsultarVariantesErroresHTTP(t *testing.T) {
	casos := []struct {
		nombre, ruta, codigo string
		status               int
	}{
		{"id inexistente", "/v1/variantes?variantes=var-remera-basica-blanca-m,no-existe", "VARIANTE_INEXISTENTE", 400},
		{"sin el parámetro", "/v1/variantes", "PEDIDO_INVALIDO", 400},
		{"parámetro desconocido", "/v1/variantes?variantes=a&color=rojo", "PEDIDO_INVALIDO", 400},
		{"medio de pago inválido", "/v1/variantes?variantes=a&medioPago=cheque", "PEDIDO_INVALIDO", 400},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			status, cuerpo := pedir(t, c.ruta)
			if status != c.status || cuerpo["codigo"] != c.codigo {
				t.Fatalf("%d %v; se esperaba %d %s", status, cuerpo, c.status, c.codigo)
			}
		})
	}

	_, cuerpo := pedir(t, "/v1/variantes?variantes=var-remera-basica-blanca-m,no-existe")
	if faltan := cuerpo["variantes"].([]any); len(faltan) != 1 || faltan[0] != "no-existe" {
		t.Errorf("la respuesta tiene que listar los ids que faltan en `variantes`: %v", cuerpo)
	}
}
