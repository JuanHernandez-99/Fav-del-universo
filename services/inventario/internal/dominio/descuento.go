package dominio

import (
	"fmt"
	"sort"
	"strings"
)

// DescuentoFisico baja el stock físico por una venta del local (HU-17). No se bloquea si no alcanza:
// el físico puede quedar negativo y la variante pasa a "stock a corregir" (RN-06, RN-25).
type DescuentoFisico struct {
	ClaveIdempotencia string
	Referencia        string
	Items             []Item // ordenados por variante, como en las reservas
	Usuario           string
}

func NuevoDescuentoFisico(clave, referencia string, items []Item, usuario string) (DescuentoFisico, error) {
	var ev ErrorValidacion
	validarClave(&ev, clave)
	if len(referencia) > 100 {
		ev.agregar("referencia", "no puede superar los 100 caracteres")
	}
	ordenados := validarItems(&ev, items)
	if err := ev.comoError(); err != nil {
		return DescuentoFisico{}, err
	}
	return DescuentoFisico{clave, referencia, ordenados, usuario}, nil
}

func (d DescuentoFisico) Huella() string {
	items := append([]Item(nil), d.Items...)
	sort.Slice(items, func(a, b int) bool { return items[a].VarianteID < items[b].VarianteID })
	partes := make([]string, len(items))
	for i, it := range items {
		partes[i] = fmt.Sprintf("%s:%d", it.VarianteID, it.Cantidad)
	}
	return huella("descuento", d.Referencia, strings.Join(partes, ","))
}
