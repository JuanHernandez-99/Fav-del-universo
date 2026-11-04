package handler

import (
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/service"
)

// Promociones (HU-05). Por el gateway se accede con la clave de administrador, en /catalogo/v1/promociones.

type vigenciaEntradaJSON struct {
	Desde *time.Time `json:"desde"`
	Hasta *time.Time `json:"hasta"`
}

type alcanceJSON struct {
	Tipo string   `json:"tipo"`
	IDs  []string `json:"ids"`
}

// pedidoPromocion no lleva id: lo genera el servidor y, como se rechazan los campos desconocidos, no se
// puede forzar. Las fechas van en RFC 3339 con su zona: 2026-11-30T23:59:59-03:00.
type pedidoPromocion struct {
	Nombre     string              `json:"nombre"`
	Tipo       string              `json:"tipo"`
	Porcentaje int                 `json:"porcentaje"`
	MedioPago  *string             `json:"medioPago"`
	Vigencia   vigenciaEntradaJSON `json:"vigencia"`
	Alcance    alcanceJSON         `json:"alcance"`
}

func (p pedidoPromocion) aDatos() service.DatosPromocion {
	return service.DatosPromocion{
		Nombre: p.Nombre, Tipo: p.Tipo, Porcentaje: p.Porcentaje, MedioPago: p.MedioPago,
		Desde: p.Vigencia.Desde, Hasta: p.Vigencia.Hasta,
		Alcance: modelo.Alcance{Tipo: p.Alcance.Tipo, IDs: p.Alcance.IDs},
	}
}

type promocionCompletaJSON struct {
	PromocionID string  `json:"promocionId"`
	Nombre      string  `json:"nombre"`
	Tipo        string  `json:"tipo"`
	Porcentaje  int     `json:"porcentaje"`
	MedioPago   *string `json:"medioPago"`
	Vigencia    struct {
		Desde time.Time `json:"desde"`
		Hasta time.Time `json:"hasta"`
	} `json:"vigencia"`
	Alcance alcanceJSON `json:"alcance"`
}

func promocionAJSON(p modelo.Promocion) promocionCompletaJSON {
	j := promocionCompletaJSON{
		PromocionID: p.ID, Nombre: p.Nombre, Tipo: p.Tipo, Porcentaje: p.Porcentaje, MedioPago: p.MedioPago,
		Alcance: alcanceJSON{p.Alcance.Tipo, p.Alcance.IDs},
	}
	j.Vigencia.Desde, j.Vigencia.Hasta = p.Vigencia.Desde.UTC(), p.Vigencia.Hasta.UTC()
	return j
}

func (h *Handler) crearPromocion(w http.ResponseWriter, r *http.Request) {
	var pedido pedidoPromocion
	if !leerJSON(w, r, &pedido) {
		return
	}
	p, err := h.s.CrearPromocion(r.Context(), pedido.aDatos())
	if err != nil {
		escribirError(w, r, err)
		return
	}
	w.Header().Set("Location", "/v1/promociones/"+p.ID)
	escribirJSON(w, http.StatusCreated, promocionAJSON(p))
}

func (h *Handler) actualizarPromocion(w http.ResponseWriter, r *http.Request) {
	var pedido pedidoPromocion
	if !leerJSON(w, r, &pedido) {
		return
	}
	p, err := h.s.ActualizarPromocion(r.Context(), r.PathValue("id"), pedido.aDatos())
	if err != nil {
		escribirError(w, r, err)
		return
	}
	escribirJSON(w, http.StatusOK, promocionAJSON(p))
}

func (h *Handler) listarPromociones(w http.ResponseWriter, r *http.Request) {
	v := &modelo.ErrorValidacion{}
	for _, nombre := range slices.Sorted(maps.Keys(r.URL.Query())) {
		v.Agregar(strings.TrimSpace(nombre), "parámetro desconocido")
	}
	if err := v.ComoError(); err != nil {
		escribirError(w, r, err)
		return
	}
	promociones, err := h.s.ListarPromociones(r.Context())
	if err != nil {
		escribirError(w, r, err)
		return
	}
	items := make([]promocionCompletaJSON, len(promociones)) // vacío, no null, cuando no hay ninguna
	for i, p := range promociones {
		items[i] = promocionAJSON(p)
	}
	escribirJSON(w, http.StatusOK, struct {
		Items []promocionCompletaJSON `json:"items"`
	}{items})
}
