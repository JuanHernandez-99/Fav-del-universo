package observabilidad

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// grabarSpans deja un proveedor que guarda los spans en memoria, para revisarlos en el test.
func grabarSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	grabador := tracetest.NewSpanRecorder()
	anterior := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(grabador)))
	t.Cleanup(func() { otel.SetTracerProvider(anterior) })
	return grabador
}

func consultaDeRemeras(t *testing.T, monitor *event.CommandMonitor, ctx context.Context, pedido int64) {
	t.Helper()
	comando, err := bson.Marshal(bson.D{{Key: "find", Value: "productos"}, {Key: "filter", Value: bson.D{{Key: "nombre", Value: "Remera básica"}}}})
	if err != nil {
		t.Fatal(err)
	}
	monitor.Started(ctx, &event.CommandStartedEvent{Command: comando, DatabaseName: "catalogo", CommandName: "find", RequestID: pedido, ConnectionID: "c1"})
	monitor.Succeeded(ctx, &event.CommandSucceededEvent{CommandFinishedEvent: event.CommandFinishedEvent{CommandName: "find", RequestID: pedido, ConnectionID: "c1"}})
}

func TestMonitorMongoTrazaSoloDentroDeUnaOperacion(t *testing.T) {
	grabador := grabarSpans(t)
	monitor := MonitorMongo()

	// Sin traza (como el proceso que avisa cambios cada segundo): no se registra nada.
	consultaDeRemeras(t, monitor, context.Background(), 1)
	if n := len(grabador.Ended()); n != 0 {
		t.Fatalf("sin traza no debería haber spans, hay %d", n)
	}

	// Dentro de una operación: un span con el comando y la colección, hijo de la operación.
	ctx, fin := Operacion(context.Background(), "indexar producto")
	consultaDeRemeras(t, monitor, ctx, 2)
	fin(nil)

	spans := grabador.Ended()
	if len(spans) != 2 {
		t.Fatalf("esperaba 2 spans (consulta y operación), hay %d", len(spans))
	}
	consulta := spans[0]
	if consulta.Name() != "find productos" {
		t.Errorf("nombre del span = %q, esperaba %q", consulta.Name(), "find productos")
	}
	if consulta.Parent().SpanID() != spans[1].SpanContext().SpanID() {
		t.Error("la consulta debería colgar de la operación")
	}
	for _, a := range consulta.Attributes() {
		if a.Value.Emit() == "Remera básica" {
			t.Error("el span no debe llevar el filtro de la consulta")
		}
	}
}

func TestMiddlewareRespetaElCorrelationID(t *testing.T) {
	grabador := grabarSpans(t)
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
		t.Errorf("el handler recibió el correlation id %q", recibido)
	}
	if got := respuesta.Header().Get(HeaderCorrelacion); got != "compra-123" {
		t.Errorf("la respuesta devolvió %q", got)
	}
	spans := grabador.Ended()
	if len(spans) != 1 || spans[0].Name() != "GET /v1/tienda/productos/{id}" {
		t.Fatalf("esperaba un span con la ruta registrada, no la URL con el id: %v", spans)
	}
}

func TestMiddlewareNoTrazaLasSondas(t *testing.T) {
	grabador := grabarSpans(t)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(http.ResponseWriter, *http.Request) {})
	Middleware(mux).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if n := len(grabador.Ended()); n != 0 {
		t.Fatalf("la sonda abrió %d spans", n)
	}
}
