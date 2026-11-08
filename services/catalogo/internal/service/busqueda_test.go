package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
)

type repoBusqueda struct {
	Repositorio
	productos  map[string]modelo.Producto
	pendientes []modelo.CambioProducto
	marcados   []modelo.CambioProducto
	listados   int
}

func (r *repoBusqueda) ProductosPorIDs(_ context.Context, ids []string) ([]modelo.Producto, error) {
	var encontrados []modelo.Producto
	for _, id := range ids {
		if p, ok := r.productos[id]; ok {
			encontrados = append(encontrados, p)
		}
	}
	return encontrados, nil
}

func (r *repoBusqueda) ObtenerProducto(_ context.Context, id string) (modelo.Producto, error) {
	if p, ok := r.productos[id]; ok {
		return p, nil
	}
	return modelo.Producto{}, modelo.ErrProductoNoEncontrado
}

func (r *repoBusqueda) ListarProductos(context.Context, modelo.FiltroProductos) ([]modelo.Producto, int64, error) {
	r.listados++
	return []modelo.Producto{}, 0, nil
}

func (r *repoBusqueda) PromocionesVigentes(context.Context, time.Time) ([]modelo.Promocion, error) {
	return nil, nil
}

func (r *repoBusqueda) ProductosPublicados(context.Context) ([]modelo.Producto, error) {
	return []modelo.Producto{r.productos["a"]}, nil
}

func (r *repoBusqueda) ProductosModificadosDesde(context.Context, time.Time) ([]modelo.Producto, error) {
	return []modelo.Producto{r.productos["despublicado"]}, nil
}

func (r *repoBusqueda) ProductosSinPublicar(context.Context, int) ([]modelo.CambioProducto, error) {
	return r.pendientes, nil
}

func (r *repoBusqueda) MarcarPublicado(_ context.Context, id string, version int) error {
	r.marcados = append(r.marcados, modelo.CambioProducto{ID: id, Version: version})
	return nil
}

type indiceFalso struct {
	Indice
	ids       []string
	total     int64
	err       error
	indexados []string
	quitados  []string
	cargados  []modelo.Producto
	creado    bool
}

func (i *indiceFalso) Buscar(context.Context, modelo.FiltroProductos) ([]string, int64, error) {
	return i.ids, i.total, i.err
}

func (i *indiceFalso) Indexar(_ context.Context, p modelo.Producto) error {
	i.indexados = append(i.indexados, p.ID)
	return i.err
}

func (i *indiceFalso) Quitar(_ context.Context, id string, _ int) error {
	i.quitados = append(i.quitados, id)
	return i.err
}

func (i *indiceFalso) Reconstruir(_ context.Context, p []modelo.Producto) error {
	i.cargados = p
	return i.err
}

func (i *indiceFalso) AsegurarIndice(context.Context) (bool, error) { return i.creado, i.err }

func repoConProductos() *repoBusqueda {
	return &repoBusqueda{productos: map[string]modelo.Producto{
		"a":            {ID: "a", Publicado: true, Version: 2, PrecioBase: modelo.Precio{Monto: 100}},
		"b":            {ID: "b", Publicado: true, Version: 1, PrecioBase: modelo.Precio{Monto: 200}},
		"despublicado": {ID: "despublicado", Publicado: false, Version: 3},
	}}
}

func filtroBusqueda(texto string) modelo.FiltroProductos {
	return modelo.FiltroProductos{Texto: texto, Orden: modelo.OrdenRelevancia, Pagina: 1, Tamano: 10}
}

func TestBusquedaRespetaElOrdenDelIndiceYLeeDeMongo(t *testing.T) {
	repo := repoConProductos()
	s := Nuevo(repo, ConIndice(&indiceFalso{ids: []string{"b", "despublicado", "no-existe", "a"}, total: 4}))
	productos, total, err := s.ListarTienda(context.Background(), filtroBusqueda("remera"))
	if err != nil {
		t.Fatal(err)
	}
	if total != 4 || len(productos) != 2 || productos[0].Producto.ID != "b" || productos[1].Producto.ID != "a" {
		t.Fatalf("%d resultados, total %d: %+v", len(productos), total, productos)
	}
	if productos[0].Precio != 200 {
		t.Fatalf("el precio sale de la fuente: %d", productos[0].Precio)
	}
}

func TestSinTextoNavegaPorMongoAunqueElMotorEsteCaido(t *testing.T) {
	repo := repoConProductos()
	s := Nuevo(repo, ConIndice(&indiceFalso{err: errors.New("caído")}))
	if _, _, err := s.ListarTienda(context.Background(), filtroBusqueda("")); err != nil {
		t.Fatalf("navegar no depende del motor: %v", err)
	}
	if repo.listados != 1 {
		t.Fatal("tenía que leer de MongoDB")
	}
}

func TestConTextoYMotorCaidoInformaNoDisponible(t *testing.T) {
	repo := repoConProductos()
	s := Nuevo(repo, ConIndice(&indiceFalso{err: errors.New("caído")}))
	if _, _, err := s.ListarTienda(context.Background(), filtroBusqueda("remera")); !errors.Is(err, modelo.ErrBusquedaNoDisponible) {
		t.Fatalf("error: %v", err)
	}
	if repo.listados != 0 {
		t.Fatal("no tenía que caer a MongoDB")
	}
}

func TestSinIndiceBuscaEnMongo(t *testing.T) {
	repo := repoConProductos()
	if _, _, err := Nuevo(repo).ListarTienda(context.Background(), filtroBusqueda("remera")); err != nil || repo.listados != 1 {
		t.Fatalf("listados %d, error %v", repo.listados, err)
	}
}

// El filtro se valida también por el camino del motor.
func TestBusquedaValidaElFiltro(t *testing.T) {
	s := Nuevo(repoConProductos(), ConIndice(&indiceFalso{}))
	f := filtroBusqueda("remera")
	f.Tamano = 0
	var validacion *modelo.ErrorValidacion
	if _, _, err := s.ListarTienda(context.Background(), f); !errors.As(err, &validacion) {
		t.Fatalf("error: %v", err)
	}
}

func TestIndexarUsaElEstadoActualDelProducto(t *testing.T) {
	indice := &indiceFalso{}
	s := Nuevo(repoConProductos(), ConIndice(indice))
	for _, id := range []string{"a", "despublicado", "no-existe"} {
		if err := s.IndexarProducto(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
	if len(indice.indexados) != 1 || indice.indexados[0] != "a" {
		t.Errorf("indexados: %v", indice.indexados)
	}
	if len(indice.quitados) != 1 || indice.quitados[0] != "despublicado" {
		t.Errorf("quitados: %v", indice.quitados)
	}
}

func TestPublicarMarcaSoloLoQueSeAviso(t *testing.T) {
	repo := repoConProductos()
	repo.pendientes = []modelo.CambioProducto{{ID: "a", Version: 2}, {ID: "b", Version: 1}}
	s := Nuevo(repo)
	falla := errors.New("broker caído")
	publicados, err := s.PublicarCambios(context.Background(), 10, func(_ context.Context, id string, _ int) error {
		if id == "b" {
			return falla
		}
		return nil
	})
	if !errors.Is(err, falla) || publicados != 1 {
		t.Fatalf("publicados %d, error %v", publicados, err)
	}
	if len(repo.marcados) != 1 || repo.marcados[0].ID != "a" {
		t.Fatalf("lo no avisado tiene que quedar pendiente: %v", repo.marcados)
	}
}

func TestPrepararCargaSoloSiCreoElIndice(t *testing.T) {
	for _, creado := range []bool{true, false} {
		indice := &indiceFalso{creado: creado}
		if err := Nuevo(repoConProductos(), ConIndice(indice)).PrepararIndice(context.Background()); err != nil {
			t.Fatal(err)
		}
		if (indice.cargados != nil) != creado {
			t.Errorf("creado %v, cargados %v", creado, indice.cargados)
		}
	}
}

// Lo que cambió durante la carga se vuelve a aplicar, para que no se pierda al cambiar el alias.
func TestReconstruirReaplicaLoQueCambioDuranteLaCarga(t *testing.T) {
	indice := &indiceFalso{}
	if err := Nuevo(repoConProductos(), ConIndice(indice)).ReconstruirIndice(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(indice.cargados) != 1 || len(indice.quitados) != 1 || indice.quitados[0] != "despublicado" {
		t.Fatalf("cargados %v, quitados %v", indice.cargados, indice.quitados)
	}
}

func TestReconstruirSinIndiceNoEstaDisponible(t *testing.T) {
	if _, err := Nuevo(repoConProductos()).IniciarReconstruccion(); !errors.Is(err, modelo.ErrBusquedaNoDisponible) {
		t.Fatalf("error: %v", err)
	}
}
