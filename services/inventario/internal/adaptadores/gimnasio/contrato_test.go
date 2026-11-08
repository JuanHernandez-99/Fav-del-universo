package gimnasio

// Test de contrato de la capacidad consumida (consigna 4.1, ADR-018). Compara el cliente con el YAML que publicó el
// gimnasio: si el contrato cambia de forma incompatible, o si el cliente se aparta de él, estos tests fallan.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/dominio"
)

const rutaContrato = "../../../../../docs/contracts/externos/gimnasio/gimnasio-consignaciones-1.0.0.openapi.yaml"

func contrato(t *testing.T) (*openapi3.T, routers.Router) {
	t.Helper()
	cargador := openapi3.NewLoader()
	doc, err := cargador.LoadFromFile(rutaContrato)
	if err != nil {
		t.Fatalf("no se pudo leer el contrato: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("contrato inválido: %v", err)
	}
	doc.Servers = nil // se valida la ruta, no la URL del entorno local que declara
	router, err := legacy.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}
	return doc, router
}

// servidorDelContrato valida cada pedido del cliente contra el YAML y responde lo que indique atender.
func servidorDelContrato(t *testing.T, router routers.Router, responder func(*http.Request) (int, []byte)) *Cliente {
	t.Helper()
	opciones := &openapi3filter.Options{AuthenticationFunc: func(_ context.Context, a *openapi3filter.AuthenticationInput) error {
		if a.RequestValidationInput.Request.Header.Get("X-API-Key") == "" {
			return errors.New("falta X-API-Key")
		}
		return nil
	}}
	servidor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cuerpo, _ := io.ReadAll(r.Body)
		copia := r.Clone(r.Context())
		copia.Body = io.NopCloser(strings.NewReader(string(cuerpo)))
		ruta, parametros, err := router.FindRoute(copia)
		if err != nil {
			t.Errorf("el cliente llamó a una ruta fuera del contrato: %s %s: %v", r.Method, r.URL, err)
			w.WriteHeader(500)
			return
		}
		entrada := &openapi3filter.RequestValidationInput{Request: copia, PathParams: parametros, Route: ruta, Options: opciones}
		if err := openapi3filter.ValidateRequest(r.Context(), entrada); err != nil {
			t.Errorf("el pedido no cumple el contrato: %v", err)
		}
		estado, respuesta := responder(r)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(estado)
		_, _ = w.Write(respuesta)
	}))
	t.Cleanup(servidor.Close)
	return NuevoCliente(Config{URLBase: servidor.URL, APIKey: "clave-de-prueba", ProveedorID: "fav", Timeout: time.Second})
}

func operacion(t *testing.T, doc *openapi3.T, ruta, metodo string) *openapi3.Operation {
	t.Helper()
	item := doc.Paths.Find(ruta)
	if item == nil || item.GetOperation(metodo) == nil {
		t.Fatalf("el contrato ya no tiene %s %s", metodo, ruta)
	}
	return item.GetOperation(metodo)
}

// ejemplosDe devuelve los ejemplos del contrato para una respuesta, con su nombre.
func ejemplosDe(t *testing.T, operacion *openapi3.Operation, estado string) map[string][]byte {
	t.Helper()
	respuesta := operacion.Responses.Value(estado)
	if respuesta == nil || respuesta.Value == nil {
		t.Fatalf("el contrato ya no documenta la respuesta %s", estado)
	}
	media := respuesta.Value.Content.Get("application/json")
	if media == nil {
		t.Fatalf("la respuesta %s ya no es JSON", estado)
	}
	ejemplos := map[string][]byte{}
	if media.Example != nil {
		ejemplos["ejemplo"] = aJSON(t, media.Example)
	}
	for nombre, e := range media.Examples {
		ejemplos[nombre] = aJSON(t, e.Value.Value)
	}
	if len(ejemplos) == 0 {
		t.Fatalf("la respuesta %s no tiene ejemplos", estado)
	}
	return ejemplos
}

func aJSON(t *testing.T, valor any) []byte {
	t.Helper()
	datos, err := json.Marshal(valor)
	if err != nil {
		t.Fatal(err)
	}
	return datos
}

func TestContratoRespuestasDelPost(t *testing.T) {
	doc, router := contrato(t)
	post := operacion(t, doc, rutaReposicion, http.MethodPost)
	casos := []struct {
		estado string
		espera error
	}{
		{"201", nil},
		{"400", dominio.ErrReposicionRechazada},
		{"401", dominio.ErrGimnasioConfiguracion},
		{"403", dominio.ErrGimnasioConfiguracion},
		{"409", dominio.ErrReposicionConflicto},
		{"413", dominio.ErrReposicionRechazada},
		{"415", dominio.ErrReposicionRechazada},
		{"422", dominio.ErrReposicionRechazada},
		{"500", errIncierto},
		{"503", errIncierto},
	}
	for _, c := range casos {
		for nombre, ejemplo := range ejemplosDe(t, post, c.estado) {
			t.Run(c.estado+"/"+nombre, func(t *testing.T) {
				estado, _ := strconv.Atoi(c.estado)
				cliente := servidorDelContrato(t, router, func(*http.Request) (int, []byte) { return estado, ejemplo })
				id, err := cliente.EnviarReposicion(context.Background(), reposicionDePrueba())
				switch c.espera {
				case nil:
					if err != nil || id == "" {
						t.Fatalf("la respuesta de éxito del contrato no se reconoce: id %q, %v", id, err)
					}
				case errIncierto:
					if err == nil || esDecisionDefinitiva(err) {
						t.Fatalf("tenía que ser incierto: %v", err)
					}
				default:
					if !errors.Is(err, c.espera) {
						t.Fatalf("se esperaba %v y dio %v (¿cambió un código del contrato?)", c.espera, err)
					}
				}
			})
		}
	}
}

func TestContratoRespuestasDelStock(t *testing.T) {
	doc, router := contrato(t)
	get := operacion(t, doc, rutaStock, http.MethodGet)
	for nombre, ejemplo := range ejemplosDe(t, get, "200") {
		t.Run("200/"+nombre, func(t *testing.T) {
			cliente := servidorDelContrato(t, router, func(*http.Request) (int, []byte) { return 200, ejemplo })
			if _, err := cliente.ConsultarStock(context.Background()); err != nil {
				t.Fatalf("el ejemplo del contrato no se entiende (¿se renombró un campo?): %v", err)
			}
		})
	}
	for _, estado := range []string{"400", "401", "403", "500", "503"} {
		for nombre, ejemplo := range ejemplosDe(t, get, estado) {
			t.Run(estado+"/"+nombre, func(t *testing.T) {
				codigo, _ := strconv.Atoi(estado)
				cliente := servidorDelContrato(t, router, func(*http.Request) (int, []byte) { return codigo, ejemplo })
				if _, err := cliente.ConsultarStock(context.Background()); !errors.Is(err, dominio.ErrProveedorNoDisponible) {
					t.Fatalf("un error del gimnasio no puede parecer stock: %v", err)
				}
			})
		}
	}
}

// Prueba que el test de contrato detecta un cambio incompatible: el mismo ejemplo, con un campo renombrado.
func TestContratoDetectaUnCampoRenombrado(t *testing.T) {
	doc, router := contrato(t)
	ejemplo := ejemplosDe(t, operacion(t, doc, rutaReposicion, http.MethodPost), "201")["ejemplo"]
	cambiado := []byte(strings.Replace(string(ejemplo), "reposicionId", "idReposicion", 1))
	cliente := servidorDelContrato(t, router, func(*http.Request) (int, []byte) { return 201, cambiado })
	if _, err := cliente.EnviarReposicion(context.Background(), reposicionDePrueba()); err == nil {
		t.Fatal("un 201 con un campo renombrado no puede darse por recibido")
	}
}
