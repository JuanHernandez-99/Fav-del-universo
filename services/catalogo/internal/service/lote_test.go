package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
)

// repoDeVariantes implementa solo lo que usa la consulta por lote; el resto de Repositorio queda nil
// y cualquier llamada de más haría fallar el test.
type repoDeVariantes struct {
	Repositorio
	existentes map[string]modelo.DetalleVariante
	pedidos    [][]string
}

func (r *repoDeVariantes) ObtenerVariantes(_ context.Context, ids []string) ([]modelo.DetalleVariante, error) {
	r.pedidos = append(r.pedidos, ids)
	var encontradas []modelo.DetalleVariante
	for _, id := range slices.Backward(ids) { // al revés, para que el servicio no dependa del orden de la base
		if d, ok := r.existentes[id]; ok {
			encontradas = append(encontradas, d)
		}
	}
	return encontradas, nil
}

func variantesDePrueba() *repoDeVariantes {
	return &repoDeVariantes{existentes: map[string]modelo.DetalleVariante{
		"var-remera-basica-blanca-m": {VarianteID: "var-remera-basica-blanca-m", ProductoID: "prod-remera-basica", Nombre: "Remera básica",
			Categoria: "remeras", Talle: "M", Color: "Blanco", Publicado: true, PrecioBase: modelo.Precio{Monto: 1500000, Moneda: "ARS"}},
		"var-jean-mom-celeste-38": {VarianteID: "var-jean-mom-celeste-38", ProductoID: "prod-jean-mom", Nombre: "Jean mom",
			Categoria: "jeans", Talle: "38", Color: "Celeste", Publicado: false, PrecioBase: modelo.Precio{Monto: 5750000, Moneda: "ARS"}},
	}}
}

func TestConsultarVariantes(t *testing.T) {
	repo := variantesDePrueba()
	s := Nuevo(repo)

	got, err := s.ConsultarVariantes(context.Background(), []string{"var-jean-mom-celeste-38", " var-remera-basica-blanca-m", "var-jean-mom-celeste-38"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].VarianteID != "var-jean-mom-celeste-38" || got[1].VarianteID != "var-remera-basica-blanca-m" {
		t.Fatalf("tiene que devolver cada id una vez y en el orden pedido: %+v", got)
	}
	if got[0].Precio != 5750000 || got[1].Precio != 1500000 {
		t.Errorf("el precio es el base (en centavos): %d y %d", got[0].Precio, got[1].Precio)
	}
	if got[0].Publicado {
		t.Error("no se filtra por publicado: la variante del producto no publicado tiene que venir igual (D6)")
	}
	if len(repo.pedidos) != 1 || len(repo.pedidos[0]) != 2 {
		t.Errorf("a la base se le pide una vez, sin repetidos ni espacios: %v", repo.pedidos)
	}
}

func TestConsultarVariantesInexistentes(t *testing.T) {
	s := Nuevo(variantesDePrueba())
	_, err := s.ConsultarVariantes(context.Background(), []string{"no-existe", "var-remera-basica-blanca-m", "tampoco"}, "")
	var faltan *modelo.ErrVariantesInexistentes
	if !errors.As(err, &faltan) || !slices.Equal(faltan.IDs, []string{"no-existe", "tampoco"}) {
		t.Fatalf("tiene que listar todos los que faltan, en el orden pedido: %v", err)
	}
}

func TestConsultarVariantesPedidoInvalido(t *testing.T) {
	masDeLaCuenta := make([]string, modelo.MaximoVariantesPorLote+1)
	for i := range masDeLaCuenta {
		masDeLaCuenta[i] = fmt.Sprintf("var-%d", i)
	}
	casos := []struct {
		nombre, campo string
		ids           []string
		medioPago     string
	}{
		{"sin ids", "variantes", nil, ""},
		{"id vacío", "variantes", []string{"var-a", " "}, ""},
		{"demasiados", "variantes", masDeLaCuenta, ""},
		{"medio de pago inválido", "medioPago", []string{"var-a"}, "BITCOIN"},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			repo := variantesDePrueba()
			_, err := Nuevo(repo).ConsultarVariantes(context.Background(), c.ids, c.medioPago)
			var v *modelo.ErrorValidacion
			if !errors.As(err, &v) || !strings.Contains(v.Error(), c.campo) {
				t.Fatalf("se esperaba un error de validación en %q: %v", c.campo, err)
			}
			if len(repo.pedidos) != 0 {
				t.Error("un pedido inválido no tiene que llegar a la base")
			}
		})
	}
}
