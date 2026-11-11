package http

import (
	"encoding/json"
	"errors"
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

// Estados del pedido en el contrato. No son los de la venta: el contrato los agrupa (ver estadoDelPedido).
const (
	pedidoPendiente  = "PENDIENTE_DE_PAGO"
	pedidoPagado     = "PAGADO"
	pedidoVencido    = "VENCIDO"
	pedidoAVerificar = "A_VERIFICAR"
)

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
	// El contrato promete la misma respuesta ante una repetición, así que el alta siempre informa el
	// estado con el que se creó. El estado vigente se consulta con GET /v1/pedidos/{pedidoId}.
	escribirJSON(w, http.StatusCreated, h.pedidoDe(v, pedidoPendiente))
}

// consultarPedido devuelve el pedido con su estado vigente (contrato 1.3.0). Solo responde pedidos de
// grupos externos: una compra online, una venta del local o un id inexistente dan el mismo 404.
func (h *Handler) consultarPedido(w http.ResponseWriter, r *http.Request) {
	v, err := h.s.PedidoExterno(r.Context(), r.PathValue("id"))
	if errors.Is(err, dominio.ErrVentaNoEncontrada) {
		escribirPedidoInexistente(w)
		return
	}
	if err != nil {
		escribirError(w, r, err)
		return
	}
	escribirJSON(w, http.StatusOK, h.pedidoDe(v, estadoDelPedido(v.Estado)))
}

// estadoDelPedido traduce el estado de la venta al del contrato. Despachar y entregar no cambian lo que
// ve el grupo: para él, el pedido está pagado y la entrega se coordina por el contacto.
func estadoDelPedido(e dominio.Estado) string {
	switch e {
	case dominio.EstadoPendiente:
		// También con el pago ya registrado, mientras Inventario confirma la reserva: todavía puede
		// terminar en A_VERIFICAR si la reserva había vencido.
		return pedidoPendiente
	case dominio.EstadoPagada, dominio.EstadoEnCorreo, dominio.EstadoEntregada:
		return pedidoPagado
	case dominio.EstadoVencida:
		return pedidoVencido
	}
	// A_VERIFICAR y cualquier estado que un pedido externo no debería alcanzar (CANCELADA, REGISTRADA):
	// FAV lo revisa a mano.
	return pedidoAVerificar
}

func (h *Handler) pedidoDe(v dominio.Venta, estado string) pedidoJSON {
	items := make([]itemPedidoJSON, len(v.Items))
	for i, it := range v.Items {
		items[i] = itemPedidoJSON{it.VarianteID, it.Cantidad, pesos(it.Precio), pesos(it.Subtotal())}
	}
	return pedidoJSON{
		PedidoID: v.ID, ReferenciaExterna: v.ReferenciaExterna, Estado: estado,
		Items: items, Total: pesos(v.Total), Moneda: "ARS",
		Pago:     pagoPedidoJSON{string(dominio.MedioTransferencia), v.VenceEn, h.transferencia},
		CreadoEn: v.CreadaEn.UTC(),
	}
}

// escribirPedidoInexistente usa el código del contrato, no VENTA_NO_ENCONTRADA del panel.
func escribirPedidoInexistente(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(problema{
		Type: "https://fav.example/errores/pedido-inexistente", Title: "Pedido inexistente", Status: http.StatusNotFound,
		Detail: "No hay un pedido con ese pedidoId.", Codigo: "PEDIDO_INEXISTENTE",
	})
}
