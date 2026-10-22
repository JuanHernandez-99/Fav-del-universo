package dominio

import (
	"fmt"
	"time"
)

type EstadoReposicion string

const (
	ReposicionPendiente EstadoReposicion = "PENDIENTE"
	ReposicionEnviada   EstadoReposicion = "ENVIADA"
	ReposicionRechazada EstadoReposicion = "RECHAZADA"
)

// Reposicion es un envío de prendas al gimnasio, que las vende en consignación (capacidad consumida, ADR-009).
// Al crearla se descuenta el stock físico; un proceso la envía al gimnasio con reintentos.
// Si el gimnasio la rechaza, las unidades vuelven al stock físico.
type Reposicion struct {
	ID                string
	ClaveIdempotencia string
	VarianteID        string
	Cantidad          int
	Estado            EstadoReposicion
	Intentos          int
	UltimoError       string
	ProximoIntento    time.Time
	CreadaEn          time.Time
	Usuario           string
}

func NuevaReposicion(id, clave, varianteID string, cantidad int, usuario string, ahora time.Time) (Reposicion, error) {
	var ev ErrorValidacion
	validarClave(&ev, clave)
	validarVariante(&ev, varianteID)
	if cantidad < 1 {
		ev.agregar("cantidad", "debe ser mayor a 0")
	}
	if err := ev.comoError(); err != nil {
		return Reposicion{}, err
	}
	ahora = ahora.UTC().Truncate(time.Second)
	return Reposicion{
		ID:                id,
		ClaveIdempotencia: clave,
		VarianteID:        varianteID,
		Cantidad:          cantidad,
		Estado:            ReposicionPendiente,
		ProximoIntento:    ahora,
		CreadaEn:          ahora,
		Usuario:           usuario,
	}, nil
}

func (r Reposicion) Huella() string {
	return huella("reposicion", r.VarianteID, fmt.Sprint(r.Cantidad))
}

// EsperaReintento crece al doble en cada intento fallido, de 5 s hasta un máximo de 5 min.
// Sin límite de intentos: la reposición ya descontó stock y tiene que llegar al gimnasio.
func EsperaReintento(intentos int) time.Duration {
	espera := 5 * time.Second
	for i := 1; i < intentos && espera < 5*time.Minute; i++ {
		espera *= 2
	}
	return min(espera, 5*time.Minute)
}

// StockConsignacion es lo que el gimnasio informa que le queda de una variante de FAV.
type StockConsignacion struct {
	VarianteID string
	Cantidad   int
	Vendidas   int
}
