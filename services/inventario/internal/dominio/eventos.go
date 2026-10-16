package dominio

import "time"

// Evento es un hecho del dominio que se avisa a otros servicios (ADR-005 §5).
// El sobre es común a todos los eventos; Datos depende del tipo. El formato en
// que viaja lo decide el adaptador que lo guarda.
type Evento struct {
	ID            string
	Tipo          string
	Version       int
	OcurridoEn    time.Time
	CorrelacionID string
	Datos         any
}

const TipoReservaVencida = "ReservaVencida"

// ReservaVencida: la reserva cumplió su plazo sin confirmarse y devolvió sus unidades.
// Lleva la referencia que envió quien reservó: Inventario no conoce las ventas.
type ReservaVencida struct {
	ReservaID  string
	Referencia string
	VencidaEn  time.Time
}

// EventoVencimiento arma el evento ReservaVencida de la reserva.
func (r Reserva) EventoVencimiento(id string, ahora time.Time) Evento {
	ahora = ahora.UTC()
	return Evento{
		ID:            id,
		Tipo:          TipoReservaVencida,
		Version:       1,
		OcurridoEn:    ahora,
		CorrelacionID: r.CorrelacionID,
		Datos:         ReservaVencida{ReservaID: r.ID, Referencia: r.Referencia, VencidaEn: ahora},
	}
}
