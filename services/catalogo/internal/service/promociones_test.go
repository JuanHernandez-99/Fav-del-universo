package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
)

type repoPromociones struct {
	Repositorio
	guardadas map[string]modelo.Promocion
}

func nuevoRepoPromociones() *repoPromociones {
	return &repoPromociones{guardadas: map[string]modelo.Promocion{}}
}

func (r *repoPromociones) ExisteCategoria(_ context.Context, id string) (bool, error) {
	return id == "remeras", nil
}

func (r *repoPromociones) ObtenerProducto(_ context.Context, id string) (modelo.Producto, error) {
	if id == "prod-jean-mom" {
		return modelo.Producto{ID: id}, nil
	}
	return modelo.Producto{}, modelo.ErrProductoNoEncontrado
}

func (r *repoPromociones) InsertarPromocion(_ context.Context, p modelo.Promocion) error {
	if _, existe := r.guardadas[p.ID]; existe {
		return modelo.ErrIDPromocionEnUso
	}
	r.guardadas[p.ID] = p
	return nil
}

func (r *repoPromociones) ReemplazarPromocion(_ context.Context, p modelo.Promocion) error {
	if _, existe := r.guardadas[p.ID]; !existe {
		return modelo.ErrPromocionNoEncontrada
	}
	r.guardadas[p.ID] = p
	return nil
}

func datosValidos() DatosPromocion {
	desde := time.Date(2026, 12, 1, 3, 0, 0, 0, time.UTC)
	hasta := desde.Add(30 * 24 * time.Hour)
	return DatosPromocion{Nombre: "Verano en remeras", Tipo: "PORCENTAJE", Porcentaje: 20, Desde: &desde, Hasta: &hasta,
		Alcance: modelo.Alcance{Tipo: "CATEGORIA", IDs: []string{"remeras"}}}
}

func TestCrearPromocionGeneraElIDConSufijoSiSeRepite(t *testing.T) {
	s := Nuevo(nuevoRepoPromociones())
	a, err := s.CrearPromocion(context.Background(), datosValidos())
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CrearPromocion(context.Background(), datosValidos())
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != "promo-verano-en-remeras" || b.ID != "promo-verano-en-remeras-2" {
		t.Errorf("ids: %s y %s", a.ID, b.ID)
	}
}

func TestPromocionInvalidaInformaElCampo(t *testing.T) {
	efectivo, cheque := "EFECTIVO", "CHEQUE"
	ayer := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	casos := map[string]struct {
		cambio func(*DatosPromocion)
		campo  string
	}{
		"sin nombre":                 {func(d *DatosPromocion) { d.Nombre = " " }, "nombre"},
		"porcentaje cero":            {func(d *DatosPromocion) { d.Porcentaje = 0 }, "porcentaje"},
		"porcentaje de más":          {func(d *DatosPromocion) { d.Porcentaje = 101 }, "porcentaje"},
		"tipo fuera de v1 (2x1)":     {func(d *DatosPromocion) { d.Tipo = "2X1" }, "tipo"},
		"sin inicio (HU-05.2)":       {func(d *DatosPromocion) { d.Desde = nil }, "vigencia.desde"},
		"sin fin (HU-05.2)":          {func(d *DatosPromocion) { d.Hasta = nil }, "vigencia.hasta"},
		"vigencia invertida":         {func(d *DatosPromocion) { d.Hasta = &ayer }, "vigencia.desde"},
		"medio sin indicar":          {func(d *DatosPromocion) { d.Tipo = "MEDIO_PAGO" }, "medioPago"},
		"medio desconocido":          {func(d *DatosPromocion) { d.Tipo, d.MedioPago = "MEDIO_PAGO", &cheque }, "medioPago"},
		"medio en una de porcentaje": {func(d *DatosPromocion) { d.MedioPago = &efectivo }, "medioPago"},
		"alcance desconocido":        {func(d *DatosPromocion) { d.Alcance.Tipo = "BANCO" }, "alcance.tipo"},
		"tienda con ids":             {func(d *DatosPromocion) { d.Alcance.Tipo = "TIENDA" }, "alcance.ids"},
		"categoría sin ids":          {func(d *DatosPromocion) { d.Alcance.IDs = nil }, "alcance.ids"},
		"categoría inexistente":      {func(d *DatosPromocion) { d.Alcance.IDs = []string{"remera"} }, "alcance.ids"},
		"producto inexistente":       {func(d *DatosPromocion) { d.Alcance = modelo.Alcance{Tipo: "PRODUCTO", IDs: []string{"prod-nada"}} }, "alcance.ids"},
	}
	for nombre, c := range casos {
		d := datosValidos()
		c.cambio(&d)
		repo := nuevoRepoPromociones()
		_, err := Nuevo(repo).CrearPromocion(context.Background(), d)
		var v *modelo.ErrorValidacion
		if !errors.As(err, &v) || !strings.Contains(err.Error(), c.campo) {
			t.Errorf("%s: %v", nombre, err)
		}
		if len(repo.guardadas) != 0 {
			t.Errorf("%s: no debía guardarse", nombre)
		}
	}
}

func TestPromocionesValidasDeCadaAlcance(t *testing.T) {
	efectivo := "EFECTIVO"
	casos := map[string]func(*DatosPromocion){
		"producto": func(d *DatosPromocion) { d.Alcance = modelo.Alcance{Tipo: "PRODUCTO", IDs: []string{"prod-jean-mom"}} },
		"tienda con medio de pago": func(d *DatosPromocion) {
			d.Tipo, d.MedioPago, d.Alcance = "MEDIO_PAGO", &efectivo, modelo.Alcance{Tipo: "TIENDA"}
		},
	}
	for nombre, cambio := range casos {
		d := datosValidos()
		cambio(&d)
		p, err := Nuevo(nuevoRepoPromociones()).CrearPromocion(context.Background(), d)
		if err != nil {
			t.Errorf("%s: %v", nombre, err)
		}
		if p.Alcance.IDs == nil {
			t.Errorf("%s: ids tiene que ser un arreglo, el esquema de la base no acepta null", nombre)
		}
	}
}

func TestLaVigenciaSeGuardaEnUTC(t *testing.T) {
	d := datosValidos()
	argentina := time.FixedZone("ART", -3*3600)
	hasta := time.Date(2026, 12, 31, 23, 59, 59, 0, argentina)
	d.Hasta = &hasta
	p, err := Nuevo(nuevoRepoPromociones()).CrearPromocion(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if esperado := time.Date(2027, 1, 1, 2, 59, 59, 0, time.UTC); !p.Vigencia.Hasta.Equal(esperado) || p.Vigencia.Hasta.Location() != time.UTC {
		t.Errorf("hasta: %v (sección 14, punto 14)", p.Vigencia.Hasta)
	}
}

// DISENO.md 5.3: una promoción se cancela editando su vigencia, aunque quede en el pasado.
func TestActualizarConservaElIDYPermiteCancelar(t *testing.T) {
	repo := nuevoRepoPromociones()
	s := Nuevo(repo)
	p, err := s.CrearPromocion(context.Background(), datosValidos())
	if err != nil {
		t.Fatal(err)
	}
	d := datosValidos()
	d.Nombre = "Otro nombre"
	desde, hasta := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)
	d.Desde, d.Hasta = &desde, &hasta
	got, err := s.ActualizarPromocion(context.Background(), p.ID, d)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != p.ID || repo.guardadas[p.ID].Nombre != "Otro nombre" || !repo.guardadas[p.ID].Vigencia.Hasta.Equal(hasta) {
		t.Errorf("guardada: %+v", repo.guardadas[p.ID])
	}
	if _, err := s.ActualizarPromocion(context.Background(), "promo-nada", datosValidos()); !errors.Is(err, modelo.ErrPromocionNoEncontrada) {
		t.Errorf("inexistente: %v", err)
	}
}
