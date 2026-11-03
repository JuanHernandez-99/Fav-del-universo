package modelo

import "time"

// Promocion es un descuento con vigencia, separado del precio base (DISENO.md 5.3, RN-05).
type Promocion struct {
	ID         string   `bson:"_id"`
	Nombre     string   `bson:"nombre"`
	Tipo       string   `bson:"tipo"`       // PORCENTAJE o MEDIO_PAGO
	Porcentaje int      `bson:"porcentaje"` // de 1 a 100
	MedioPago  *string  `bson:"medioPago"`  // solo si Tipo es MEDIO_PAGO
	Vigencia   Vigencia `bson:"vigencia"`
	Alcance    Alcance  `bson:"alcance"`
}

// Vigencia con los dos extremos incluidos, en UTC.
type Vigencia struct {
	Desde time.Time `bson:"desde"`
	Hasta time.Time `bson:"hasta"`
}

// Alcance indica a qué se aplica: PRODUCTO o CATEGORIA, con sus ids, o TIENDA (todo el catálogo).
type Alcance struct {
	Tipo string   `bson:"tipo"`
	IDs  []string `bson:"ids"`
}
