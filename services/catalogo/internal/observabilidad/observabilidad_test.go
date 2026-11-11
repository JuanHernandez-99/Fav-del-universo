package observabilidad

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// grabarLogs deja slog escribiendo en un buffer, para revisar las líneas en el test.
func grabarLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var salida bytes.Buffer
	anterior := slog.Default()
	slog.SetDefault(slog.New(manejadorConContexto{slog.NewJSONHandler(&salida, nil)}))
	t.Cleanup(func() { slog.SetDefault(anterior) })
	return &salida
}

func TestMiddlewareRespetaElTrackIDYLogueaLaRutaRegistrada(t *testing.T) {
	salida := grabarLogs(t)
	mux := http.NewServeMux()
	var recibido string
	mux.HandleFunc("GET /v1/tienda/productos/{id}", func(w http.ResponseWriter, r *http.Request) {
		recibido = CorrelationID(r.Context())
	})

	pedido := httptest.NewRequest(http.MethodGet, "/v1/tienda/productos/prd-remera-basica", nil)
	pedido.Header.Set(HeaderCorrelacion, "compra-123")
	respuesta := httptest.NewRecorder()
	Middleware(mux).ServeHTTP(respuesta, pedido)

	if recibido != "compra-123" {
		t.Errorf("el handler recibió el track id %q", recibido)
	}
	if got := respuesta.Header().Get(HeaderCorrelacion); got != "compra-123" {
		t.Errorf("la respuesta devolvió %q", got)
	}
	linea := salida.String()
	if !strings.Contains(linea, `"correlation_id":"compra-123"`) || !strings.Contains(linea, `"ruta":"/v1/tienda/productos/{id}"`) {
		t.Errorf("la línea tiene que llevar el track id y la ruta registrada, no la URL con el id: %s", linea)
	}
}

func TestMiddlewareAceptaXTrackIDYGeneraUnoSiNoViene(t *testing.T) {
	mux := http.NewServeMux()
	var recibido string
	mux.HandleFunc("GET /v1/tienda/productos", func(w http.ResponseWriter, r *http.Request) { recibido = CorrelationID(r.Context()) })

	pedido := httptest.NewRequest(http.MethodGet, "/v1/tienda/productos", nil)
	pedido.Header.Set(HeaderTrackID, "track-de-la-catedra")
	Middleware(mux).ServeHTTP(httptest.NewRecorder(), pedido)
	if recibido != "track-de-la-catedra" {
		t.Errorf("con X-Track-ID el handler recibió %q", recibido)
	}

	respuesta := httptest.NewRecorder()
	Middleware(mux).ServeHTTP(respuesta, httptest.NewRequest(http.MethodGet, "/v1/tienda/productos", nil))
	if recibido == "" || respuesta.Header().Get(HeaderCorrelacion) != recibido {
		t.Errorf("sin encabezado tiene que generar uno y devolverlo: recibido %q, respuesta %q", recibido, respuesta.Header().Get(HeaderCorrelacion))
	}
}

func TestMiddlewareNoLogueaLasSondas(t *testing.T) {
	salida := grabarLogs(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(http.ResponseWriter, *http.Request) {})
	Middleware(mux).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if salida.Len() != 0 {
		t.Fatalf("la sonda escribió en el log: %s", salida.String())
	}
}

func TestTransportePropagaElTrackID(t *testing.T) {
	var recibido string
	servidor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recibido = r.Header.Get(HeaderCorrelacion)
	}))
	defer servidor.Close()

	cliente := &http.Client{Transport: Transporte(http.DefaultTransport)}
	pedido, _ := http.NewRequestWithContext(ConCorrelationID(context.Background(), "alta-remera-7"), http.MethodGet, servidor.URL, nil)
	respuesta, err := cliente.Do(pedido)
	if err != nil {
		t.Fatal(err)
	}
	respuesta.Body.Close()
	if recibido != "alta-remera-7" {
		t.Errorf("Elasticsearch recibió el track id %q", recibido)
	}
}
