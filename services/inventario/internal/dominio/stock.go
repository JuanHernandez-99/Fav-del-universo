package dominio

// Canal de stock de una variante (RN-06).
type Canal string

const (
	CanalOnline Canal = "ONLINE"
	CanalFisico Canal = "FISICO"
)

// Stock de una variante en un canal.
type Stock struct {
	VarianteID string
	Canal      Canal
	Cantidad   int // unidades registradas (el físico puede quedar negativo, RN-25)
	Reservado  int // unidades apartadas por reservas activas
}

// Disponible son las unidades que se pueden reservar o transferir.
func (s Stock) Disponible() int { return s.Cantidad - s.Reservado }
