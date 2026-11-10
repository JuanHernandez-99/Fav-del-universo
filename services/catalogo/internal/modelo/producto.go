// Package modelo tiene los tipos del dominio que comparten las capas (DISENO.md 5.3).
// Los tags bson indican el nombre de cada campo en el documento de MongoDB.
package modelo

import "time"

// SistemasTalles son los sistemas de RN-02 con sus valores válidos.
var SistemasTalles = map[string][]string{
	"LETRAS":       {"XS", "S", "M", "L", "XL", "XXL"},
	"NUMERICO_PAR": {"32", "34", "36", "38", "40", "42", "44", "46"},
	"NUMERICO_1_4": {"1", "2", "3", "4"},
	"UNICO":        {"U"},
}

type Producto struct {
	ID            string         `bson:"_id"`
	Nombre        string         `bson:"nombre"`
	Descripcion   string         `bson:"descripcion,omitempty"`
	Categoria     string         `bson:"categoria"` // referencia a categorias._id
	SistemaTalles string         `bson:"sistemaTalles"`
	PrecioBase    Precio         `bson:"precioBase"`
	Publicado     bool           `bson:"publicado"`
	Atributos     map[string]any `bson:"atributos,omitempty"` // libres, varían por categoría
	Fotos         []Foto         `bson:"fotos,omitempty"`
	Variantes     []Variante     `bson:"variantes"`
	CreadoEn      time.Time      `bson:"creadoEn"`
	ActualizadoEn time.Time      `bson:"actualizadoEn"`
	Version       int            `bson:"version"`
	// PublicadoVersion es la última versión avisada por evento (ADR-006). Va en el mismo documento: escribir un
	// documento es atómico, así que un cambio queda pendiente de avisar sin necesitar una transacción.
	PublicadoVersion int `bson:"publicadoVersion,omitempty"`
}

// CambioProducto es una versión de un producto que falta avisar.
type CambioProducto struct {
	ID      string `bson:"_id"`
	Version int    `bson:"version"`
}

// DatosProducto son los campos que el administrador carga al crear y puede cambiar con PUT.
// No incluye el id (no cambia nunca), el sistema de talles (invalidaría las variantes), las
// variantes ni publicado (tienen sus propias rutas).
type DatosProducto struct {
	Nombre      string
	Descripcion string
	Categoria   string
	PrecioBase  Precio
	Atributos   map[string]any
	Fotos       []Foto
}

// Precio en centavos enteros (D13): nunca en coma flotante.
type Precio struct {
	Monto  int64  `bson:"monto"`
	Moneda string `bson:"moneda"`
}

type Foto struct {
	URL   string  `bson:"url"`
	Color *string `bson:"color"` // nil: la foto no está asociada a un color
}

type Variante struct {
	ID    string `bson:"id"`
	Talle string `bson:"talle"`
	Color string `bson:"color"`
}

// DetalleVariante es una variante junto con los datos de su producto: lo que Ventas y el panel
// piden por lote (DISENO.md sección 7). Los tags bson son los de la proyección del repositorio.
type DetalleVariante struct {
	VarianteID string `bson:"varianteId"`
	ProductoID string `bson:"productoId"`
	Nombre     string `bson:"nombre"`
	Categoria  string `bson:"categoria"`
	Talle      string `bson:"talle"`
	Color      string `bson:"color"`
	Publicado  bool   `bson:"publicado"`
	PrecioBase Precio `bson:"precioBase"`
	// Precio es el que se cobra, en centavos, y Promocion la que se aplicó (nil si ninguna). Los
	// calcula el servicio; no vienen de la base.
	Precio    int64      `bson:"-"`
	Promocion *Promocion `bson:"-"`
}

// MaximoVariantesPorLote acota la consulta por lote: Ventas arma pedidos de a lo sumo 20 variantes y
// el panel pide una página de variantes a la vez.
const MaximoVariantesPorLote = 200

// MediosDePago son los de RN-18. Si el pedido no indica uno vale MedioPagoPorDefecto (D4).
var MediosDePago = []string{"EFECTIVO", "TRANSFERENCIA", "MERCADO_PAGO"}

const MedioPagoPorDefecto = "TRANSFERENCIA"
