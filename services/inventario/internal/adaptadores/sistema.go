// Package adaptadores tiene implementaciones simples de puertos que no necesitan
// paquete propio.
package adaptadores

import (
	"crypto/rand"
	"fmt"
	"time"
)

// RelojSistema da la hora real en UTC.
type RelojSistema struct{}

func (RelojSistema) Ahora() time.Time { return time.Now().UTC() }

// GeneradorUUID crea UUID versión 4.
type GeneradorUUID struct{}

func (GeneradorUUID) Nuevo() string { return NuevoUUID() }

// NuevoUUID crea un UUID versión 4 (aleatorio).
func NuevoUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand no falla en la práctica
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
