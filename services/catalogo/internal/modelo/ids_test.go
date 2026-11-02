package modelo

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Los resultados esperados salen de `normalizar` de tp-mongo/datos/generar.py.
func TestNormalizarIgualQuePython(t *testing.T) {
	casos := map[string]string{
		"Tapado de paño":          "tapado-de-pano",
		"Remera cuello V":         "remera-cuello-v",
		"Azul marino":             "azul-marino",
		"Bordó":                   "bordo",
		"  Remera   básica!! ":    "remera-basica",
		"Ñandú":                   "nandu",
		"XXL":                     "xxl",
		"38":                      "38",
		"Riñonera de cuero":       "rinonera-de-cuero",
		"Jean wide leg tiro alto": "jean-wide-leg-tiro-alto",
	}
	for entrada, esperado := range casos {
		if obtenido := Normalizar(entrada); obtenido != esperado {
			t.Errorf("Normalizar(%q) = %q, se esperaba %q", entrada, obtenido, esperado)
		}
	}
}

func TestIDs(t *testing.T) {
	if id := IDProducto("Remera lisa", 1); id != "prod-remera-lisa" {
		t.Errorf("IDProducto intento 1 = %q", id)
	}
	if id := IDProducto("Remera lisa", 2); id != "prod-remera-lisa-2" {
		t.Errorf("IDProducto intento 2 = %q", id)
	}
	id, err := IDVariante("prod-remera-basica", "Blanco", "M")
	if err != nil || id != "var-remera-basica-blanco-m" {
		t.Errorf("IDVariante = %q, %v", id, err)
	}
	if _, err := IDVariante("prod-"+strings.Repeat("a", 95), "Azul marino", "XXL"); err == nil {
		t.Error("se esperaba error por superar los 100 caracteres")
	}
}

// TestIDsDeLosDatosGenerados recalcula en Go los 881 ids de variante que generó Python y los
// compara. Si alguna vez difieren, un producto creado por la API no seguiría la misma regla.
func TestIDsDeLosDatosGenerados(t *testing.T) {
	contenido, err := os.ReadFile("../../../../tp-mongo/datos/productos.json")
	if err != nil {
		t.Skip("no está tp-mongo/datos/productos.json:", err)
	}
	var productos []struct {
		ID        string `json:"_id"`
		Nombre    string `json:"nombre"`
		Variantes []struct {
			ID, Talle, Color string
		} `json:"variantes"`
	}
	if err := json.Unmarshal(contenido, &productos); err != nil {
		t.Fatal(err)
	}
	for _, p := range productos {
		if base := IDProducto(p.Nombre, 1); base != p.ID {
			t.Errorf("producto %q: Go da %q, Python dio %q", p.Nombre, base, p.ID)
		}
		for _, v := range p.Variantes {
			if id, _ := IDVariante(p.ID, v.Color, v.Talle); id != v.ID {
				t.Errorf("variante: Go da %q, Python dio %q", id, v.ID)
			}
		}
	}
}
