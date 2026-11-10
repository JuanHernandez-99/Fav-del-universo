// Package claves genera y lee el par de claves RS256 del token (ADR-015).
//
// La privada y la pública van en carpetas distintas, porque en Docker son volúmenes distintos: el gateway
// solo monta el de la pública, así no puede emitir tokens.
package claves

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
)

const bits = 2048

// Asegurar crea el par si no existe. Si ya existe la privada no la cambia, para no invalidar las sesiones
// iniciadas, y vuelve a escribir la pública a partir de ella. Devuelve true si generó una clave nueva.
func Asegurar(rutaPrivada, rutaPublica string) (bool, error) {
	clave, err := LeerPrivada(rutaPrivada)
	nueva := false
	switch {
	case errors.Is(err, os.ErrNotExist):
		if clave, err = rsa.GenerateKey(rand.Reader, bits); err != nil {
			return false, err
		}
		contenido := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: mustPKCS8(clave)})
		if err := os.WriteFile(rutaPrivada, contenido, 0o600); err != nil {
			return false, fmt.Errorf("escribiendo la clave privada: %w", err)
		}
		nueva = true
	case err != nil:
		return false, err
	}

	publica, err := x509.MarshalPKIXPublicKey(&clave.PublicKey)
	if err != nil {
		return false, err
	}
	contenido := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publica})
	if err := os.WriteFile(rutaPublica, contenido, 0o644); err != nil {
		return false, fmt.Errorf("escribiendo la clave pública: %w", err)
	}
	return nueva, nil
}

func mustPKCS8(clave *rsa.PrivateKey) []byte {
	contenido, err := x509.MarshalPKCS8PrivateKey(clave)
	if err != nil {
		panic(err) // solo falla con un tipo de clave no soportado, y siempre es RSA
	}
	return contenido
}

// LeerPrivada devuelve un error que cumple os.ErrNotExist si el archivo no está.
func LeerPrivada(ruta string) (*rsa.PrivateKey, error) {
	contenido, err := os.ReadFile(ruta)
	if err != nil {
		return nil, err
	}
	bloque, _ := pem.Decode(contenido)
	if bloque == nil {
		return nil, fmt.Errorf("%s no tiene una clave en formato PEM", ruta)
	}
	clave, err := x509.ParsePKCS8PrivateKey(bloque.Bytes)
	if err != nil {
		return nil, fmt.Errorf("leyendo %s: %w", ruta, err)
	}
	privada, ok := clave.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%s no es una clave RSA", ruta)
	}
	return privada, nil
}
