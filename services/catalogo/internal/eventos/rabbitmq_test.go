package eventos

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/observabilidad"
)

const mensajeValido = `{"id":"m1","tipo":"ProductoCambiado","version":1,"datos":{"productoId":"remera-basica","version":3}}`

func TestDecideDondeVaElMensaje(t *testing.T) {
	transitorio := fmt.Errorf("%w: elasticsearch caído", ErrTransitorio)
	casos := []struct {
		nombre   string
		cuerpo   string
		intentos int
		err      error
		destino  string
	}{
		{"indexado", mensajeValido, 0, nil, ""},
		{"JSON inválido", `{`, 0, nil, ColaFallidos},
		{"sin producto", `{"id":"m1","datos":{}}`, 0, nil, ColaFallidos},
		{"falla del índice", mensajeValido, 0, errors.New("rechazado"), ColaReintento},
		{"reintentos agotados", mensajeValido, maxReintentos, errors.New("rechazado"), ColaFallidos},
		{"motor caído nunca va a fallidos", mensajeValido, 1000, transitorio, ColaReintento},
	}
	for _, caso := range casos {
		t.Run(caso.nombre, func(t *testing.T) {
			var producto string
			c := NuevoConsumidor("", func(_ context.Context, id string) error { producto = id; return caso.err }, 0)
			destino, _ := c.decidir(context.Background(), []byte(caso.cuerpo), "m1", caso.intentos)
			if destino != caso.destino {
				t.Fatalf("destino %q, se esperaba %q", destino, caso.destino)
			}
			if caso.cuerpo == mensajeValido && producto != "remera-basica" {
				t.Fatalf("producto: %q", producto)
			}
		})
	}
}

func TestMotorCaidoNoGastaReintentos(t *testing.T) {
	c := NuevoConsumidor("", func(context.Context, string) error { return ErrTransitorio }, 0)
	_, cabeceras := c.decidir(context.Background(), []byte(mensajeValido), "m1", 2)
	if intentosDe(cabeceras) != 2 {
		t.Fatalf("intentos: %d", intentosDe(cabeceras))
	}
}

// La indexación usa el track id que trae el evento: el del pedido que cambió el producto (ADR-020).
func TestLaIndexacionLlevaElTrackIDDelEvento(t *testing.T) {
	var recibido string
	c := NuevoConsumidor("", func(ctx context.Context, _ string) error {
		recibido = observabilidad.CorrelationID(ctx)
		return nil
	}, 0)
	cuerpo := `{"id":"m1","tipo":"ProductoCambiado","version":1,"correlationId":"alta-remera-7","datos":{"productoId":"remera-basica","version":3}}`
	c.decidir(context.Background(), []byte(cuerpo), "m1", 0)
	if recibido != "alta-remera-7" {
		t.Fatalf("track id en la indexación = %q, esperaba alta-remera-7", recibido)
	}
}
