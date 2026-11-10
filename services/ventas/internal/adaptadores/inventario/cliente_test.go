package inventario

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/dominio"
)

func servidor(t *testing.T, manejador http.HandlerFunc) *Cliente {
	t.Helper()
	s := httptest.NewServer(manejador)
	t.Cleanup(s.Close)
	c := NuevoCliente(s.URL)
	c.esperas = []time.Duration{0, time.Millisecond, time.Millisecond}
	return c
}

var lineas = []dominio.LineaPedido{{VarianteID: "var-remera", Cantidad: 1}}

// ADR-005: una falla transitoria se reintenta con la misma clave de idempotencia.
func TestReintentaAnteFallaTransitoriaConLaMismaClave(t *testing.T) {
	var claves []string
	c := servidor(t, func(w http.ResponseWriter, r *http.Request) {
		claves = append(claves, r.Header.Get("Idempotency-Key"))
		if len(claves) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"reservaId":"r1","estado":"ACTIVA","venceEn":"2026-11-04T12:20:00Z","creadaEn":"2026-11-04T12:00:00Z"}`))
	})
	r, err := c.Reservar(context.Background(), "venta-1234", "v1", lineas, 20*time.Minute)
	if err != nil || r.ID != "r1" || r.Estado != "ACTIVA" {
		t.Fatalf("%+v %v", r, err)
	}
	if len(claves) != 3 || claves[0] != claves[2] {
		t.Errorf("claves: %v", claves)
	}
}

func TestAgotadosLosReintentosInformaNoDisponible(t *testing.T) {
	llamadas := 0
	c := servidor(t, func(w http.ResponseWriter, _ *http.Request) {
		llamadas++
		w.WriteHeader(http.StatusBadGateway)
	})
	if err := c.Confirmar(context.Background(), "r1"); !errors.Is(err, dominio.ErrInventarioNoDisponible) {
		t.Errorf("error: %v", err)
	}
	if llamadas != 3 {
		t.Errorf("llamadas: %d", llamadas)
	}
}

// Un error de negocio es definitivo: no se reintenta y se traduce al dominio de Ventas.
func TestErroresDeNegocioNoSeReintentan(t *testing.T) {
	casos := map[string]struct {
		codigo int
		cuerpo string
		error  error
	}{
		"sin stock":   {409, `{"codigo":"STOCK_INSUFICIENTE","faltantes":[{"varianteId":"var-remera","solicitada":3,"disponible":1}]}`, dominio.ErrStockInsuficiente},
		"vencida":     {409, `{"codigo":"RESERVA_VENCIDA"}`, dominio.ErrReservaVencida},
		"liberada":    {409, `{"codigo":"RESERVA_LIBERADA"}`, dominio.ErrReservaLiberada},
		"confirmada":  {409, `{"codigo":"RESERVA_CONFIRMADA"}`, dominio.ErrReservaConfirmada},
		"no existe":   {404, `{"codigo":"RESERVA_NO_ENCONTRADA"}`, dominio.ErrReservaNoEncontrada},
		"inexistente": {404, `{"codigo":"VARIANTE_INEXISTENTE","variantes":["var-remera"]}`, dominio.ErrVarianteInexistente},
		"clave":       {409, `{"codigo":"CLAVE_IDEMPOTENCIA_REUTILIZADA"}`, dominio.ErrClaveReutilizada},
	}
	for nombre, caso := range casos {
		llamadas := 0
		c := servidor(t, func(w http.ResponseWriter, _ *http.Request) {
			llamadas++
			w.WriteHeader(caso.codigo)
			_, _ = w.Write([]byte(caso.cuerpo))
		})
		if err := c.Confirmar(context.Background(), "r1"); !errors.Is(err, caso.error) {
			t.Errorf("%s: %v", nombre, err)
		}
		if llamadas != 1 {
			t.Errorf("%s: %d llamadas", nombre, llamadas)
		}
	}
}

// El contrato público no informa cuánto stock hay: Ventas no debe conservar ese dato.
func TestFaltantesNoExponenElDisponible(t *testing.T) {
	c := servidor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"codigo":"STOCK_INSUFICIENTE","faltantes":[{"varianteId":"var-remera","solicitada":3,"disponible":1}]}`))
	})
	_, err := c.Reservar(context.Background(), "venta-1234", "v1", lineas, time.Minute)
	var stock *dominio.ErrorStockInsuficiente
	if !errors.As(err, &stock) || stock.Faltantes[0] != (dominio.Faltante{VarianteID: "var-remera", Solicitada: 3}) {
		t.Errorf("error: %+v", err)
	}
}
