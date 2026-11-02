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
type Repositorio interface {
	Ping(ctx context.Context) error
	ObtenerProducto(ctx context.Context, id string) (modelo.Producto, error)
	ExisteCategoria(ctx context.Context, id string) (bool, error)
	InsertarProducto(ctx context.Context, p modelo.Producto) error
}

type Servicio struct {
	repo Repositorio
}

func Nuevo(repo Repositorio) *Servicio {
	return &Servicio{repo: repo}
}

// Listo indica si el servicio puede atender pedidos (sonda /readyz).
func (s *Servicio) Listo(ctx context.Context) error {
	return s.repo.Ping(ctx)
}

func (s *Servicio) ObtenerProducto(ctx context.Context, id string) (modelo.Producto, error) {
	return s.repo.ObtenerProducto(ctx, id)
}

// NuevoProducto son los datos que manda el administrador. No trae ids: los genera el servidor (D12).
type NuevoProducto struct {
	Nombre        string
	Descripcion   string
	Categoria     string
	SistemaTalles string
	PrecioBase    modelo.Precio // en centavos
	Atributos     map[string]any
	Fotos         []modelo.Foto
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
	n = limpiar(n)
	if err := s.validar(ctx, n); err != nil {
		return modelo.Producto{}, err
	}

	ahora := time.Now().UTC().Truncate(time.Millisecond) // Mongo guarda las fechas en milisegundos
	p := modelo.Producto{
		Nombre:        n.Nombre,
		Descripcion:   n.Descripcion,
		Categoria:     n.Categoria,
		SistemaTalles: n.SistemaTalles,
		PrecioBase:    n.PrecioBase,
		Publicado:     false,
		Atributos:     n.Atributos,
		Fotos:         n.Fotos,
		CreadoEn:      ahora,
		ActualizadoEn: ahora,
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

func limpiar(n NuevoProducto) NuevoProducto {
	n.Nombre = strings.TrimSpace(n.Nombre)
	n.Descripcion = strings.TrimSpace(n.Descripcion)
	n.Categoria = strings.TrimSpace(n.Categoria)
	if n.PrecioBase.Moneda == "" {
		n.PrecioBase.Moneda = "ARS"
	}
	for i := range n.Variantes {
		n.Variantes[i].Talle = strings.ToUpper(strings.TrimSpace(n.Variantes[i].Talle))
		n.Variantes[i].Color = strings.TrimSpace(n.Variantes[i].Color)
	}
	return n
}

// validar junta todos los problemas y los devuelve juntos (PROYECTO.md 5.6).
func (s *Servicio) validar(ctx context.Context, n NuevoProducto) error {
	v := &modelo.ErrorValidacion{}

	if n.Nombre == "" {
		v.Agregar("nombre", "es obligatorio")
	} else if modelo.Normalizar(n.Nombre) == "" {
		v.Agregar("nombre", "tiene que incluir al menos una letra o un número")
	}

	if n.Categoria == "" {
		v.Agregar("categoria", "es obligatoria")
	} else if existe, err := s.repo.ExisteCategoria(ctx, n.Categoria); err != nil {
		return err
	} else if !existe {
		v.Agregar("categoria", "no existe la categoría %q (RN-17)", n.Categoria)
	}

	talles, sistemaValido := modelo.SistemasTalles[n.SistemaTalles]
	if !sistemaValido {
		v.Agregar("sistemaTalles", "tiene que ser LETRAS, NUMERICO_PAR, NUMERICO_1_4 o UNICO (RN-02)")
	}

	if n.PrecioBase.Monto <= 0 {
		v.Agregar("precioBase.monto", "tiene que ser mayor a 0")
	}
	if n.PrecioBase.Moneda != "ARS" {
		v.Agregar("precioBase.moneda", "la única moneda es ARS")
	}

	if len(n.Variantes) == 0 {
		v.Agregar("variantes", "tiene que haber al menos una (RN-01)")
	}
	// Dos variantes con el mismo talle y color normalizado tendrían el mismo id (sección 14, punto 12).
	vistas := map[string]int{}
	for i, nv := range n.Variantes {
		campo := fmt.Sprintf("variantes[%d]", i)
		if nv.Color == "" || modelo.Normalizar(nv.Color) == "" {
			v.Agregar(campo+".color", "es obligatorio")
		}
		if sistemaValido && !slices.Contains(talles, nv.Talle) {
			v.Agregar(campo+".talle", "%q no pertenece al sistema %s (RN-02)", nv.Talle, n.SistemaTalles)
		}
		clave := nv.Talle + "|" + modelo.Normalizar(nv.Color)
		if anterior, repetida := vistas[clave]; repetida {
			v.Agregar(campo, "repite talle y color de variantes[%d] (HU-03.4)", anterior)
		} else {
			vistas[clave] = i
		}
	}

	for i, f := range n.Fotos {
		if strings.TrimSpace(f.URL) == "" {
			v.Agregar(fmt.Sprintf("fotos[%d].url", i), "es obligatoria")
		}
	}
	return v.ComoError()
}
