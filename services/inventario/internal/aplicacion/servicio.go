// Package aplicacion tiene los casos de uso de Inventario. Coordina el dominio y los puertos;
// no conoce HTTP, MySQL ni el gimnasio.
package aplicacion

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/dominio"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/puertos"
)

type Servicio struct {
	repo     puertos.Repositorio
	gimnasio puertos.Gimnasio
	ahora    func() time.Time
	nuevoID  func() string
}

func NuevoServicio(repo puertos.Repositorio, gimnasio puertos.Gimnasio) *Servicio {
	return &Servicio{
		repo:     repo,
		gimnasio: gimnasio,
		ahora:    time.Now,
		nuevoID:  func() string { return uuid.NewString() },
	}
}

type PedidoReserva struct {
	ClaveIdempotencia string
	Referencia        string
	Items             []dominio.Item
	Vigencia          time.Duration
}

// Reservar aparta stock online, todo o nada. Un reintento con la misma clave y el mismo pedido
// devuelve la misma reserva; con otro pedido, ErrClaveReutilizada.
func (s *Servicio) Reservar(ctx context.Context, p PedidoReserva) (dominio.Reserva, bool, error) {
	nueva, err := dominio.NuevaReserva(s.nuevoID(), p.ClaveIdempotencia, p.Referencia, p.Items, p.Vigencia, s.ahora())
	if err != nil {
		return dominio.Reserva{}, false, err
	}
	guardada, repetida, err := s.repo.CrearReserva(ctx, nueva)
	if err != nil {
		return dominio.Reserva{}, false, err
	}
	if repetida && guardada.Huella != nueva.Huella {
		return dominio.Reserva{}, false, dominio.ErrClaveReutilizada
	}
	if repetida {
		slog.InfoContext(ctx, "reserva repetida, se devuelve la existente", "reserva_id", guardada.ID)
	} else {
		slog.InfoContext(ctx, "reserva creada", "reserva_id", guardada.ID, "items", len(guardada.Items), "vence_en", guardada.VenceEn)
	}
	return guardada, repetida, nil
}

func (s *Servicio) ObtenerReserva(ctx context.Context, id string) (dominio.Reserva, error) {
	return s.repo.ObtenerReserva(ctx, id)
}

func (s *Servicio) Confirmar(ctx context.Context, id string) (dominio.Reserva, error) {
	r, err := s.repo.ModificarReserva(ctx, id, func(r *dominio.Reserva) error { return r.Confirmar(s.ahora()) })
	s.registrar(ctx, "confirmar", r, err)
	return r, err
}

func (s *Servicio) Liberar(ctx context.Context, id string) (dominio.Reserva, error) {
	r, err := s.repo.ModificarReserva(ctx, id, func(r *dominio.Reserva) error { return r.Liberar(s.ahora()) })
	s.registrar(ctx, "liberar", r, err)
	return r, err
}

// ReservasPorVencer devuelve las reservas activas cuyo plazo ya pasó.
func (s *Servicio) ReservasPorVencer(ctx context.Context, limite int) ([]string, error) {
	return s.repo.ReservasVencidas(ctx, s.ahora(), limite)
}

// VencerReserva libera la reserva si su plazo pasó. Devuelve false si no hubo cambio,
// por ejemplo porque otra réplica la venció antes o porque se confirmó a tiempo.
func (s *Servicio) VencerReserva(ctx context.Context, id string) (bool, error) {
	cambio := false
	_, err := s.repo.ModificarReserva(ctx, id, func(r *dominio.Reserva) error {
		cambio = r.Vencer(s.ahora())
		return nil
	})
	if err == nil && cambio {
		slog.InfoContext(ctx, "reserva vencida", "reserva_id", id)
	}
	return cambio, err
}

func (s *Servicio) ConsultarStock(ctx context.Context, varianteIDs []string) ([]dominio.Stock, error) {
	return s.repo.ConsultarStock(ctx, varianteIDs)
}

// MaximoACorregir acota la lista de stock a corregir. Con 10 a 20 prendas por semana (SPEC §3) no se espera
// llegar: si se llega, la respuesta lo indica.
const MaximoACorregir = 500

// StockACorregir devuelve las variantes con físico negativo (HU-09.2) y si hay más que las devueltas.
func (s *Servicio) StockACorregir(ctx context.Context) ([]dominio.Stock, bool, error) {
	stocks, err := s.repo.StockACorregir(ctx, MaximoACorregir+1)
	if err != nil {
		return nil, false, err
	}
	if len(stocks) > MaximoACorregir {
		return stocks[:MaximoACorregir], true, nil
	}
	return stocks, false, nil
}

// ConsultarDisponibilidad responde por cada id pedido, en el mismo orden. Una variante sin fila de stock
// no tiene unidades: se responde 0.
func (s *Servicio) ConsultarDisponibilidad(ctx context.Context, varianteIDs []string) ([]dominio.Disponibilidad, error) {
	stocks, err := s.repo.ConsultarStock(ctx, varianteIDs)
	if err != nil {
		return nil, err
	}
	disponibles := make(map[string]int, len(stocks))
	for _, st := range stocks {
		disponibles[st.VarianteID] = max(st.DisponibleOnline(), 0)
	}
	resultado := make([]dominio.Disponibilidad, len(varianteIDs))
	for i, id := range varianteIDs {
		resultado[i] = dominio.Disponibilidad{VarianteID: id, Disponible: disponibles[id]}
	}
	return resultado, nil
}

func (s *Servicio) Ingresar(ctx context.Context, clave, varianteID string, canal dominio.Canal, cantidad int, usuario string) (dominio.Stock, error) {
	i, err := dominio.NuevoIngreso(clave, varianteID, canal, cantidad, usuario)
	if err != nil {
		return dominio.Stock{}, err
	}
	st, err := s.repo.Ingresar(ctx, i)
	if err == nil {
		slog.InfoContext(ctx, "ingreso de stock", "variante_id", varianteID, "canal", canal, "cantidad", cantidad)
	}
	return st, err
}

func (s *Servicio) Transferir(ctx context.Context, clave, varianteID string, origen dominio.Canal, cantidad int, usuario string) (dominio.Stock, error) {
	t, err := dominio.NuevaTransferencia(clave, varianteID, origen, cantidad, usuario)
	if err != nil {
		return dominio.Stock{}, err
	}
	st, err := s.repo.Transferir(ctx, t)
	if err == nil {
		slog.InfoContext(ctx, "transferencia de stock", "variante_id", varianteID, "origen", origen, "destino", t.Destino(), "cantidad", cantidad)
	}
	return st, err
}

// Ajustar corrige el stock de un canal con un motivo (HU-08).
func (s *Servicio) Ajustar(ctx context.Context, clave, varianteID string, canal dominio.Canal, cantidad int, motivo dominio.Motivo, nota, usuario string) (dominio.Stock, error) {
	a, err := dominio.NuevoAjuste(clave, varianteID, canal, cantidad, motivo, nota, usuario)
	if err != nil {
		return dominio.Stock{}, err
	}
	st, err := s.repo.Ajustar(ctx, a)
	if err == nil {
		slog.InfoContext(ctx, "ajuste de stock", "variante_id", varianteID, "canal", canal, "cantidad", cantidad, "motivo", motivo)
	}
	return st, err
}

// DescontarFisico aplica una venta del local sobre el stock físico (HU-17).
func (s *Servicio) DescontarFisico(ctx context.Context, clave, referencia string, items []dominio.Item, usuario string) ([]dominio.Stock, error) {
	d, err := dominio.NuevoDescuentoFisico(clave, referencia, items, usuario)
	if err != nil {
		return nil, err
	}
	stocks, err := s.repo.DescontarFisico(ctx, d)
	if err == nil {
		slog.InfoContext(ctx, "descuento de stock físico", "referencia", referencia, "items", len(d.Items))
	}
	return stocks, err
}

// PublicarEventos envía al broker los eventos pendientes del outbox.
func (s *Servicio) PublicarEventos(ctx context.Context, publicador puertos.PublicadorEventos, limite int) (int, error) {
	return s.repo.PublicarEventos(ctx, limite, publicador.Publicar)
}

// Reponer descuenta el stock físico y deja la reposición pendiente. El envío al gimnasio
// lo hace EnviarReposiciones, para que la caída del gimnasio no frene a FAV (ADR-010).
func (s *Servicio) Reponer(ctx context.Context, clave, varianteID string, cantidad int, usuario string) (dominio.Reposicion, bool, error) {
	nueva, err := dominio.NuevaReposicion(s.nuevoID(), clave, varianteID, cantidad, usuario, s.ahora())
	if err != nil {
		return dominio.Reposicion{}, false, err
	}
	guardada, repetida, err := s.repo.CrearReposicion(ctx, nueva)
	if err != nil {
		return dominio.Reposicion{}, false, err
	}
	if repetida && guardada.Huella() != nueva.Huella() {
		return dominio.Reposicion{}, false, dominio.ErrClaveReutilizada
	}
	if !repetida {
		slog.InfoContext(ctx, "reposición al gimnasio pendiente", "reposicion_id", guardada.ID, "variante_id", varianteID, "cantidad", cantidad)
	}
	return guardada, repetida, nil
}

// TomarReposiciones aparta para este proceso las reposiciones listas para enviar.
// Quedan apartadas 30 s: si el proceso se cae, otra réplica las toma después.
func (s *Servicio) TomarReposiciones(ctx context.Context, limite int) ([]dominio.Reposicion, error) {
	return s.repo.TomarReposiciones(ctx, s.ahora(), 30*time.Second, limite)
}

// EnviarReposicion manda una reposición al gimnasio y decide según el resultado (ADR-018). Solo un rechazo
// definitivo devuelve las unidades al stock físico: en los demás casos la mercadería pudo haber llegado.
func (s *Servicio) EnviarReposicion(ctx context.Context, r dominio.Reposicion) (bool, error) {
	idGimnasio, err := s.gimnasio.EnviarReposicion(ctx, r)
	registro := slog.With("reposicion_id", r.ID, "referencia", r.ID, "intentos", r.Intentos+1)
	switch {
	case err == nil:
		if err := s.repo.ConfirmarReposicion(ctx, r.ID, idGimnasio, s.ahora()); err != nil {
			return false, err
		}
		registro.InfoContext(ctx, "el gimnasio confirmó la reposición", "reposicion_gimnasio_id", idGimnasio)
		return true, nil
	case errors.Is(err, dominio.ErrReposicionRechazada):
		registro.WarnContext(ctx, "el gimnasio rechazó la reposición; las unidades vuelven al stock físico", "error", err)
		return false, s.repo.RechazarReposicion(ctx, r.ID, err.Error(), s.ahora())
	case errors.Is(err, dominio.ErrReposicionConflicto):
		registro.ErrorContext(ctx, "conflicto con otra entrega; requiere revisión", "error", err)
		return false, s.repo.MarcarReposicionARevisar(ctx, r.ID, err.Error(), s.ahora())
	case errors.Is(err, dominio.ErrGimnasioConfiguracion):
		proximo := s.ahora().Add(dominio.EsperaBloqueada)
		registro.ErrorContext(ctx, "credencial del gimnasio rechazada; la reposición queda bloqueada", "proximo_intento", proximo, "error", err)
		return false, s.repo.BloquearReposicion(ctx, r.ID, err.Error(), proximo)
	default:
		espera := dominio.EsperaReintento(r.Intentos + 1)
		// La variación evita que muchas reposiciones reintenten juntas cuando vuelve el gimnasio.
		proximo := s.ahora().Add(espera + rand.N(espera/5))
		registro.WarnContext(ctx, "resultado incierto; se reintenta con la misma clave", "proximo_intento", proximo, "error", err)
		return false, s.repo.ReprogramarReposicion(ctx, r.ID, err.Error(), proximo)
	}
}

// ResumirReposiciones cuenta por estado, con todos los estados, y dice hace cuánto espera la pendiente más antigua.
func (s *Servicio) ResumirReposiciones(ctx context.Context) (map[dominio.EstadoReposicion]int, time.Duration, error) {
	cantidades, masAntigua, err := s.repo.ResumirReposiciones(ctx)
	if err != nil {
		return nil, 0, err
	}
	resumen := map[dominio.EstadoReposicion]int{}
	for _, estado := range dominio.EstadosReposicion {
		resumen[estado] = cantidades[estado]
	}
	if masAntigua.IsZero() {
		return resumen, 0, nil
	}
	return resumen, s.ahora().Sub(masAntigua), nil
}

func (s *Servicio) ListarReposiciones(ctx context.Context, limite int) ([]dominio.Reposicion, error) {
	return s.repo.ListarReposiciones(ctx, limite)
}

func (s *Servicio) StockEnGimnasio(ctx context.Context) ([]dominio.StockConsignacion, error) {
	return s.gimnasio.ConsultarStock(ctx)
}

func (s *Servicio) Listo(ctx context.Context) error { return s.repo.Ping(ctx) }

func (s *Servicio) registrar(ctx context.Context, operacion string, r dominio.Reserva, err error) {
	if err != nil {
		slog.InfoContext(ctx, "operación sobre la reserva rechazada", "operacion", operacion, "reserva_id", r.ID, "estado", r.Estado, "motivo", err.Error())
		return
	}
	slog.InfoContext(ctx, "operación sobre la reserva", "operacion", operacion, "reserva_id", r.ID, "estado", r.Estado)
}
