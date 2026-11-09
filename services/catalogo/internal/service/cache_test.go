package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
)

type cacheFalsa struct {
	mu         sync.Mutex
	datos      map[string][]byte
	generacion int64
	consultas  int
	err        error
}

func nuevaCache() *cacheFalsa { return &cacheFalsa{datos: map[string][]byte{}} }

func (c *cacheFalsa) Generacion(context.Context) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consultas++
	return c.generacion, c.err
}

func (c *cacheFalsa) SubirGeneracion(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil {
		c.generacion++
	}
	return c.err
}

func (c *cacheFalsa) Leer(_ context.Context, clave string) ([]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.datos[clave]
	return v, ok, c.err
}

func (c *cacheFalsa) Guardar(_ context.Context, clave string, valor []byte, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err == nil {
		c.datos[clave] = valor
	}
	return c.err
}

type repoCache struct {
	Repositorio
	producto    modelo.Producto
	promociones []modelo.Promocion
	demora      time.Duration
	lecturas    atomic.Int64
	vigentes    atomic.Int64
	listados    atomic.Int64
	err         error
}

func (r *repoCache) ObtenerProducto(_ context.Context, id string) (modelo.Producto, error) {
	r.lecturas.Add(1)
	time.Sleep(r.demora)
	if r.err != nil {
		return modelo.Producto{}, r.err
	}
	if id != r.producto.ID {
		return modelo.Producto{}, modelo.ErrProductoNoEncontrado
	}
	return r.producto, nil
}

func (r *repoCache) ListarProductos(context.Context, modelo.FiltroProductos) ([]modelo.Producto, int64, error) {
	r.listados.Add(1)
	return []modelo.Producto{r.producto}, 1, r.err
}

func (r *repoCache) ListarPromociones(context.Context) ([]modelo.Promocion, error) {
	return r.promociones, nil
}

func (r *repoCache) PromocionesVigentes(context.Context, time.Time) ([]modelo.Promocion, error) {
	r.vigentes.Add(1)
	return r.promociones, nil
}

func (r *repoCache) Publicar(context.Context, string, time.Time) (modelo.Producto, error) {
	return r.producto, nil
}

func (r *repoCache) Despublicar(context.Context, string, time.Time) error { return nil }

func remera() modelo.Producto {
	return modelo.Producto{ID: "remera", Nombre: "Remera básica", Categoria: "remeras", Publicado: true, Version: 1, PrecioBase: modelo.Precio{Monto: 1500000, Moneda: "ARS"}}
}

func TestLaSegundaLecturaNoVaAMongo(t *testing.T) {
	repo := &repoCache{producto: remera()}
	s := Nuevo(repo, ConCache(nuevaCache()))
	for range 3 {
		p, err := s.ObtenerDeTienda(context.Background(), "remera")
		if err != nil || p.Producto.Nombre != "Remera básica" {
			t.Fatalf("%+v, %v", p, err)
		}
	}
	if n := repo.lecturas.Load(); n != 1 {
		t.Fatalf("lecturas a MongoDB: %d", n)
	}
}

func TestInvalidarHaceLeerElDatoNuevo(t *testing.T) {
	repo := &repoCache{producto: remera()}
	s := Nuevo(repo, ConCache(nuevaCache()))
	ctx := context.Background()
	if _, err := s.ObtenerDeTienda(ctx, "remera"); err != nil {
		t.Fatal(err)
	}
	repo.producto.Nombre = "Remera nueva"
	if _, err := s.Publicar(ctx, "remera"); err != nil {
		t.Fatal(err)
	}
	p, err := s.ObtenerDeTienda(ctx, "remera")
	if err != nil || p.Producto.Nombre != "Remera nueva" {
		t.Fatalf("%+v, %v", p, err)
	}
}

// La carrera clásica: alguien lee el dato viejo, otro escribe e invalida, y el primero guarda lo viejo. Con la generación
// en la clave, lo viejo queda en una clave que ya nadie lee.
func TestUnaLecturaLentaNoDejaElDatoViejoEnLaCache(t *testing.T) {
	repo := &repoCache{producto: remera()}
	cache := nuevaCache()
	s := Nuevo(repo, ConCache(cache))
	ctx := context.Background()

	escritura := make(chan struct{})
	lista := make(chan struct{})
	_, err := leerConCache(ctx, s, "ficha", "producto:remera", func(context.Context) (modelo.Producto, error) {
		viejo := repo.producto
		go func() {
			<-lista
			repo.producto.Nombre = "Remera nueva"
			s.invalidarCache(ctx)
			close(escritura)
		}()
		close(lista)
		<-escritura
		return viejo, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.ObtenerDeTienda(ctx, "remera")
	if err != nil || p.Producto.Nombre != "Remera nueva" {
		t.Fatalf("quedó el dato viejo: %+v, %v", p, err)
	}
}

func TestLecturasSimultaneasHacenUnaSolaConsulta(t *testing.T) {
	repo := &repoCache{producto: remera(), demora: 100 * time.Millisecond}
	s := Nuevo(repo, ConCache(nuevaCache()))
	var espera sync.WaitGroup
	for range 50 {
		espera.Go(func() {
			if _, err := s.ObtenerDeTienda(context.Background(), "remera"); err != nil {
				t.Error(err)
			}
		})
	}
	espera.Wait()
	if n := repo.lecturas.Load(); n != 1 {
		t.Fatalf("lecturas a MongoDB: %d", n)
	}
}

func TestSinRedisRespondeDesdeMongoSinError(t *testing.T) {
	repo := &repoCache{producto: remera()}
	cache := nuevaCache()
	cache.err = errors.New("redis caído")
	s := Nuevo(repo, ConCache(cache))
	ctx := context.Background()
	if _, err := s.ObtenerDeTienda(ctx, "remera"); err != nil {
		t.Fatalf("Redis caído no puede ser un error para el comprador: %v", err)
	}
	if _, _, err := s.ListarTienda(ctx, modelo.FiltroProductos{Orden: modelo.OrdenNuevos, Pagina: 1, Tamano: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publicar(ctx, "remera"); err != nil {
		t.Fatalf("no poder invalidar no frena la escritura: %v", err)
	}
}

// Con Redis caído no se paga su tiempo de espera en cada lectura.
func TestTrasUnErrorNoSeVuelveAIntentarPorUnRato(t *testing.T) {
	repo := &repoCache{producto: remera()}
	cache := nuevaCache()
	cache.err = errors.New("redis caído")
	s := Nuevo(repo, ConCache(cache))
	ctx := context.Background()
	for range 5 {
		if _, err := s.ObtenerDeTienda(ctx, "remera"); err != nil {
			t.Fatal(err)
		}
	}
	if cache.consultas != 1 {
		t.Fatalf("se consultó a Redis %d veces", cache.consultas)
	}
	// Pasada la pausa, vuelve a usarla sola.
	cache.err = nil
	s.cachePausadaHasta.Store(0)
	if _, err := s.ObtenerDeTienda(ctx, "remera"); err != nil {
		t.Fatal(err)
	}
	if len(cache.datos) == 0 {
		t.Fatal("tenía que volver a guardar")
	}
}

func TestUnFalloDeLaFuenteNoSeGuarda(t *testing.T) {
	repo := &repoCache{producto: remera(), err: errors.New("mongo caído")}
	cache := nuevaCache()
	s := Nuevo(repo, ConCache(cache))
	if _, err := s.ObtenerDeTienda(context.Background(), "remera"); err == nil {
		t.Fatal("tenía que fallar")
	}
	if len(cache.datos) != 0 {
		t.Fatalf("se guardó un error: %v", cache.datos)
	}
}

func TestLoNoPublicadoNoSeGuarda(t *testing.T) {
	producto := remera()
	producto.Publicado = false
	repo := &repoCache{producto: producto}
	cache := nuevaCache()
	s := Nuevo(repo, ConCache(cache))
	if _, err := s.ObtenerDeTienda(context.Background(), "remera"); !errors.Is(err, modelo.ErrProductoNoEncontrado) {
		t.Fatalf("error: %v", err)
	}
	if len(cache.datos) != 0 {
		t.Fatal("un no publicado no se guarda: publicarlo tiene que verse enseguida")
	}
}

func TestUnaEntradaDanadaSeReemplaza(t *testing.T) {
	repo := &repoCache{producto: remera()}
	cache := nuevaCache()
	cache.datos["catalogo:v1:g0:producto:remera"] = []byte("{")
	s := Nuevo(repo, ConCache(cache))
	if _, err := s.ObtenerDeTienda(context.Background(), "remera"); err != nil {
		t.Fatal(err)
	}
	if repo.lecturas.Load() != 1 || string(cache.datos["catalogo:v1:g0:producto:remera"]) == "{" {
		t.Fatal("tenía que leer la fuente y reemplazar la entrada")
	}
}

// La promoción está en la caché, pero la vigencia se evalúa al leer: empieza y termina en su hora exacta.
func TestLaPromocionRespetaSuVigenciaAunqueEsteEnCache(t *testing.T) {
	ahoraMismo := time.Now()
	repo := &repoCache{producto: remera(), promociones: []modelo.Promocion{
		{ID: "vigente", Vigencia: modelo.Vigencia{Desde: ahoraMismo.Add(-time.Hour), Hasta: ahoraMismo.Add(time.Hour)}},
		{ID: "vencida", Vigencia: modelo.Vigencia{Desde: ahoraMismo.Add(-2 * time.Hour), Hasta: ahoraMismo.Add(-time.Hour)}},
		{ID: "futura", Vigencia: modelo.Vigencia{Desde: ahoraMismo.Add(time.Hour), Hasta: ahoraMismo.Add(2 * time.Hour)}},
	}}
	s := Nuevo(repo, ConCache(nuevaCache()))
	vigentes, err := s.promocionesParaMostrar(context.Background(), ahoraMismo)
	if err != nil {
		t.Fatal(err)
	}
	if len(vigentes) != 1 || vigentes[0].ID != "vigente" {
		t.Fatalf("vigentes: %+v", vigentes)
	}
	mas := ahoraMismo.Add(90 * time.Minute)
	vigentes, _ = s.promocionesParaMostrar(context.Background(), mas)
	if len(vigentes) != 1 || vigentes[0].ID != "futura" {
		t.Fatalf("una hora y media después: %+v", vigentes)
	}
}

// El precio con el que se cobra no puede salir de la caché.
func TestLaConsultaDeVentasNoUsaLaCache(t *testing.T) {
	repo := &repoVentasCache{}
	cache := nuevaCache()
	cache.err = errors.New("no se tenía que usar")
	s := Nuevo(repo, ConCache(cache))
	if _, err := s.ConsultarVariantes(context.Background(), []string{"var-1"}, ""); err != nil {
		t.Fatal(err)
	}
	if repo.vigentes != 1 {
		t.Fatalf("las promociones tienen que leerse de MongoDB: %d", repo.vigentes)
	}
}

type repoVentasCache struct {
	Repositorio
	vigentes int
}

func (r *repoVentasCache) ObtenerVariantes(context.Context, []string) ([]modelo.DetalleVariante, error) {
	return []modelo.DetalleVariante{{VarianteID: "var-1", ProductoID: "p", PrecioBase: modelo.Precio{Monto: 100, Moneda: "ARS"}}}, nil
}

func (r *repoVentasCache) PromocionesVigentes(context.Context, time.Time) ([]modelo.Promocion, error) {
	r.vigentes++
	return nil, nil
}

func TestLaHuellaDistingueLosFiltros(t *testing.T) {
	precio := int64(100)
	base := modelo.FiltroProductos{Categoria: "remeras", Orden: modelo.OrdenNuevos, Pagina: 1, Tamano: 24}
	otros := []func(*modelo.FiltroProductos){
		func(f *modelo.FiltroProductos) { f.Pagina = 2 },
		func(f *modelo.FiltroProductos) { f.Categoria = "jeans" },
		func(f *modelo.FiltroProductos) { f.Talle = "M" },
		func(f *modelo.FiltroProductos) { f.Color = "Blanco" },
		func(f *modelo.FiltroProductos) { f.PrecioMin = &precio },
		func(f *modelo.FiltroProductos) { f.PrecioMax = &precio },
		func(f *modelo.FiltroProductos) { f.Orden = modelo.OrdenNombre },
		func(f *modelo.FiltroProductos) { f.Tamano = 12 },
	}
	vistas := map[string]bool{huellaFiltro(base): true}
	for i, cambio := range otros {
		f := base
		cambio(&f)
		h := huellaFiltro(f)
		if vistas[h] {
			t.Errorf("el cambio %d da la misma clave", i)
		}
		vistas[h] = true
	}
}
