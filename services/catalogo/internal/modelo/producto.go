// Package modelo tiene los tipos del dominio que comparten las capas (PROYECTO.md 5.3).
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
