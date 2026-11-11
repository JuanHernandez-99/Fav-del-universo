package http

// Comprueba que POST /v1/pedidos y GET /v1/pedidos/{pedidoId} cumplen el contrato publicado (versión 1.3.0,
// ADR-008). La clave y el límite de uso los aplica el gateway: acá se valida lo que responde Ventas.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"

	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/aplicacion"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/dominio"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/puertos"
)

const contratoPublicado = "../../../../../docs/contracts/catalogo-pedidos-v1.openapi.yaml"

// ---------- Falsos: solo lo que usan el alta y la consulta del pedido ----------

type repoFalso struct {
	puertos.Repositorio // lo no implementado entra en pánico: el test no debe llegar ahí
	ventas              map[string]dominio.Venta
	err                 error
}

func (r *repoFalso) ObtenerVenta(_ context.Context, id string) (dominio.Venta, error) {
	if r.err != nil {
		return dominio.Venta{}, r.err
	}
	v, ok := r.ventas[id]
	if !ok {
		return dominio.Venta{}, dominio.ErrVentaNoEncontrada
	}
	return v, nil
}

func (r *repoFalso) VentaPorClave(_ context.Context, clave string) (dominio.Venta, error) {
	for _, v := range r.ventas {
		if v.Clave == clave {
			return v, nil
		}
	}
	return dominio.Venta{}, dominio.ErrVentaNoEncontrada
}

func (r *repoFalso) CrearVenta(_ context.Context, v dominio.Venta, _ []dominio.TipoTarea, _ []dominio.Mail) (dominio.Venta, bool, error) {
	r.ventas[v.ID] = v
	return v, false, nil
}

type catalogoFalso struct{}

func (catalogoFalso) Variantes(_ context.Context, ids []string, _ dominio.Medio) ([]puertos.DetalleVariante, error) {
	detalles := make([]puertos.DetalleVariante, len(ids))
	for i, id := range ids {
		detalles[i] = puertos.DetalleVariante{VarianteID: id, Nombre: "Remera básica", Talle: "M", Color: "Blanco", Publicado: true, Precio: 1500000}
	}
	return detalles, nil
}

type inventarioFalso struct{ puertos.Inventario }

func (inventarioFalso) Reservar(_ context.Context, clave, _ string, _ []dominio.LineaPedido, vigencia time.Duration) (puertos.Reserva, error) {
	ahora := time.Now().UTC().Truncate(time.Second)
	return puertos.Reserva{ID: "res-" + clave, Estado: puertos.ReservaActiva, VenceEn: ahora.Add(vigencia), CreadaEn: ahora}, nil
}

func armarRouter(repo *repoFalso, opciones ...Opcion) http.Handler {
	s := aplicacion.NuevoServicio(repo, catalogoFalso{}, inventarioFalso{}, nil, nil, "admin@fav.example")
	return NuevoRouter(s, DatosTransferencia{Alias: "FAV.ALIAS", Titular: "Cielo Stellantis"}, opciones...)
}

func ventaDe(id string, canal dominio.Canal, estado dominio.Estado) dominio.Venta {
	creada := time.Date(2026, 11, 10, 15, 0, 0, 0, time.UTC)
	vence := creada.Add(48 * time.Hour)
	return dominio.Venta{
		ID: id, Clave: "grupo:clave-" + id, Canal: canal, Estado: estado, Medio: dominio.MedioTransferencia,
		ReferenciaExterna: "OC-2026-0457", VenceEn: &vence, CreadaEn: creada, ActualizadaEn: creada,
		Items: []dominio.Item{{VarianteID: "var-remera-basica-blanca-m", Cantidad: 3, Precio: 1500000}}, Total: 4500000,
	}
}

func cargarContrato(t *testing.T) routers.Router {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromFile(contratoPublicado)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("contrato inválido: %v", err)
	}
	if doc.Info.Version != "1.3.0" {
		t.Fatalf("versión del contrato %s, se esperaba 1.3.0", doc.Info.Version)
	}
	doc.Servers = nil
	router, err := legacy.NewRouter(doc)
	if err != nil {
		t.Fatal(err)
	}
	return router
}

func validarContraContrato(t *testing.T, router routers.Router, pedido *http.Request, w *httptest.ResponseRecorder) {
	t.Helper()
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

func consultar(h http.Handler, id string) (*http.Request, *httptest.ResponseRecorder) {
	pedido := httptest.NewRequest(http.MethodGet, "/v1/pedidos/"+id, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, pedido)
	return pedido, w
}

// ---------- GET /v1/pedidos/{pedidoId} ----------

func TestConsultarPedidoCumpleElContrato(t *testing.T) {
	router := cargarContrato(t)
	casos := []struct {
		interno  dominio.Estado
		contrato string
	}{
		{dominio.EstadoPendiente, "PENDIENTE_DE_PAGO"},
		{dominio.EstadoPagada, "PAGADO"},
		{dominio.EstadoEnCorreo, "PAGADO"},
		{dominio.EstadoEntregada, "PAGADO"},
		{dominio.EstadoVencida, "VENCIDO"},
		{dominio.EstadoAVerificar, "A_VERIFICAR"},
	}
	for _, c := range casos {
		t.Run(string(c.interno), func(t *testing.T) {
			id := "7d9f2c1e-4b3a-4e8f-9c2d-1a0b3c4d5e6f"
			repo := &repoFalso{ventas: map[string]dominio.Venta{id: ventaDe(id, dominio.CanalGrupoExterno, c.interno)}}
			pedido, w := consultar(armarRouter(repo), id)
			if w.Code != http.StatusOK {
				t.Fatalf("estado %d: %s", w.Code, w.Body)
			}
			validarContraContrato(t, router, pedido, w)
			var p pedidoJSON
			_ = json.Unmarshal(w.Body.Bytes(), &p)
			if p.Estado != c.contrato || p.PedidoID != id || p.Total != 45000 {
				t.Errorf("pedido %+v, se esperaba estado %s", p, c.contrato)
			}
		})
	}
}

// Una venta que no es de un grupo externo responde exactamente igual que una inexistente.
func TestConsultarPedidoDeOtroCanalEsInexistente(t *testing.T) {
	router := cargarContrato(t)
	online := "0a1b2c3d-0000-4000-8000-000000000001"
	local := "0a1b2c3d-0000-4000-8000-000000000002"
	repo := &repoFalso{ventas: map[string]dominio.Venta{
		online: ventaDe(online, dominio.CanalOnline, dominio.EstadoPagada),
		local:  ventaDe(local, dominio.CanalLocal, dominio.EstadoRegistrada),
	}}
	h := armarRouter(repo)

	pedido, inexistente := consultar(h, "0a1b2c3d-0000-4000-8000-0000000000ff")
	if inexistente.Code != http.StatusNotFound || !strings.Contains(inexistente.Body.String(), `"codigo":"PEDIDO_INEXISTENTE"`) {
		t.Fatalf("inexistente: %d %s", inexistente.Code, inexistente.Body)
	}
	validarContraContrato(t, router, pedido, inexistente)

	for _, id := range []string{online, local} {
		_, w := consultar(h, id)
		if w.Code != http.StatusNotFound || w.Body.String() != inexistente.Body.String() {
			t.Errorf("venta %s: %d %s; tiene que ser igual a una inexistente", id, w.Code, w.Body)
		}
	}
}

func TestConsultarPedidoConLaBaseCaida(t *testing.T) {
	router := cargarContrato(t)
	pedido, w := consultar(armarRouter(&repoFalso{err: context.DeadlineExceeded}), "7d9f2c1e-4b3a-4e8f-9c2d-1a0b3c4d5e6f")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("estado %d: %s", w.Code, w.Body)
	}
	validarContraContrato(t, router, pedido, w)
}

func TestConsultarPedidoSeApagaConLaCapacidad(t *testing.T) {
	_, w := consultar(armarRouter(&repoFalso{ventas: map[string]dominio.Venta{}}, SinCapacidadPublica()), "x")
	if w.Code != http.StatusNotFound {
		t.Errorf("apagada: código %d", w.Code)
	}
}

// ---------- POST /v1/pedidos no cambió ----------

// El alta responde como antes: 201, PENDIENTE_DE_PAGO, y la repetición devuelve el mismo cuerpo aunque el
// pedido ya esté pagado (lo promete la idempotencia; el estado vigente sale de la consulta).
func TestCrearPedidoSigueIgual(t *testing.T) {
	router := cargarContrato(t)
	repo := &repoFalso{ventas: map[string]dominio.Venta{}}
	h := armarRouter(repo)
	cuerpo := `{"referenciaExterna":"OC-2026-0457","items":[{"varianteId":"var-remera-basica-blanca-m","cantidad":3}]}`
	crear := func() (*http.Request, *httptest.ResponseRecorder) {
		pedido := httptest.NewRequest(http.MethodPost, "/v1/pedidos", strings.NewReader(cuerpo))
		pedido.Header.Set("Content-Type", "application/json")
		pedido.Header.Set("Idempotency-Key", "6f1c2b8e-3d4a-4e5f-9a7b-1c2d3e4f5a6b")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, pedido)
		return pedido, w
	}

	pedido, primera := crear()
	if primera.Code != http.StatusCreated {
		t.Fatalf("estado %d: %s", primera.Code, primera.Body)
	}
	pedido.Body = nopCloser{bytes.NewReader([]byte(cuerpo))}
	validarContraContrato(t, router, pedido, primera)
	var p pedidoJSON
	_ = json.Unmarshal(primera.Body.Bytes(), &p)
	if p.Estado != "PENDIENTE_DE_PAGO" || p.Total != 45000 || p.Pago.Medio != "TRANSFERENCIA" || p.Pago.DatosTransferencia.Titular != "Cielo Stellantis" {
		t.Fatalf("respuesta del alta: %+v", p)
	}

	v := repo.ventas[p.PedidoID]
	v.Estado = dominio.EstadoPagada
	repo.ventas[p.PedidoID] = v
	if _, segunda := crear(); segunda.Code != http.StatusCreated || segunda.Body.String() != primera.Body.String() {
		t.Errorf("la repetición cambió: %d %s", segunda.Code, segunda.Body)
	}
	if _, w := consultar(h, p.PedidoID); !strings.Contains(w.Body.String(), `"estado":"PAGADO"`) {
		t.Errorf("la consulta tiene que dar el estado vigente: %s", w.Body)
	}
}
