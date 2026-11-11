package repository

// Tests de integración contra un MongoDB real. Necesitan CATALOGO_TEST_MONGO_URI; sin ella se saltean. Usan una
// base propia y la borran al terminar:
//
//	docker run -d --rm --name mongo-pruebas -p 27099:27017 mongo:7
//	CATALOGO_TEST_MONGO_URI=mongodb://localhost:27099 go test ./internal/repository/

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/observabilidad"
)

var ctx = context.Background()

func repoDePrueba(t *testing.T) *Repositorio {
	t.Helper()
	uri := os.Getenv("CATALOGO_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("CATALOGO_TEST_MONGO_URI no definida: se saltea el test de integración")
	}
	repo, err := Conectar(uri, fmt.Sprintf("catalogo_prueba_%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.AsegurarIndices(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = repo.productos.Database().Drop(ctx)
		_ = repo.Cerrar(ctx)
	})
	return repo
}

func nuevo(t *testing.T, repo *Repositorio, id string) modelo.Producto {
	t.Helper()
	ahora := time.Now().UTC().Truncate(time.Millisecond)
	p := modelo.Producto{
		ID: id, Nombre: "Remera " + id, Categoria: "remeras", SistemaTalles: "LETRAS",
		PrecioBase: modelo.Precio{Monto: 1500000, Moneda: "ARS"},
		Variantes:  []modelo.Variante{{ID: "var-" + id + "-m", Talle: "M", Color: "Blanco"}},
		CreadoEn:   ahora, ActualizadoEn: ahora, Version: 1,
	}
	if err := repo.InsertarProducto(ctx, p); err != nil {
		t.Fatal(err)
	}
	return p
}

func pendientes(t *testing.T, repo *Repositorio) map[string]int {
	t.Helper()
	cambios, err := repo.ProductosSinPublicar(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	porID := map[string]int{}
	for _, c := range cambios {
		porID[c.ID] = c.Version
	}
	return porID
}

// El outbox dentro del documento: todo cambio deja el producto pendiente, sin transacción.
func TestUnCambioQuedaPendienteHastaAvisarlo(t *testing.T) {
	repo := repoDePrueba(t)
	nuevo(t, repo, "a")
	if p := pendientes(t, repo); p["a"] != 1 {
		t.Fatalf("un producto nuevo queda pendiente: %v", p)
	}
	if err := repo.MarcarPublicado(ctx, "a", 1); err != nil {
		t.Fatal(err)
	}
	if p := pendientes(t, repo); len(p) != 0 {
		t.Fatalf("ya se avisó: %v", p)
	}

	ahora := time.Now().UTC()
	for paso, cambio := range []func() error{
		func() error { _, err := repo.Publicar(ctx, "a", ahora); return err },
		func() error { return repo.Despublicar(ctx, "a", ahora) },
		func() error {
			return repo.AgregarVariante(ctx, "a", modelo.Variante{ID: "var-a-l", Talle: "L", Color: "Negro"}, ahora)
		},
		func() error {
			p, err := repo.ObtenerProducto(ctx, "a")
			if err != nil {
				return err
			}
			_, err = repo.ActualizarProducto(ctx, "a", p.Version, modelo.DatosProducto{Nombre: "Otra", Categoria: "remeras", PrecioBase: p.PrecioBase}, ahora)
			return err
		},
	} {
		if err := cambio(); err != nil {
			t.Fatal(err)
		}
		version := pendientes(t, repo)["a"]
		if version != paso+2 {
			t.Fatalf("paso %d: pendiente en versión %d", paso, version)
		}
		if err := repo.MarcarPublicado(ctx, "a", version); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMarcarNoTocaElProductoNiBajaLaVersionAvisada(t *testing.T) {
	repo := repoDePrueba(t)
	antes := nuevo(t, repo, "a")
	for _, version := range []int{5, 3} { // la 3 llega tarde, de otra réplica
		if err := repo.MarcarPublicado(ctx, "a", version); err != nil {
			t.Fatal(err)
		}
	}
	p, err := repo.ObtenerProducto(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if p.PublicadoVersion != 5 {
		t.Fatalf("publicadoVersion: %d", p.PublicadoVersion)
	}
	if p.Version != antes.Version || !p.ActualizadoEn.Equal(antes.ActualizadoEn) {
		t.Fatalf("avisar cambió el producto: versión %d, actualizado %v", p.Version, p.ActualizadoEn)
	}
}

func TestUnProductoCargadoSinElCampoCuentaComoPendiente(t *testing.T) {
	repo := repoDePrueba(t)
	nuevo(t, repo, "viejo")
	if _, err := repo.productos.UpdateByID(ctx, "viejo", map[string]any{"$unset": map[string]any{"publicadoVersion": ""}}); err != nil {
		t.Fatal(err)
	}
	if p := pendientes(t, repo); p["viejo"] != 1 {
		t.Fatalf("pendientes: %v", p)
	}
}

func TestProductosPorIDs(t *testing.T) {
	repo := repoDePrueba(t)
	nuevo(t, repo, "a")
	nuevo(t, repo, "b")
	nuevo(t, repo, "c")
	productos, err := repo.ProductosPorIDs(ctx, []string{"a", "c", "no-existe"})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, p := range productos {
		ids = append(ids, p.ID)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"a", "c"}) {
		t.Fatalf("ids: %v", ids)
	}
}

func TestModificadosDesdeIncluyeLosDespublicados(t *testing.T) {
	repo := repoDePrueba(t)
	viejo := nuevo(t, repo, "viejo")
	limite := viejo.ActualizadoEn.Add(time.Minute)
	nuevo(t, repo, "cambiado")
	if err := repo.Despublicar(ctx, "cambiado", limite); err != nil { // no estaba publicado: no cambia
		t.Fatal(err)
	}
	if _, err := repo.Publicar(ctx, "cambiado", limite.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := repo.Despublicar(ctx, "cambiado", limite.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	productos, err := repo.ProductosModificadosDesde(ctx, limite)
	if err != nil {
		t.Fatal(err)
	}
	if len(productos) != 1 || productos[0].ID != "cambiado" || productos[0].Publicado {
		t.Fatalf("productos: %+v", productos)
	}
}

// La operación crítica de Catálogo (DISENO.md 5.9): con pedidos simultáneos, una sola variante entra.
func TestAgregarVarianteConcurrenteAgregaUnaSola(t *testing.T) {
	repo := repoDePrueba(t)
	nuevo(t, repo, "a")
	var espera sync.WaitGroup
	var mu sync.Mutex
	exitos, duplicadas := 0, 0
	for range 20 {
		espera.Go(func() {
			err := repo.AgregarVariante(ctx, "a", modelo.Variante{ID: "var-a-l", Talle: "L", Color: "Negro"}, time.Now().UTC())
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				exitos++
			case errors.Is(err, modelo.ErrVarianteDuplicada):
				duplicadas++
			default:
				t.Errorf("error: %v", err)
			}
		})
	}
	espera.Wait()
	p, err := repo.ObtenerProducto(ctx, "a")
	if err != nil {
		t.Fatal(err)
	}
	if exitos != 1 || duplicadas != 19 || len(p.Variantes) != 2 || p.Version != 2 {
		t.Fatalf("éxitos %d, duplicadas %d, variantes %d, versión %d", exitos, duplicadas, len(p.Variantes), p.Version)
	}
}

// El cambio guarda el track id del pedido que lo hizo, y el outbox lo devuelve para mandarlo en el evento (ADR-020).
func TestElCambioGuardaElTrackIDDelPedido(t *testing.T) {
	repo := repoDePrueba(t)
	nuevo(t, repo, "a")
	ctxAdmin := observabilidad.ConCorrelationID(ctx, "publicar-remera-9")
	if _, err := repo.Publicar(ctxAdmin, "a", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	cambios, err := repo.ProductosSinPublicar(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(cambios) != 1 || cambios[0].Correlacion != "publicar-remera-9" {
		t.Fatalf("cambios = %+v, esperaba el track id publicar-remera-9", cambios)
	}
}
