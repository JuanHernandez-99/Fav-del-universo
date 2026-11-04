package handler

import (
	"net/http"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
)

// Capacidad publicada: el catálogo que ve otro grupo (contrato catalogo-pedidos-v1, D14). Es una entrada
// aparte de la API de administración; su formato sale del contrato, no del modelo interno, y se puede
// apagar con SinCapacidadPublica (ADR-008).

type productoPublicoJSON struct {
	ProductoID  string                `json:"productoId"`
	Nombre      string                `json:"nombre"`
	Categoria   string                `json:"categoria"`
	Descripcion string                `json:"descripcion,omitempty"`
	FotoURL     string                `json:"fotoUrl,omitempty"`
	Variantes   []variantePublicaJSON `json:"variantes"`
}

// variantePublicaJSON no lleva stock: si una variante no alcanza, lo informa el pedido (ADR-008).
type variantePublicaJSON struct {
	VarianteID string  `json:"varianteId"`
	Talle      string  `json:"talle"`
	Color      string  `json:"color"`
	Precio     float64 `json:"precio"`
}

func (h *Handler) listarCatalogoPublico(w http.ResponseWriter, r *http.Request) {
	catalogo, err := h.s.CatalogoPublico(r.Context())
	if err != nil {
		escribirError(w, r, err)
		return
	}
	items := make([]productoPublicoJSON, len(catalogo)) // vacío, no null, cuando no hay publicados
	for i, c := range catalogo {
		items[i] = productoPublicoAJSON(c.Producto, c.Precio)
	}
	escribirJSON(w, http.StatusOK, struct {
		Items []productoPublicoJSON `json:"items"`
	}{items})
}

func productoPublicoAJSON(p modelo.Producto, precio int64) productoPublicoJSON {
	variantes := make([]variantePublicaJSON, len(p.Variantes))
	for i, v := range p.Variantes {
		variantes[i] = variantePublicaJSON{v.ID, v.Talle, v.Color, pesos(precio)}
	}
	publico := productoPublicoJSON{
		ProductoID: p.ID, Nombre: p.Nombre, Categoria: p.Categoria, Descripcion: p.Descripcion, Variantes: variantes,
	}
	if len(p.Fotos) > 0 {
		publico.FotoURL = p.Fotos[0].URL // la primera es la principal (sección 14, punto 5)
	}
	return publico
}
