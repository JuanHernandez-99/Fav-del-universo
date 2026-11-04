package mercadopago

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/dominio"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/puertos"
)

func cliente(t *testing.T, manejador http.HandlerFunc) *Cliente {
	t.Helper()
	s := httptest.NewServer(manejador)
	t.Cleanup(s.Close)
	return NuevoCliente(Config{URLBase: s.URL, AccessToken: "token", SecretoAviso: "secreto", URLAviso: "http://aviso", Timeout: time.Second})
}

func TestVerificarFirma(t *testing.T) {
	c := NuevoCliente(Config{SecretoAviso: "secreto", Timeout: time.Second})
	valida := "ts=1700000000,v1=" + FirmarAviso("secreto", "123", "pedido-1", "1700000000")
	if !c.VerificarFirma(valida, "pedido-1", "123") {
		t.Error("la firma válida debe aceptarse")
	}
	casos := map[string][3]string{
		"otro pago":    {valida, "pedido-1", "999"},
		"otro pedido":  {valida, "pedido-2", "123"},
		"otro secreto": {"ts=1700000000,v1=" + FirmarAviso("otro", "123", "pedido-1", "1700000000"), "pedido-1", "123"},
		"sin firma":    {"", "pedido-1", "123"},
		"incompleta":   {"ts=1700000000", "pedido-1", "123"},
	}
	for nombre, c2 := range casos {
		if c.VerificarFirma(c2[0], c2[1], c2[2]) {
			t.Errorf("%s: debía rechazarse", nombre)
		}
	}
}

func TestConsultarPagoTraduceLosEstados(t *testing.T) {
	casos := map[string]puertos.EstadoPago{"approved": puertos.PagoAprobado, "rejected": puertos.PagoRechazado, "cancelled": puertos.PagoRechazado, "in_process": puertos.PagoPendiente}
	for estado, esperado := range casos {
		c := cliente(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer token" {
				t.Error("falta el token")
			}
			_, _ = w.Write([]byte(`{"id":123,"status":"` + estado + `","external_reference":"venta-1"}`))
		})
		p, err := c.ConsultarPago(context.Background(), "123")
		if err != nil || p.Estado != esperado || p.ID != "123" || p.VentaID != "venta-1" {
			t.Errorf("%s: %+v %v", estado, p, err)
		}
	}
}

// ADR-005: tras 5 fallas seguidas el circuito se abre y deja de llamar.
func TestCircuitoSeAbreTrasCincoFallas(t *testing.T) {
	llamadas := 0
	c := cliente(t, func(w http.ResponseWriter, _ *http.Request) {
		llamadas++
		w.WriteHeader(http.StatusInternalServerError)
	})
	v := dominio.Venta{ID: "v1"}
	for range 8 {
		if _, err := c.CrearPago(context.Background(), v); !errors.Is(err, dominio.ErrPagoNoDisponible) {
			t.Fatalf("error: %v", err)
		}
	}
	if llamadas != 5 {
		t.Errorf("llamadas: %d", llamadas)
	}
}
