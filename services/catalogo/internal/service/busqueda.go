package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
)

// Indice es lo que el servicio necesita del motor de búsqueda (ADR-006). Lo implementa busqueda.Cliente.
type Indice interface {
	// AsegurarIndice lo crea si no existe. Devuelve true solo a quien lo creó.
	AsegurarIndice(ctx context.Context) (bool, error)
	// Buscar devuelve los ids de la página, en orden, y el total.
	Buscar(ctx context.Context, f modelo.FiltroProductos) ([]string, int64, error)
	// Indexar y Quitar no aplican una versión igual o menor a la guardada.
	Indexar(ctx context.Context, p modelo.Producto) error
	Quitar(ctx context.Context, id string, version int) error
	// Reconstruir arma un índice nuevo y cambia el alias al terminar.
	Reconstruir(ctx context.Context, productos []modelo.Producto) error
}

// Opcion configura el servicio al crearlo.
type Opcion func(*Servicio)

// ConIndice hace que la búsqueda de la tienda use el motor. Sin índice, usa el texto de MongoDB (plan B de ADR-006).
func ConIndice(i Indice) Opcion {
	return func(s *Servicio) { s.indice = i }
}

// margenReconstruccion cubre la diferencia de reloj entre réplicas al buscar lo que cambió durante la carga.
const margenReconstruccion = 5 * time.Second

// buscarEnTienda busca en el índice y lee los productos de MongoDB: el índice dice cuáles y en qué orden, pero
// lo que se muestra sale de la fuente. Lo que se despublicó y todavía no salió del índice no se muestra (ADR-006).
func (s *Servicio) buscarEnTienda(ctx context.Context, f modelo.FiltroProductos) ([]modelo.Producto, int64, error) {
	ids, total, err := s.indice.Buscar(ctx, f)
	if err != nil {
		var validacion *modelo.ErrorValidacion
		if errors.As(err, &validacion) {
			return nil, 0, err
		}
		slog.WarnContext(ctx, "búsqueda no disponible", "error", err)
		return nil, 0, fmt.Errorf("%w: %v", modelo.ErrBusquedaNoDisponible, err)
	}
	if len(ids) == 0 {
		return []modelo.Producto{}, total, nil
	}
	encontrados, err := s.repo.ProductosPorIDs(ctx, ids)
	if err != nil {
		return nil, 0, err
	}
	porID := make(map[string]modelo.Producto, len(encontrados))
	for _, p := range encontrados {
		porID[p.ID] = p
	}
	productos := make([]modelo.Producto, 0, len(ids))
	for _, id := range ids {
		if p, ok := porID[id]; ok && p.Publicado {
			productos = append(productos, p)
		}
	}
	return productos, total, nil
}

// PrepararIndice crea el índice si no existe y, si lo creó esta réplica, lo carga desde MongoDB. También cubre
// el caso de un índice perdido con los productos ya marcados como publicados.
func (s *Servicio) PrepararIndice(ctx context.Context) error {
	creado, err := s.indice.AsegurarIndice(ctx)
	if err != nil || !creado {
		return err
	}
	slog.InfoContext(ctx, "índice de búsqueda nuevo: se carga desde MongoDB")
	return s.ReconstruirIndice(ctx)
}

// IndexarProducto aplica al índice el estado actual del producto. Por eso un evento viejo no puede revivir un
// producto despublicado: nunca se usa el contenido del evento.
func (s *Servicio) IndexarProducto(ctx context.Context, id string) error {
	p, err := s.repo.ObtenerProducto(ctx, id)
	if errors.Is(err, modelo.ErrProductoNoEncontrado) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.aplicarAlIndice(ctx, p)
}

func (s *Servicio) aplicarAlIndice(ctx context.Context, p modelo.Producto) error {
	if p.Publicado {
		if err := s.indice.Indexar(ctx, p); err != nil {
			return err
		}
	} else if err := s.indice.Quitar(ctx, p.ID, p.Version); err != nil {
		return err
	}
	slog.InfoContext(ctx, "índice actualizado", "producto_id", p.ID, "version", p.Version, "publicado", p.Publicado,
		"retraso_ms", time.Since(p.ActualizadoEn).Milliseconds())
	return nil
}

// ReconstruirIndice carga desde MongoDB un índice nuevo. Los cambios que entran durante la carga van al índice
// anterior, así que al terminar se vuelven a aplicar los productos modificados desde el inicio.
func (s *Servicio) ReconstruirIndice(ctx context.Context) error {
	if s.indice == nil {
		return modelo.ErrBusquedaNoDisponible
	}
	inicio := ahora().Add(-margenReconstruccion)
	productos, err := s.repo.ProductosPublicados(ctx)
	if err != nil {
		return err
	}
	if err := s.indice.Reconstruir(ctx, productos); err != nil {
		return err
	}
	cambiados, err := s.repo.ProductosModificadosDesde(ctx, inicio)
	if err != nil {
		return err
	}
	for _, p := range cambiados {
		if err := s.aplicarAlIndice(ctx, p); err != nil {
			return err
		}
	}
	slog.InfoContext(ctx, "índice reconstruido", "productos", len(productos), "reaplicados", len(cambiados))
	return nil
}

// IniciarReconstruccion la lanza en segundo plano, porque puede tardar más que el límite de un pedido.
// Devuelve false si esta réplica ya está reconstruyendo.
func (s *Servicio) IniciarReconstruccion() (bool, error) {
	if s.indice == nil {
		return false, modelo.ErrBusquedaNoDisponible
	}
	if !s.reconstruyendo.CompareAndSwap(false, true) {
		return false, nil
	}
	go func() {
		defer s.reconstruyendo.Store(false)
		ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancelar()
		if err := s.ReconstruirIndice(ctx); err != nil {
			slog.ErrorContext(ctx, "no se pudo reconstruir el índice", "error", err)
		}
	}()
	return true, nil
}

// PublicarCambios es el outbox de Catálogo: busca productos con una versión sin publicar, llama a publicar y
// recién entonces la marca. Si el proceso se cae en el medio, el evento se publica de nuevo (al menos una vez).
func (s *Servicio) PublicarCambios(ctx context.Context, limite int, publicar func(ctx context.Context, id string, version int) error) (int, error) {
	pendientes, err := s.repo.ProductosSinPublicar(ctx, limite)
	if err != nil {
		return 0, err
	}
	for i, c := range pendientes {
		if err := publicar(ctx, c.ID, c.Version); err != nil {
			return i, err
		}
		if err := s.repo.MarcarPublicado(ctx, c.ID, c.Version); err != nil {
			return i, err
		}
	}
	return len(pendientes), nil
}
