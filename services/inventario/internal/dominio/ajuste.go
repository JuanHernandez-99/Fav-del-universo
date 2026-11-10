package dominio

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Motivo explica por qué se corrige el stock (HU-08). La lista es una propuesta del equipo, a confirmar con
// el cliente: el SPEC habla de pérdidas, fallas y correcciones a mano.
type Motivo string

const (
	MotivoPerdida          Motivo = "PERDIDA"
	MotivoFalla            Motivo = "FALLA"
	MotivoCorreccionConteo Motivo = "CORRECCION_CONTEO"
)

func (m Motivo) Valido() bool {
	return m == MotivoPerdida || m == MotivoFalla || m == MotivoCorreccionConteo
}

const largoMaximoNota = 200

// Ajuste suma o resta unidades de un canal para que el stock refleje la realidad (HU-08). A diferencia del
// ingreso, la cantidad puede ser negativa. Restar tiene dos límites, que valida la base al aplicarlo:
// el online no puede quedar por debajo de lo reservado (HU-08.3) y el físico no puede quedar negativo
// [propuesta]. Sumar siempre se puede: así se corrige un físico negativo (RN-25).
type Ajuste struct {
	ClaveIdempotencia string
	VarianteID        string
	Canal             Canal
	Cantidad          int
	Motivo            Motivo
	Nota              string
	Usuario           string
}

func NuevoAjuste(clave, varianteID string, canal Canal, cantidad int, motivo Motivo, nota, usuario string) (Ajuste, error) {
	var ev ErrorValidacion
	validarClave(&ev, clave)
	validarVariante(&ev, varianteID)
	if !canal.Valido() {
		ev.agregar("canal", "debe ser ONLINE o FISICO")
	}
	if cantidad == 0 {
		ev.agregar("cantidad", "no puede ser 0: positiva para sumar, negativa para restar")
	}
	if !motivo.Valido() {
		ev.agregar("motivo", "es obligatorio: PERDIDA, FALLA o CORRECCION_CONTEO (HU-08.2)")
	}
	nota = strings.TrimSpace(nota)
	if utf8.RuneCountInString(nota) > largoMaximoNota {
		ev.agregar("nota", "hasta %d caracteres", largoMaximoNota)
	}
	return Ajuste{clave, varianteID, canal, cantidad, motivo, nota, usuario}, ev.comoError()
}

func (a Ajuste) Huella() string {
	return huella("ajuste", a.VarianteID, string(a.Canal), fmt.Sprint(a.Cantidad), string(a.Motivo), a.Nota)
}
