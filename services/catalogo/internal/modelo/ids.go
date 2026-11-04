package modelo

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// LargoMaximoID es el límite de Inventario para el id de variante (VARCHAR(100)).
const LargoMaximoID = 100

var noAlfanumerico = regexp.MustCompile(`[^a-z0-9]+`)

// Normalizar deja minúsculas sin acentos (la ñ queda n) y cambia todo lo que no sea letra o número
// por '-' (DISENO.md 5.5 y sección 14, punto 11). Tiene que dar lo mismo que `normalizar` de
// db/datos/generar.py: NFD separa la letra de su acento y después se quitan los acentos.
func Normalizar(texto string) string {
	sinAcentos, _, _ := transform.String(
		transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn))), strings.ToLower(texto))
	return strings.Trim(noAlfanumerico.ReplaceAllString(sinAcentos, "-"), "-")
}

// IDProducto arma el id a partir del nombre. intento 1 da el id base; 2, 3… agregan un sufijo,
// para cuando el base ya está en uso.
func IDProducto(nombre string, intento int) string {
	id := "prod-" + Normalizar(nombre)
	if intento > 1 {
		id = fmt.Sprintf("%s-%d", id, intento)
	}
	return id
}

// IDPromocion arma el id como IDProducto: promo-verano-en-remeras, con sufijo si ya está en uso.
func IDPromocion(nombre string, intento int) string {
	id := "promo-" + Normalizar(nombre)
	if intento > 1 {
		id = fmt.Sprintf("%s-%d", id, intento)
	}
	return id
}

// IDVariante arma `var-<producto>-<color>-<talle>` (D12). Una vez creado no cambia nunca.
func IDVariante(productoID, color, talle string) (string, error) {
	id := "var-" + strings.TrimPrefix(productoID, "prod-") + "-" + Normalizar(color) + "-" + Normalizar(talle)
	if len(id) > LargoMaximoID {
		return "", fmt.Errorf("el id de variante %q supera los %d caracteres", id, LargoMaximoID)
	}
	return id, nil
}
