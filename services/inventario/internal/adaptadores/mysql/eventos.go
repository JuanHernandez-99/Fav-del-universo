package mysql

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/dominio"
)

// Formato JSON de los eventos, tal como se guardan en el outbox y se publican.
// Es el contrato con los consumidores: agregar campos es compatible; quitar o
// cambiar uno exige una versión nueva del evento (ADR-005 §5).

type sobreJSON struct {
	ID            string    `json:"id"`
	Tipo          string    `json:"tipo"`
	Version       int       `json:"version"`
	OcurridoEn    time.Time `json:"ocurridoEn"`
	CorrelationID string    `json:"correlationId"`
	Datos         any       `json:"datos"`
}

type reservaVencidaJSON struct {
	ReservaID  string    `json:"reservaId"`
	Referencia string    `json:"referencia"`
	VencidaEn  time.Time `json:"vencidaEn"`
}

func serializar(e dominio.Evento) ([]byte, error) {
	var datos any
	switch d := e.Datos.(type) {
	case dominio.ReservaVencida:
		datos = reservaVencidaJSON{ReservaID: d.ReservaID, Referencia: d.Referencia, VencidaEn: d.VencidaEn.UTC()}
	default:
		return nil, fmt.Errorf("evento %s: datos de tipo %T sin formato definido", e.Tipo, e.Datos)
	}
	return json.Marshal(sobreJSON{
		ID: e.ID, Tipo: e.Tipo, Version: e.Version, OcurridoEn: e.OcurridoEn.UTC(),
		CorrelationID: e.CorrelacionID, Datos: datos,
	})
}
