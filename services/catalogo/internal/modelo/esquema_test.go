package modelo

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// El validador $jsonSchema de MongoDB (db/scripts/01_crear_colecciones_e_indices.js) repite los
// sistemas de talles de SistemasTalles. Este test falla si las dos listas se separan.
func TestSistemasDeTallesIgualQueElEsquemaDeMongo(t *testing.T) {
	script, err := os.ReadFile("../../db/scripts/01_crear_colecciones_e_indices.js")
	if err != nil {
		t.Fatalf("no se pudo leer el script de Mongo: %v", err)
	}

	bloque := regexp.MustCompile(`(?s)const TALLES = \{(.*?)\};`).FindSubmatch(script)
	if bloque == nil {
		t.Fatal("no se encontró `const TALLES = {...}` en el script")
	}
	desdeElScript := map[string][]string{}
	for _, m := range regexp.MustCompile(`(\w+):\s*\[([^\]]*)\]`).FindAllStringSubmatch(string(bloque[1]), -1) {
		var talles []string
		for _, talle := range strings.Split(m[2], ",") {
			talles = append(talles, strings.Trim(strings.TrimSpace(talle), `"`))
		}
		desdeElScript[m[1]] = talles
	}

	if len(desdeElScript) != len(SistemasTalles) {
		t.Errorf("el script tiene %d sistemas de talles y Go %d", len(desdeElScript), len(SistemasTalles))
	}
	for sistema, talles := range SistemasTalles {
		if !slices.Equal(desdeElScript[sistema], talles) {
			t.Errorf("%s: el script tiene %v y Go %v", sistema, desdeElScript[sistema], talles)
		}
	}
}
