package claves

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func rutas(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "jwt_private.pem"), filepath.Join(dir, "jwt_public.pem")
}

func publicaDe(t *testing.T, ruta string) *rsa.PublicKey {
	t.Helper()
	contenido, err := os.ReadFile(ruta)
	if err != nil {
		t.Fatal(err)
	}
	bloque, _ := pem.Decode(contenido)
	clave, err := x509.ParsePKIXPublicKey(bloque.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return clave.(*rsa.PublicKey)
}

func TestGeneraUnParQueSeCorresponde(t *testing.T) {
	privada, publica := rutas(t)
	nueva, err := Asegurar(privada, publica)
	if err != nil || !nueva {
		t.Fatalf("%v %v", nueva, err)
	}
	clave, err := LeerPrivada(privada)
	if err != nil {
		t.Fatal(err)
	}
	if !clave.PublicKey.Equal(publicaDe(t, publica)) {
		t.Error("la pública tiene que corresponder a la privada")
	}
}

// Volver a levantar el sistema no puede cambiar la clave: invalidaría las sesiones iniciadas.
func TestUnSegundoArranqueConservaLaClave(t *testing.T) {
	privada, publica := rutas(t)
	if _, err := Asegurar(privada, publica); err != nil {
		t.Fatal(err)
	}
	antes, _ := os.ReadFile(privada)
	nueva, err := Asegurar(privada, publica)
	if err != nil || nueva {
		t.Fatalf("%v %v", nueva, err)
	}
	despues, _ := os.ReadFile(privada)
	if string(antes) != string(despues) {
		t.Error("cambió la clave privada")
	}
}

func TestReconstruyeLaPublicaSiFalta(t *testing.T) {
	privada, publica := rutas(t)
	if _, err := Asegurar(privada, publica); err != nil {
		t.Fatal(err)
	}
	original := publicaDe(t, publica)
	if err := os.Remove(publica); err != nil {
		t.Fatal(err)
	}
	if _, err := Asegurar(privada, publica); err != nil {
		t.Fatal(err)
	}
	if !original.Equal(publicaDe(t, publica)) {
		t.Error("tiene que ser la misma clave")
	}
}

func TestLeerUnArchivoQueNoEsClave(t *testing.T) {
	privada, _ := rutas(t)
	if _, err := LeerPrivada(privada); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("sin archivo: %v", err)
	}
	if err := os.WriteFile(privada, []byte("esto no es una clave"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LeerPrivada(privada); err == nil {
		t.Error("tiene que fallar")
	}
}
