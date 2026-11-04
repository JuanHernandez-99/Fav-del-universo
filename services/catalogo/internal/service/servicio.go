// Package service tiene las reglas del catálogo. No conoce HTTP ni MongoDB: recibe el repositorio
// como interfaz, así se puede reemplazar (por una caché, o por uno falso en los tests).
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

// Repositorio es lo que el servicio necesita de la base. Lo implementa repository.Repositorio.
// Las operaciones que dependen de una condición la resuelven en la base, en la misma escritura.
type Repositorio interface {
	Ping(ctx context.Context) error
	ObtenerProducto(ctx context.Context, id string) (modelo.Producto, error)
	ExisteCategoria(ctx context.Context, id string) (bool, error)
	InsertarProducto(ctx context.Context, p modelo.Producto) error
	// AgregarVariante agrega la variante solo si el producto no tiene una con el mismo talle y
	// color (ni el mismo id), en una operación atómica. Si ya la tiene: modelo.ErrVarianteDuplicada.
	AgregarVariante(ctx context.Context, productoID string, v modelo.Variante, ahora time.Time) error
	// ActualizarProducto aplica los cambios solo si la versión guardada es la indicada.
	// Si otro cambio se coló: modelo.ErrConflictoDeVersion.
	ActualizarProducto(ctx context.Context, id string, version int, d modelo.DatosProducto, ahora time.Time) (modelo.Producto, error)
	// Publicar y Despublicar solo cambian (y suben la versión) si el estado era el otro.
	Publicar(ctx context.Context, id string, ahora time.Time) (modelo.Producto, error)
	Despublicar(ctx context.Context, id string, ahora time.Time) error
	// ListarProductos devuelve la página pedida y el total que cumple el filtro.
	ListarProductos(ctx context.Context, f modelo.FiltroProductos) ([]modelo.Producto, int64, error)
	// ProductosPublicados devuelve todos los productos publicados, sin paginar (ADR-008).
	ProductosPublicados(ctx context.Context) ([]modelo.Producto, error)
	// ObtenerVariantes devuelve las variantes que existan entre esos ids, con los datos de su
	// producto y sin filtrar por publicado. Sin orden garantizado.
	ObtenerVariantes(ctx context.Context, ids []string) ([]modelo.DetalleVariante, error)
	// PromocionesVigentes devuelve las promociones vigentes en ese momento, de cualquier alcance y
	// medio de pago. A qué producto se aplica cada una lo decide PrecioVigente.
	PromocionesVigentes(ctx context.Context, ahora time.Time) ([]modelo.Promocion, error)
}

type Servicio struct {
	repo Repositorio
}

func Nuevo(repo Repositorio) *Servicio {
	return &Servicio{repo: repo}
}

// ahora en UTC y en milisegundos, que es la precisión con que Mongo guarda las fechas.
func ahora() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

// Listo indica si el servicio puede atender pedidos (sonda /readyz).
func (s *Servicio) Listo(ctx context.Context) error {
	return s.repo.Ping(ctx)
}

func (s *Servicio) ObtenerProducto(ctx context.Context, id string) (modelo.Producto, error) {
	return s.repo.ObtenerProducto(ctx, id)
}

// ListarProductos valida el rango de la paginación, el orden y el rango de precios. El formato
// de cada parámetro ya lo revisó el handler.
func (s *Servicio) ListarProductos(ctx context.Context, f modelo.FiltroProductos) ([]modelo.Producto, int64, error) {
	v := &modelo.ErrorValidacion{}
	if f.Pagina < 1 || f.Pagina > modelo.PaginaMaxima {
		v.Agregar("pagina", "tiene que estar entre 1 y %d", modelo.PaginaMaxima)
	}
	if f.Tamano < 1 || f.Tamano > modelo.TamanoMaximo {
		v.Agregar("tamano", "tiene que estar entre 1 y %d", modelo.TamanoMaximo)
	}
	if !slices.Contains(modelo.Ordenes, f.Orden) {
		v.Agregar("orden", "tiene que ser uno de: %v", modelo.Ordenes)
	}
	if f.PrecioMin != nil && f.PrecioMax != nil && *f.PrecioMin > *f.PrecioMax {
		v.Agregar("precioMin", "no puede ser mayor que precioMax")
	}
	if err := v.ComoError(); err != nil {
		return nil, 0, err
	}
	return s.repo.ListarProductos(ctx, f)
}

// ---------- Crear ----------

// NuevoProducto son los datos que manda el administrador. No trae ids: los genera el servidor (D12).
type NuevoProducto struct {
	modelo.DatosProducto
	SistemaTalles string
	Variantes     []NuevaVariante
}

type NuevaVariante struct {
	Talle string
	Color string
}

// intentosDeID limita los sufijos -2, -3… si ya hay productos con el mismo nombre.
const intentosDeID = 50

// CrearProducto valida, genera los ids y guarda. El producto nace no publicado (HU-03.1).
func (s *Servicio) CrearProducto(ctx context.Context, n NuevoProducto) (modelo.Producto, error) {
	n.DatosProducto = limpiarDatos(n.DatosProducto)
	for i := range n.Variantes {
		n.Variantes[i] = limpiarVariante(n.Variantes[i])
	}
	if err := s.validarNuevo(ctx, n); err != nil {
		return modelo.Producto{}, err
	}

	t := ahora()
	p := modelo.Producto{
		Nombre:        n.Nombre,
		Descripcion:   n.Descripcion,
		Categoria:     n.Categoria,
		SistemaTalles: n.SistemaTalles,
		PrecioBase:    n.PrecioBase,
		Publicado:     false,
		Atributos:     n.Atributos,
		Fotos:         n.Fotos,
		CreadoEn:      t,
		ActualizadoEn: t,
		Version:       1,
	}

	// Si el id ya existe, la base lo rechaza y se prueba con el siguiente sufijo. Los ids de las
	// variantes dependen del id del producto, por eso se recalculan en cada intento.
	for intento := 1; intento <= intentosDeID; intento++ {
		p.ID = modelo.IDProducto(n.Nombre, intento)
		variantes, err := armarVariantes(p.ID, n.Variantes)
		if err != nil {
			v := &modelo.ErrorValidacion{}
			v.Agregar("nombre", "es demasiado largo: %v", err)
			return modelo.Producto{}, v
		}
		p.Variantes = variantes

		err = s.repo.InsertarProducto(ctx, p)
		if errors.Is(err, modelo.ErrIDProductoEnUso) {
			continue
		}
		if err != nil {
			return modelo.Producto{}, err
		}
		return p, nil
	}
	return modelo.Producto{}, fmt.Errorf("no se encontró un id libre para %q en %d intentos", n.Nombre, intentosDeID)
}

func armarVariantes(productoID string, nuevas []NuevaVariante) ([]modelo.Variante, error) {
	variantes := make([]modelo.Variante, len(nuevas))
	for i, nv := range nuevas {
		id, err := modelo.IDVariante(productoID, nv.Color, nv.Talle)
		if err != nil {
			return nil, err
		}
		variantes[i] = modelo.Variante{ID: id, Talle: nv.Talle, Color: nv.Color}
	}
	return variantes, nil
}

// ---------- Variantes ----------

// AgregarVariante suma una variante a un producto existente (DISENO.md 5.9). Lee el producto
// solo para validar el talle contra su sistema; la regla de no duplicar la decide la base, en la
// misma operación que escribe.
func (s *Servicio) AgregarVariante(ctx context.Context, productoID string, nv NuevaVariante) (modelo.Variante, error) {
	nv = limpiarVariante(nv)
	p, err := s.repo.ObtenerProducto(ctx, productoID)
	if err != nil {
		return modelo.Variante{}, err
	}

	v := &modelo.ErrorValidacion{}
	validarVariante(v, "", nv, p.SistemaTalles)
	if err := v.ComoError(); err != nil {
		return modelo.Variante{}, err
	}
	id, err := modelo.IDVariante(p.ID, nv.Color, nv.Talle)
	if err != nil {
		v.Agregar("color", "es demasiado largo: %v", err)
		return modelo.Variante{}, v
	}

	variante := modelo.Variante{ID: id, Talle: nv.Talle, Color: nv.Color}
	if err := s.repo.AgregarVariante(ctx, p.ID, variante, ahora()); err != nil {
		return modelo.Variante{}, err
	}
	return variante, nil
}

// ---------- Actualizar, publicar y despublicar ----------

// ActualizarProducto reemplaza los datos editables. version es la que el administrador leyó: si
// el producto cambió desde entonces, no se pisa (409). El id no cambia aunque cambie el nombre (5.5).
func (s *Servicio) ActualizarProducto(ctx context.Context, id string, version int, d modelo.DatosProducto) (modelo.Producto, error) {
	d = limpiarDatos(d)
	v := &modelo.ErrorValidacion{}
	if version < 1 {
		v.Agregar("version", "es obligatoria: la que se leyó del producto")
	}
	if err := s.validarDatos(ctx, v, d); err != nil {
		return modelo.Producto{}, err
	}
	if err := v.ComoError(); err != nil {
		return modelo.Producto{}, err
	}
	return s.repo.ActualizarProducto(ctx, id, version, d, ahora())
}

// Publicar habilita el producto en la web (RN-04). Publicar algo ya publicado no es un error.
func (s *Servicio) Publicar(ctx context.Context, id string) (modelo.Producto, error) {
	return s.repo.Publicar(ctx, id, ahora())
}

// Despublicar es la baja lógica (D2): el producto no se borra y su id no se reutiliza.
func (s *Servicio) Despublicar(ctx context.Context, id string) error {
	return s.repo.Despublicar(ctx, id, ahora())
}

// ---------- Limpieza y validación ----------

func limpiarDatos(d modelo.DatosProducto) modelo.DatosProducto {
	d.Nombre = strings.TrimSpace(d.Nombre)
	d.Descripcion = strings.TrimSpace(d.Descripcion)
	d.Categoria = strings.TrimSpace(d.Categoria)
	if d.PrecioBase.Moneda == "" {
		d.PrecioBase.Moneda = "ARS"
	}
	return d
}

// limpiarVariante quita espacios y pasa el talle a mayúscula (sección 14, punto 16).
func limpiarVariante(nv NuevaVariante) NuevaVariante {
	return NuevaVariante{Talle: strings.ToUpper(strings.TrimSpace(nv.Talle)), Color: strings.TrimSpace(nv.Color)}
}

// validarNuevo junta todos los problemas y los devuelve juntos (DISENO.md 5.6).
func (s *Servicio) validarNuevo(ctx context.Context, n NuevoProducto) error {
	v := &modelo.ErrorValidacion{}
	if err := s.validarDatos(ctx, v, n.DatosProducto); err != nil {
		return err
	}

	if _, sistemaValido := modelo.SistemasTalles[n.SistemaTalles]; !sistemaValido {
		v.Agregar("sistemaTalles", "tiene que ser LETRAS, NUMERICO_PAR, NUMERICO_1_4 o UNICO (RN-02)")
	}
	if len(n.Variantes) == 0 {
		v.Agregar("variantes", "tiene que haber al menos una (RN-01)")
	}
	// Dos variantes con el mismo talle y color normalizado tendrían el mismo id (sección 14, punto 12).
	vistas := map[string]int{}
	for i, nv := range n.Variantes {
		campo := fmt.Sprintf("variantes[%d]", i)
		validarVariante(v, campo+".", nv, n.SistemaTalles)
		clave := nv.Talle + "|" + modelo.Normalizar(nv.Color)
		if anterior, repetida := vistas[clave]; repetida {
			v.Agregar(campo, "repite talle y color de variantes[%d] (HU-03.4)", anterior)
		} else {
			vistas[clave] = i
		}
	}
	return v.ComoError()
}

// validarDatos agrega a v los problemas de los datos editables. Solo devuelve error si falló la
// consulta a la base.
func (s *Servicio) validarDatos(ctx context.Context, v *modelo.ErrorValidacion, d modelo.DatosProducto) error {
	if d.Nombre == "" {
		v.Agregar("nombre", "es obligatorio")
	} else if modelo.Normalizar(d.Nombre) == "" {
		v.Agregar("nombre", "tiene que incluir al menos una letra o un número")
	}

	if d.Categoria == "" {
		v.Agregar("categoria", "es obligatoria")
	} else if existe, err := s.repo.ExisteCategoria(ctx, d.Categoria); err != nil {
		return err
	} else if !existe {
		v.Agregar("categoria", "no existe la categoría %q (RN-17)", d.Categoria)
	}

	if d.PrecioBase.Monto <= 0 {
		v.Agregar("precioBase.monto", "tiene que ser mayor a 0")
	}
	if d.PrecioBase.Moneda != "ARS" {
		v.Agregar("precioBase.moneda", "la única moneda es ARS")
	}

	for i, f := range d.Fotos {
		if strings.TrimSpace(f.URL) == "" {
			v.Agregar(fmt.Sprintf("fotos[%d].url", i), "es obligatoria")
		}
	}
	return nil
}

// validarVariante revisa color obligatorio y talle del sistema (RN-02). prefijo es la ruta del
// campo en el pedido, por ejemplo "variantes[2]."; vacío cuando el pedido es la variante sola.
func validarVariante(v *modelo.ErrorValidacion, prefijo string, nv NuevaVariante, sistema string) {
	if nv.Color == "" || modelo.Normalizar(nv.Color) == "" {
		v.Agregar(prefijo+"color", "es obligatorio")
	}
	if talles, ok := modelo.SistemasTalles[sistema]; ok && !slices.Contains(talles, nv.Talle) {
		v.Agregar(prefijo+"talle", "%q no pertenece al sistema %s (RN-02)", nv.Talle, sistema)
	}
}
