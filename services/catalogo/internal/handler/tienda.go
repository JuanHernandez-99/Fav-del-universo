package handler

import (
	"net/http"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/service"
)

// Lectura del comprador (HU-01, HU-02). Es pública y no depende de CAPACIDAD_PUBLICA_ACTIVA: esa variable
// apaga lo que se le ofrece al otro grupo, no la tienda. Si hay stock lo responde Inventario (D1).

type productoTiendaJSON struct {
	ProductoID  string         `json:"productoId"`
	Nombre      string         `json:"nombre"`
	Descripcion string         `json:"descripcion,omitempty"`
	Categoria   string         `json:"categoria"`
	Precio      float64        `json:"precio"`
	PrecioBase  float64        `json:"precioBase"`
	Promocion   *promocionJSON `json:"promocion,omitempty"`
	Fotos       []fotoJSON     `json:"fotos"`
	Variantes   []varianteJSON `json:"variantes"`
}

func productoTiendaAJSON(t service.ProductoTienda) productoTiendaJSON {
	p := t.Producto
	j := productoTiendaJSON{
		ProductoID: p.ID, Nombre: p.Nombre, Descripcion: p.Descripcion, Categoria: p.Categoria,
		Precio: pesos(t.Precio), PrecioBase: pesos(p.PrecioBase.Monto),
		Fotos: make([]fotoJSON, len(p.Fotos)), Variantes: make([]varianteJSON, len(p.Variantes)),
	}
	if t.Promocion != nil {
		j.Promocion = &promocionJSON{t.Promocion.ID, t.Promocion.Nombre, t.Promocion.Porcentaje}
	}
	for i, f := range p.Fotos {
		j.Fotos[i] = fotoJSON{f.URL, f.Color}
	}
	for i, v := range p.Variantes {
		j.Variantes[i] = varianteJSON{v.ID, v.Talle, v.Color}
	}
	return j
}

func (h *Handler) listarTienda(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Has("publicado") { // para el comprador no es una opción: ve solo lo publicado (HU-01.2)
		v := &modelo.ErrorValidacion{}
		v.Agregar("publicado", "parámetro desconocido")
		escribirError(w, r, v)
		return
	}
	f, err := parsearFiltro(q)
	if err != nil {
		escribirError(w, r, err)
		return
	}
	productos, total, err := h.s.ListarTienda(r.Context(), f)
	if err != nil {
		escribirError(w, r, err)
		return
	}
	items := make([]productoTiendaJSON, len(productos)) // vacío, no null, cuando no hay resultados
	for i, p := range productos {
		items[i] = productoTiendaAJSON(p)
	}
	escribirJSON(w, http.StatusOK, struct {
		Items   []productoTiendaJSON `json:"items"`
		Pagina  int                  `json:"pagina"`
		Tamano  int                  `json:"tamano"`
		Total   int64                `json:"total"`
		Paginas int64                `json:"paginas"`
	}{items, f.Pagina, f.Tamano, total, (total + int64(f.Tamano) - 1) / int64(f.Tamano)})
}

func (h *Handler) obtenerDeTienda(w http.ResponseWriter, r *http.Request) {
	p, err := h.s.ObtenerDeTienda(r.Context(), r.PathValue("id"))
	if err != nil {
		escribirError(w, r, err)
		return
	}
	escribirJSON(w, http.StatusOK, productoTiendaAJSON(p))
}
