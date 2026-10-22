package dominio

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

type Canal string

const (
	CanalOnline Canal = "ONLINE"
	CanalFisico Canal = "FISICO"
)

func (c Canal) Valido() bool { return c == CanalOnline || c == CanalFisico }

// Stock de una variante. Las reservas apartan unidades del stock online sin descontarlas
// hasta que se confirman. El físico puede quedar negativo por una venta del local (RN-06, RN-25);
// el online nunca.
type Stock struct {
	VarianteID    string
	Online        int
	Fisico        int
	Reservado     int
	ActualizadoEn time.Time
}

func (s Stock) DisponibleOnline() int { return s.Online - s.Reservado }

// AConfirmar marca la variante como "stock a corregir" (RN-25).
func (s Stock) AConfirmar() bool { return s.Fisico < 0 }

// Ingreso suma mercadería nueva a un canal (HU-06).
type Ingreso struct {
	ClaveIdempotencia string
	VarianteID        string
	Canal             Canal
	Cantidad          int
	Usuario           string
}

func NuevoIngreso(clave, varianteID string, canal Canal, cantidad int, usuario string) (Ingreso, error) {
	var ev ErrorValidacion
	validarClave(&ev, clave)
	validarVariante(&ev, varianteID)
	if !canal.Valido() {
		ev.agregar("canal", "debe ser ONLINE o FISICO")
	}
	if cantidad < 1 {
		ev.agregar("cantidad", "debe ser mayor a 0")
	}
	return Ingreso{clave, varianteID, canal, cantidad, usuario}, ev.comoError()
}

func (i Ingreso) Huella() string {
	return huella("ingreso", i.VarianteID, string(i.Canal), fmt.Sprint(i.Cantidad))
}

// Transferencia mueve unidades de un canal al otro en una sola transacción (HU-07, RN-10).
// Del online solo se pueden mover las unidades no reservadas.
type Transferencia struct {
	ClaveIdempotencia string
	VarianteID        string
	Origen            Canal
	Cantidad          int
	Usuario           string
}

func NuevaTransferencia(clave, varianteID string, origen Canal, cantidad int, usuario string) (Transferencia, error) {
	var ev ErrorValidacion
	validarClave(&ev, clave)
	validarVariante(&ev, varianteID)
	if !origen.Valido() {
		ev.agregar("origen", "debe ser ONLINE o FISICO")
	}
	if cantidad < 1 {
		ev.agregar("cantidad", "debe ser mayor a 0")
	}
	return Transferencia{clave, varianteID, origen, cantidad, usuario}, ev.comoError()
}

func (t Transferencia) Destino() Canal {
	if t.Origen == CanalOnline {
		return CanalFisico
	}
	return CanalOnline
}

func (t Transferencia) Huella() string {
	return huella("transferencia", t.VarianteID, string(t.Origen), fmt.Sprint(t.Cantidad))
}

func validarVariante(ev *ErrorValidacion, varianteID string) {
	if strings.TrimSpace(varianteID) == "" || len(varianteID) > 100 {
		ev.agregar("varianteId", "es obligatorio y de hasta 100 caracteres")
	}
}

func huella(partes ...string) string {
	suma := sha256.Sum256([]byte(strings.Join(partes, "|")))
	return hex.EncodeToString(suma[:])
}
