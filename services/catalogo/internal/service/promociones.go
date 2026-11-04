package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
)

// DatosPromocion son los campos que carga el administrador. Vigencia con punteros: así se distingue
// una fecha que no vino, que se rechaza (HU-05.2). El id lo genera el servidor (D12).
type DatosPromocion struct {
	Nombre     string
	Tipo       string
	Porcentaje int
	MedioPago  *string
	Desde      *time.Time
	Hasta      *time.Time
	Alcance    modelo.Alcance
}

// CrearPromocion valida, genera el id y guarda. Si el nombre ya existe se agrega un sufijo, igual que
// en los productos.
func (s *Servicio) CrearPromocion(ctx context.Context, d DatosPromocion) (modelo.Promocion, error) {
	p, err := s.armarPromocion(ctx, d)
	if err != nil {
		return modelo.Promocion{}, err
	}
	for intento := 1; intento <= intentosDeID; intento++ {
		p.ID = modelo.IDPromocion(p.Nombre, intento)
		err := s.repo.InsertarPromocion(ctx, p)
		if errors.Is(err, modelo.ErrIDPromocionEnUso) {
			continue
		}
		if err != nil {
			return modelo.Promocion{}, err
		}
		return p, nil
	}
	return modelo.Promocion{}, fmt.Errorf("no se encontró un id libre para %q en %d intentos", p.Nombre, intentosDeID)
}

// ActualizarPromocion reemplaza todos los datos; el id no cambia. Una promoción se cancela o se acorta
// editando su vigencia (DISENO.md 5.3): no se borra.
func (s *Servicio) ActualizarPromocion(ctx context.Context, id string, d DatosPromocion) (modelo.Promocion, error) {
	p, err := s.armarPromocion(ctx, d)
	if err != nil {
		return modelo.Promocion{}, err
	}
	p.ID = id
	if err := s.repo.ReemplazarPromocion(ctx, p); err != nil {
		return modelo.Promocion{}, err
	}
	return p, nil
}

func (s *Servicio) ListarPromociones(ctx context.Context) ([]modelo.Promocion, error) {
	return s.repo.ListarPromociones(ctx)
}

// armarPromocion junta todos los problemas y los devuelve juntos (DISENO.md 5.6).
func (s *Servicio) armarPromocion(ctx context.Context, d DatosPromocion) (modelo.Promocion, error) {
	v := &modelo.ErrorValidacion{}
	d.Nombre = strings.TrimSpace(d.Nombre)
	if d.Nombre == "" {
		v.Agregar("nombre", "es obligatorio")
	} else if modelo.Normalizar(d.Nombre) == "" {
		v.Agregar("nombre", "tiene que incluir al menos una letra o un número")
	}
	if d.Porcentaje < 1 || d.Porcentaje > 100 {
		v.Agregar("porcentaje", "tiene que estar entre 1 y 100")
	}

	switch d.Tipo {
	case "PORCENTAJE":
		if d.MedioPago != nil {
			v.Agregar("medioPago", "solo va en las promociones de tipo MEDIO_PAGO")
		}
	case "MEDIO_PAGO":
		if d.MedioPago == nil || !slices.Contains(modelo.MediosDePago, *d.MedioPago) {
			v.Agregar("medioPago", "es obligatorio y tiene que ser uno de: %s", strings.Join(modelo.MediosDePago, ", "))
		}
	default:
		v.Agregar("tipo", "tiene que ser PORCENTAJE o MEDIO_PAGO (RN-05)")
	}

	if d.Desde == nil {
		v.Agregar("vigencia.desde", "es obligatoria (HU-05.2)")
	}
	if d.Hasta == nil {
		v.Agregar("vigencia.hasta", "es obligatoria (HU-05.2)")
	}
	if d.Desde != nil && d.Hasta != nil && !d.Desde.Before(*d.Hasta) {
		v.Agregar("vigencia.desde", "tiene que ser anterior a hasta")
	}

	alcance, err := s.validarAlcance(ctx, v, d.Alcance)
	if err != nil {
		return modelo.Promocion{}, err
	}
	if err := v.ComoError(); err != nil {
		return modelo.Promocion{}, err
	}
	return modelo.Promocion{
		Nombre: d.Nombre, Tipo: d.Tipo, Porcentaje: d.Porcentaje, MedioPago: d.MedioPago, Alcance: alcance,
		// En UTC y en milisegundos, como las guarda Mongo (sección 14, punto 14).
		Vigencia: modelo.Vigencia{Desde: d.Desde.UTC().Truncate(time.Millisecond), Hasta: d.Hasta.UTC().Truncate(time.Millisecond)},
	}, nil
}

// validarAlcance exige que los ids existan: con un error de tipeo la promoción no se aplicaría a nada,
// sin que nadie se entere. Solo devuelve error si falló la consulta a la base.
func (s *Servicio) validarAlcance(ctx context.Context, v *modelo.ErrorValidacion, a modelo.Alcance) (modelo.Alcance, error) {
	ids := []string{} // vacío, no nil: el esquema de la base exige un arreglo
	for _, id := range a.IDs {
		if id = strings.TrimSpace(id); !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	a.IDs = ids

	switch a.Tipo {
	case "TIENDA":
		if len(ids) > 0 {
			v.Agregar("alcance.ids", "tiene que estar vacío cuando el alcance es TIENDA")
		}
	case "PRODUCTO", "CATEGORIA":
		if len(ids) == 0 {
			v.Agregar("alcance.ids", "tiene que indicar al menos un id")
		}
		for _, id := range ids {
			existe, err := s.existeEnAlcance(ctx, a.Tipo, id)
			if err != nil {
				return a, err
			}
			if !existe {
				v.Agregar("alcance.ids", "no existe %q en %s", id, strings.ToLower(a.Tipo))
			}
		}
	default:
		v.Agregar("alcance.tipo", "tiene que ser PRODUCTO, CATEGORIA o TIENDA")
	}
	return a, nil
}

func (s *Servicio) existeEnAlcance(ctx context.Context, tipo, id string) (bool, error) {
	if tipo == "CATEGORIA" {
		return s.repo.ExisteCategoria(ctx, id)
	}
	_, err := s.repo.ObtenerProducto(ctx, id)
	if errors.Is(err, modelo.ErrProductoNoEncontrado) {
		return false, nil
	}
	return err == nil, err
}
