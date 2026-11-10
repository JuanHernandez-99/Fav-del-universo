package http

import (
	"net/http"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/dominio"
)

// Capacidad publicada: el pedido de un grupo externo (contrato catalogo-pedidos-v1, HU-26).
// Es una entrada aparte de la compra online; el formato sale del contrato, no del modelo interno.

type pedidoNuevoJSON struct {
	ReferenciaExterna string      `json:"referenciaExterna,omitempty"`
	Items             []lineaJSON `json:"items"`
}

type itemPedidoJSON struct {
	VarianteID     string  `json:"varianteId"`
	Cantidad       int     `json:"cantidad"`
	PrecioUnitario float64 `json:"precioUnitario"`
	Subtotal       float64 `json:"subtotal"`
}

type pagoPedidoJSON struct {
	Medio              string             `json:"medio"`
	VenceEn            *time.Time         `json:"venceEn"`
	DatosTransferencia DatosTransferencia `json:"datosTransferencia"`
}

type pedidoJSON struct {
	PedidoID          string           `json:"pedidoId"`
	ReferenciaExterna string           `json:"referenciaExterna,omitempty"`
	Estado            string           `json:"estado"`
	Items             []itemPedidoJSON `json:"items"`
	Total             float64          `json:"total"`
	Moneda            string           `json:"moneda"`
	Pago              pagoPedidoJSON   `json:"pago"`
	CreadoEn          time.Time        `json:"creadoEn"`
}

func (h *Handler) crearPedido(w http.ResponseWriter, r *http.Request) {
	var p pedidoNuevoJSON
	if !leerJSON(w, r, &p) {
		return
	}
	v, err := h.s.PedirExterno(r.Context(), r.Header.Get("Idempotency-Key"), p.ReferenciaExterna, lineasDe(p.Items))
	if err != nil {
		escribirError(w, r, err)
		return
	}
	items := make([]itemPedidoJSON, len(v.Items))
	for i, it := range v.Items {
		items[i] = itemPedidoJSON{it.VarianteID, it.Cantidad, pesos(it.Precio), pesos(it.Subtotal())}
	}
	escribirJSON(w, http.StatusCreated, pedidoJSON{
		PedidoID: v.ID, ReferenciaExterna: v.ReferenciaExterna,
		// El contrato promete la misma respuesta ante una repetición y consultar el estado no está en la
		// v1, así que el pedido siempre se informa como al crearse.
		Estado: string(dominio.EstadoPendiente),
		Items:  items, Total: pesos(v.Total), Moneda: "ARS",
		Pago:     pagoPedidoJSON{string(dominio.MedioTransferencia), v.VenceEn, h.transferencia},
		CreadoEn: v.CreadaEn.UTC(),
	})
}
