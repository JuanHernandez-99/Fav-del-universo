package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/dominio"
)

// Los errores del dominio se traducen a los códigos que publica el contrato catalogo-pedidos-v1.
func TestErroresSiguenElContrato(t *testing.T) {
	casos := []struct {
		error  error
		status int
		codigo string
	}{
		{&dominio.ErrorValidacion{Campos: []dominio.CampoInvalido{{Campo: "items", Mensaje: "x"}}}, 400, "PEDIDO_INVALIDO"},
		{&dominio.ErrorVarianteInexistente{Variantes: []string{"a"}}, 400, "VARIANTE_INEXISTENTE"},
		{&dominio.ErrorStockInsuficiente{Faltantes: []dominio.Faltante{{VarianteID: "a", Solicitada: 1}}}, 409, "STOCK_INSUFICIENTE"},
		{dominio.ErrClaveReutilizada, 409, "CLAVE_IDEMPOTENCIA_REUTILIZADA"},
		{dominio.ErrInventarioNoDisponible, 503, "NO_DISPONIBLE"},
		{dominio.ErrCatalogoNoDisponible, 503, "NO_DISPONIBLE"},
		{dominio.ErrPagoNoDisponible, 503, "PAGO_NO_DISPONIBLE"},
		{dominio.ErrVentaNoEncontrada, 404, "VENTA_NO_ENCONTRADA"},
		{dominio.ErrTransicionInvalida, 409, "TRANSICION_INVALIDA"},
		{dominio.ErrFirmaInvalida, 401, "NO_AUTENTICADO"},
		{dominio.ErrNoAutenticado, 401, "NO_AUTENTICADO"},
		{dominio.ErrNoAutorizado, 403, "NO_AUTORIZADO"},
		{errors.New("detalle interno"), 500, "ERROR_INTERNO"},
	}
	for _, c := range casos {
		w := httptest.NewRecorder()
		escribirError(w, httptest.NewRequest(http.MethodGet, "/", nil), c.error)
		var p problema
		if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		if w.Code != c.status || p.Codigo != c.codigo || p.Type == "" || p.Title == "" {
			t.Errorf("%v: %d %+v", c.error, w.Code, p)
		}
		if w.Header().Get("Content-Type") != "application/problem+json" {
			t.Errorf("%v: content-type %s", c.error, w.Header().Get("Content-Type"))
		}
		if c.status == 500 && p.Detail != "" {
			t.Error("un error inesperado no debe mostrar su detalle")
		}
		if c.status == 503 && w.Header().Get("Retry-After") == "" {
			t.Errorf("%v: falta Retry-After (contrato)", c.error)
		}
	}
}

// HU-19: sin rol de administrador no se entra, aunque se llegue al servicio.
func TestRutasDeAdminExigenElRol(t *testing.T) {
	llamado := false
	h := soloAdmin(func(http.ResponseWriter, *http.Request) { llamado = true })

	w := httptest.NewRecorder()
	h(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != 403 || llamado {
		t.Errorf("sin rol: %d, llamado %v", w.Code, llamado)
	}

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-User-Role", "comprador")
	w = httptest.NewRecorder()
	h(w, r)
	if w.Code != 403 || llamado {
		t.Errorf("comprador: %d, llamado %v", w.Code, llamado)
	}

	r.Header.Set("X-User-Role", rolAdministrador)
	h(httptest.NewRecorder(), r)
	if !llamado {
		t.Error("el administrador debe entrar")
	}
}

// ADR-008: los pedidos externos se pueden apagar sin tocar la tienda.
func TestPedidosExternosSePuedenApagar(t *testing.T) {
	pedir := func(h http.Handler, metodo, ruta string) int {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(metodo, ruta, strings.NewReader("{")))
		return w.Code
	}
	apagada := NuevoRouter(nil, DatosTransferencia{}, SinCapacidadPublica())
	if c := pedir(apagada, http.MethodPost, "/v1/pedidos"); c != http.StatusNotFound {
		t.Errorf("apagada: código %d", c)
	}
	if c := pedir(apagada, http.MethodPost, "/v1/compras"); c != http.StatusBadRequest {
		t.Errorf("la compra online tiene que seguir: código %d", c)
	}
	// Prendida, la ruta existe: un cuerpo roto da 400, no 404.
	if c := pedir(NuevoRouter(nil, DatosTransferencia{}), http.MethodPost, "/v1/pedidos"); c != http.StatusBadRequest {
		t.Errorf("prendida: código %d", c)
	}
}

func TestLeerLineas(t *testing.T) {
	l, errores := leerLineas("var-remera:2, var-jean:1")
	if len(errores) != 0 || len(l) != 2 || l[0].Cantidad != 2 || l[1].VarianteID != "var-jean" {
		t.Errorf("%+v %+v", l, errores)
	}
	for _, malo := range []string{"", "var-remera", "var-remera:0", ":1", "var-remera:x"} {
		if _, errores := leerLineas(malo); len(errores) == 0 {
			t.Errorf("%q debía rechazarse", malo)
		}
	}
}

func TestPesosYCentavos(t *testing.T) {
	if centavos(15999.90) != 1_599_990 || pesos(1_599_990) != 15999.90 {
		t.Errorf("%d %v", centavos(15999.90), pesos(1_599_990))
	}
}
