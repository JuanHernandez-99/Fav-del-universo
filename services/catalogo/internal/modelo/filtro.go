package modelo

// Orden del listado de productos. Siempre se desempata por _id (DISENO.md sección 7).
type Orden string

const (
	OrdenNuevos     Orden = "nuevos" // por creadoEn, el más nuevo primero (por defecto)
	OrdenPrecioAsc  Orden = "precio_asc"
	OrdenPrecioDesc Orden = "precio_desc"
	OrdenNombre     Orden = "nombre"
)

var Ordenes = []Orden{OrdenNuevos, OrdenPrecioAsc, OrdenPrecioDesc, OrdenNombre}

const (
	TamanoPorDefecto = 20
	TamanoMaximo     = 100
	PaginaMaxima     = 100000
)

// FiltroProductos son los parámetros del listado. Un campo vacío (o nil) no filtra. Los precios
// están en centavos (D13) y se comparan contra el precio base (sección 14, punto 7).
type FiltroProductos struct {
	Categoria string
	Talle     string
	Color     string
	Texto     string // búsqueda en nombre y descripción
	PrecioMin *int64
	PrecioMax *int64
	Publicado *bool
	Orden     Orden
	Pagina    int // desde 1
	Tamano    int
}
