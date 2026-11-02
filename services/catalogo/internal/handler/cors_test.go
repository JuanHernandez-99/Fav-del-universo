package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConCORS(t *testing.T) {
	final := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	t.Run("apagado no agrega cabeceras", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/v1/admin/productos", nil)
		r.Header.Set("Origin", "http://localhost:8086")
		w := httptest.NewRecorder()
		ConCORS(final, "").ServeHTTP(w, r)
		if w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("con CORS_ORIGENES vacío no debe haber cabeceras CORS: %v", w.Header())
		}
	})

	t.Run("origen permitido", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/v1/admin/productos", nil)
		r.Header.Set("Origin", "http://localhost:8086")
		w := httptest.NewRecorder()
		ConCORS(final, "http://otro, http://localhost:8086").ServeHTTP(w, r)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:8086" {
			t.Errorf("Allow-Origin = %q", got)
		}
		if w.Code != http.StatusOK {
			t.Errorf("código %d; el pedido tenía que llegar al router", w.Code)
		}
	})

	t.Run("origen no permitido", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/v1/admin/productos", nil)
		r.Header.Set("Origin", "http://malo.example")
		w := httptest.NewRecorder()
		ConCORS(final, "http://localhost:8086").ServeHTTP(w, r)
		if w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("no debe permitir un origen que no está en la lista: %v", w.Header())
		}
	})

	t.Run("preflight", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodOptions, "/v1/admin/productos", nil)
		r.Header.Set("Origin", "http://localhost:8086")
		r.Header.Set("Access-Control-Request-Method", "POST")
		w := httptest.NewRecorder()
		llamo := false
		siguiente := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { llamo = true })
		ConCORS(siguiente, "http://localhost:8086").ServeHTTP(w, r)
		if w.Code != http.StatusNoContent || llamo {
			t.Errorf("el preflight se contesta ahí mismo con 204: código %d, llegó al router: %v", w.Code, llamo)
		}
		if w.Header().Get("Access-Control-Allow-Methods") == "" || w.Header().Get("Access-Control-Allow-Headers") == "" {
			t.Errorf("faltan métodos o cabeceras permitidas: %v", w.Header())
		}
	})
}
