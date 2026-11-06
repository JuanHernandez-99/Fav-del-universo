package dominio

import (
	"errors"
	"strings"
	"testing"
)

func TestAjusteValido(t *testing.T) {
	a, err := NuevoAjuste("clave-ajuste-1", "var-remera-blanca-m", CanalFisico, -2, MotivoPerdida, "  se perdieron en el depósito  ", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if a.Cantidad != -2 || a.Nota != "se perdieron en el depósito" {
		t.Errorf("ajuste: %+v", a)
	}
}

// HU-08.2: sin motivo se rechaza. También la cantidad 0, el canal desconocido y una nota demasiado larga.
func TestAjusteInvalido(t *testing.T) {
	casos := map[string]struct {
		canal    Canal
		cantidad int
		motivo   Motivo
		nota     string
		campo    string
	}{
		"sin motivo":       {CanalOnline, 1, "", "", "motivo"},
		"motivo inventado": {CanalOnline, 1, "ROBO", "", "motivo"},
		"cantidad 0":       {CanalOnline, 0, MotivoFalla, "", "cantidad"},
		"canal inválido":   {"DEPOSITO", 1, MotivoFalla, "", "canal"},
		"nota larga":       {CanalFisico, 1, MotivoCorreccionConteo, strings.Repeat("ñ", 201), "nota"},
	}
	for nombre, c := range casos {
		_, err := NuevoAjuste("clave-ajuste-1", "var-remera-blanca-m", c.canal, c.cantidad, c.motivo, c.nota, "admin")
		var ev *ErrorValidacion
		if !errors.As(err, &ev) || len(ev.Campos) != 1 || ev.Campos[0].Campo != c.campo {
			t.Errorf("%s: %v", nombre, err)
		}
	}
}

// Una nota de 200 letras con tilde entra: se cuentan caracteres, no bytes.
func TestAjusteNotaCuentaCaracteres(t *testing.T) {
	if _, err := NuevoAjuste("clave-ajuste-1", "v", CanalFisico, 1, MotivoFalla, strings.Repeat("ñ", 200), "admin"); err != nil {
		t.Error(err)
	}
}

// La misma clave con otro motivo es otro pedido: la huella cambia y el reintento se rechaza.
func TestAjusteHuellaIncluyeElMotivo(t *testing.T) {
	a, _ := NuevoAjuste("clave-ajuste-1", "v", CanalFisico, -1, MotivoPerdida, "", "admin")
	b, _ := NuevoAjuste("clave-ajuste-1", "v", CanalFisico, -1, MotivoFalla, "", "admin")
	if a.Huella() == b.Huella() {
		t.Error("misma huella con distinto motivo")
	}
}
