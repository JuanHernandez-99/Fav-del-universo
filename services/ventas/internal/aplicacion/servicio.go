// Package aplicacion tiene los casos de uso de Ventas. Coordina el dominio y los puertos;
// no conoce HTTP, MySQL, RabbitMQ ni Mercado Pago.
package aplicacion

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/dominio"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/observabilidad"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/puertos"
)

// espacioVentas hace que el id de la venta se derive de su clave de idempotencia: un reintento usa
// el mismo id y, por eso, la misma referencia en la reserva de Inventario (ADR-014).
var espacioVentas = uuid.MustParse("6f1c2b8e-3d4a-4e5f-9a7b-1c2d3e4f5a6b")

type Servicio struct {
	repo       puertos.Repositorio
	catalogo   puertos.Catalogo
	inventario puertos.Inventario
	pagos      puertos.ProveedorPagos
	mailer     puertos.Mailer
	emailAdmin string
	ahora      func() time.Time
}

func NuevoServicio(repo puertos.Repositorio, catalogo puertos.Catalogo, inventario puertos.Inventario, pagos puertos.ProveedorPagos, mailer puertos.Mailer, emailAdmin string) *Servicio {
	return &Servicio{repo, catalogo, inventario, pagos, mailer, emailAdmin, time.Now}
}

// ---------- Compra online y pedidos externos ----------

type PedidoCompra struct {
	Clave       string
	Email       string
	CompradorID string
	Medio       dominio.Medio
	Lineas      []dominio.LineaPedido
}

// Comprar reserva el stock online y registra la venta pendiente de pago (CU-01, CU-02). Si es con
// Mercado Pago crea además el pago. Si esa última parte falla, la venta queda creada: un reintento con
// la misma clave retoma el pago y, si no llega, la reserva vence sola.
func (s *Servicio) Comprar(ctx context.Context, p PedidoCompra) (dominio.Venta, error) {
	if err := dominio.ValidarCompra(p.Clave, p.Email, p.Medio, p.Lineas); err != nil {
		return dominio.Venta{}, err
	}
	huella := dominio.HuellaPedido([]string{"compra", string(p.Medio), p.Email, p.CompradorID}, p.Lineas)
	v, err := s.crearConReserva(ctx, "web:"+p.Clave, huella, dominio.Venta{
		Canal: dominio.CanalOnline, Medio: p.Medio, CompradorID: p.CompradorID, Email: p.Email,
	}, p.Lineas)
	if err != nil {
		return dominio.Venta{}, err
	}
	if v.Medio == dominio.MedioMercadoPago && v.Estado == dominio.EstadoPendiente && v.UrlPago == "" {
		url, err := s.pagos.CrearPago(ctx, v)
		if err != nil {
			return dominio.Venta{}, err
		}
		if err := s.repo.GuardarUrlPago(ctx, v.ID, url); err != nil {
			return dominio.Venta{}, err
		}
		v.UrlPago = url
	}
	return v, nil
}

// PedirExterno es el pedido del grupo de proveedores (HU-26): siempre por transferencia y con 48 horas.
func (s *Servicio) PedirExterno(ctx context.Context, clave, referencia string, lineas []dominio.LineaPedido) (dominio.Venta, error) {
	if err := dominio.ValidarPedidoExterno(clave, referencia, lineas); err != nil {
		return dominio.Venta{}, err
	}
	huella := dominio.HuellaPedido([]string{"pedido", referencia}, lineas)
	return s.crearConReserva(ctx, "grupo:"+clave, huella, dominio.Venta{
		Canal: dominio.CanalGrupoExterno, Medio: dominio.MedioTransferencia, ReferenciaExterna: referencia,
	}, lineas)
}

// crearConReserva primero reserva y después guarda (ADR-014). Si Inventario falla no queda ninguna
// venta; si Ventas se cae después de reservar, la reserva vence sola y un reintento la recupera.
func (s *Servicio) crearConReserva(ctx context.Context, clave, huella string, base dominio.Venta, lineas []dominio.LineaPedido) (dominio.Venta, error) {
	if existente, listo, err := s.ventaExistente(ctx, clave, huella); listo || err != nil {
		return existente, err
	}

	items, err := s.itemsParaVender(ctx, lineas, base.Medio)
	if err != nil {
		return dominio.Venta{}, err
	}

	id := uuid.NewSHA1(espacioVentas, []byte(clave)).String()
	reserva, err := s.inventario.Reservar(ctx, "venta-"+id, id, lineas, base.Medio.Vigencia())
	if err != nil {
		// Catálogo ya confirmó que la variante existe: si Inventario no la conoce, es que no tiene stock
		// (mientras no exista el evento VarianteCreada, se da de alta con el primer ingreso).
		var sinFila *dominio.ErrorVarianteInexistente
		if errors.As(err, &sinFila) {
			return dominio.Venta{}, &dominio.ErrorStockInsuficiente{Faltantes: faltantesDe(sinFila.Variantes, lineas)}
		}
		return dominio.Venta{}, err
	}
	if reserva.Estado != puertos.ReservaActiva {
		// La reserva de un intento anterior ya venció y la venta nunca se llegó a guardar:
		// esa clave no se puede retomar, hay que hacer un pedido nuevo.
		return dominio.Venta{}, fmt.Errorf("%w: la reserva de esta clave ya no está vigente", dominio.ErrClaveReutilizada)
	}

	ahora := s.ahora().UTC().Truncate(time.Second)
	vence := reserva.VenceEn
	v := base
	v.ID, v.Clave, v.Huella = id, clave, huella
	v.Estado = dominio.EstadoPendiente
	v.ReservaID = reserva.ID
	v.Items = items
	v.Total = dominio.TotalDe(items)
	v.VenceEn = &vence
	v.CreadaEn, v.ActualizadaEn = ahora, ahora

	guardada, repetida, err := s.repo.CrearVenta(ctx, v, nil, dominio.MailsAlCrear(v, s.emailAdmin))
	if err != nil {
		return dominio.Venta{}, err
	}
	if repetida && guardada.Huella != huella {
		return dominio.Venta{}, dominio.ErrClaveReutilizada
	}
	if !repetida {
		slog.InfoContext(ctx, "venta creada", "venta_id", v.ID, "canal", v.Canal, "medio", v.Medio, "total", v.Total, "reserva_id", v.ReservaID)
	}
	return guardada, nil
}

func (s *Servicio) ventaExistente(ctx context.Context, clave, huella string) (dominio.Venta, bool, error) {
	existente, err := s.repo.VentaPorClave(ctx, clave)
	if errors.Is(err, dominio.ErrVentaNoEncontrada) {
		return dominio.Venta{}, false, nil
	}
	if err != nil {
		return dominio.Venta{}, false, err
	}
	if existente.Huella != huella {
		return dominio.Venta{}, false, dominio.ErrClaveReutilizada
	}
	return existente, true, nil
}

// itemsParaVender pide precios a Catálogo. El precio lo calcula siempre el servidor (RN-22) y una
// variante no publicada se rechaza en la compra online y en los pedidos públicos.
func (s *Servicio) itemsParaVender(ctx context.Context, lineas []dominio.LineaPedido, medio dominio.Medio) ([]dominio.Item, error) {
	detalles, err := s.catalogo.Variantes(ctx, idsDe(lineas), medio)
	if err != nil {
		return nil, err
	}
	var noPublicadas []string
	for _, d := range detalles {
		if !d.Publicado {
			noPublicadas = append(noPublicadas, d.VarianteID)
		}
	}
	if len(noPublicadas) > 0 {
		return nil, &dominio.ErrorVarianteInexistente{Variantes: noPublicadas}
	}
	return armarItems(lineas, detalles), nil
}

func armarItems(lineas []dominio.LineaPedido, detalles []puertos.DetalleVariante) []dominio.Item {
	porID := make(map[string]puertos.DetalleVariante, len(detalles))
	for _, d := range detalles {
		porID[d.VarianteID] = d
	}
	items := make([]dominio.Item, len(lineas))
	for i, l := range lineas {
		d := porID[l.VarianteID]
		items[i] = dominio.Item{VarianteID: l.VarianteID, Nombre: d.Nombre, Talle: d.Talle, Color: d.Color, Cantidad: l.Cantidad, Precio: d.Precio}
	}
	return items
}

func idsDe(lineas []dominio.LineaPedido) []string {
	ids := make([]string, len(lineas))
	for i, l := range lineas {
		ids[i] = l.VarianteID
	}
	return ids
}

func faltantesDe(variantes []string, lineas []dominio.LineaPedido) []dominio.Faltante {
	cantidades := make(map[string]int, len(lineas))
	for _, l := range lineas {
		cantidades[l.VarianteID] = l.Cantidad
	}
	faltantes := make([]dominio.Faltante, len(variantes))
	for i, id := range variantes {
		faltantes[i] = dominio.Faltante{VarianteID: id, Solicitada: cantidades[id]}
	}
	return faltantes
}

// Cancelar es la acción del comprador (HU-13). La venta cambia de estado ya y la reserva se libera
// después, con reintentos: si Inventario está caído, cancelar igual funciona.
func (s *Servicio) Cancelar(ctx context.Context, id, compradorID string) (dominio.Venta, error) {
	return s.repo.ModificarVenta(ctx, id, func(v *dominio.Venta) ([]dominio.TipoTarea, []dominio.Mail, error) {
		// Una venta ajena responde igual que una inexistente, para no revelar que existe.
		if v.Canal != dominio.CanalOnline || (v.CompradorID != "" && v.CompradorID != compradorID) {
			return nil, nil, dominio.ErrVentaNoEncontrada
		}
		cambio, err := v.Cancelar(s.ahora())
		if err != nil || !cambio {
			return nil, nil, err
		}
		return []dominio.TipoTarea{dominio.TareaLiberar}, nil, nil
	})
}

// ---------- Pagos ----------

// AvisoPago procesa el aviso de Mercado Pago. Se consulta el pago en lugar de creer lo que trae el
// aviso. Si la consulta falla responde error y Mercado Pago reintenta el aviso.
func (s *Servicio) AvisoPago(ctx context.Context, firma, pedidoID, pagoID string) error {
	if !s.pagos.VerificarFirma(firma, pedidoID, pagoID) {
		return dominio.ErrFirmaInvalida
	}
	pago, err := s.pagos.ConsultarPago(ctx, pagoID)
	if err != nil {
		return err
	}
	if pago.Estado == puertos.PagoPendiente {
		return nil // todavía sin resultado: llegará otro aviso
	}
	ctx = s.conTrackIDDeLaVenta(ctx, pago.VentaID, "aviso de pago recibido", "pago_id", pago.ID)
	err = s.repo.RegistrarAvisoPago(ctx, pago.ID, pago.VentaID, func(v *dominio.Venta) ([]dominio.TipoTarea, []dominio.Mail, error) {
		if pago.Estado == puertos.PagoAprobado {
			tareas, mails := s.aplicarPago(v)
			return tareas, mails, nil
		}
		// Pago rechazado: se libera la reserva (HU-11.4). Si la venta ya avanzó, el aviso no tiene efecto.
		cambio, err := v.Cancelar(s.ahora())
		if err != nil || !cambio {
			return nil, nil, nil
		}
		return []dominio.TipoTarea{dominio.TareaLiberar}, nil, nil
	})
	if errors.Is(err, dominio.ErrVentaNoEncontrada) {
		// Reintentar no lo va a arreglar: se registra y se responde bien para cortar los reintentos.
		slog.WarnContext(ctx, "aviso de pago de una venta inexistente", "pago_id", pago.ID, "venta_id", pago.VentaID)
		return nil
	}
	if err == nil {
		slog.InfoContext(ctx, "aviso de pago aplicado", "venta_id", pago.VentaID, "pago_id", pago.ID, "estado_pago", pago.Estado)
	}
	return err
}

// conTrackIDDeLaVenta pasa a usar el track id con el que se creó la venta (ADR-020): el pago y lo que dispara
// llegan en otro pedido, con otro id. Antes deja una línea con el id del pedido que llegó, para unir los dos.
// Si la venta no existe o no tiene track id, sigue con el del pedido.
func (s *Servicio) conTrackIDDeLaVenta(ctx context.Context, ventaID, mensaje string, atributos ...any) context.Context {
	v, err := s.repo.ObtenerVenta(ctx, ventaID)
	if err != nil || v.TrackID == "" || v.TrackID == observabilidad.CorrelationID(ctx) {
		return ctx
	}
	slog.InfoContext(ctx, mensaje, append([]any{"venta_id", ventaID, "track_id_venta", v.TrackID}, atributos...)...)
	return observabilidad.ConCorrelationID(ctx, v.TrackID)
}

func (s *Servicio) aplicarPago(v *dominio.Venta) ([]dominio.TipoTarea, []dominio.Mail) {
	switch v.RegistrarPago(s.ahora()) {
	case dominio.PagoAConfirmar:
		return []dominio.TipoTarea{dominio.TareaConfirmar}, nil
	case dominio.PagoAVerificar:
		return nil, dominio.MailAVerificarAdmin(*v, s.emailAdmin)
	}
	return nil, nil
}

// ConfirmarTransferencia es la acción del administrador (HU-15). Aplicarla dos veces no tiene efecto.
func (s *Servicio) ConfirmarTransferencia(ctx context.Context, id string) (dominio.Venta, error) {
	// La acción del admin queda con su propio id; el pago y lo que dispara, con el de la venta (ADR-020).
	ctx = s.conTrackIDDeLaVenta(ctx, id, "transferencia confirmada por el administrador")
	v, err := s.repo.ModificarVenta(ctx, id, func(v *dominio.Venta) ([]dominio.TipoTarea, []dominio.Mail, error) {
		if v.Medio != dominio.MedioTransferencia || v.Canal == dominio.CanalLocal {
			return nil, nil, fmt.Errorf("%w: la venta no espera una transferencia", dominio.ErrTransicionInvalida)
		}
		tareas, mails := s.aplicarPago(v)
		return tareas, mails, nil
	})
	if err == nil {
		slog.InfoContext(ctx, "pago por transferencia registrado", "venta_id", id, "estado", v.Estado)
	}
	return v, err
}

// Avanzar lleva un pedido pagado hacia Entregada, sin saltar estados (HU-23).
func (s *Servicio) Avanzar(ctx context.Context, id string) (dominio.Venta, error) {
	return s.repo.ModificarVenta(ctx, id, func(v *dominio.Venta) ([]dominio.TipoTarea, []dominio.Mail, error) {
		if err := v.Avanzar(s.ahora()); err != nil {
			return nil, nil, err
		}
		if v.Estado == dominio.EstadoEnCorreo {
			return nil, dominio.MailDespacho(*v), nil
		}
		return nil, nil, nil
	})
}

// ---------- Evento ReservaVencida ----------

// ProcesarReservaVencida aplica el evento una sola vez por id de mensaje. La referencia es el id de la
// venta que Ventas envió al reservar; si no hay venta (la caída entre reservar y guardar), no hace nada.
func (s *Servicio) ProcesarReservaVencida(ctx context.Context, mensajeID, reservaID, referencia string) error {
	procesado, err := s.repo.ProcesarEvento(ctx, mensajeID, referencia, func(v *dominio.Venta) ([]dominio.Mail, error) {
		if v.ReservaID == reservaID && v.Vencer(s.ahora()) {
			slog.InfoContext(ctx, "venta vencida", "venta_id", v.ID, "reserva_id", reservaID)
		}
		return nil, nil
	})
	if err == nil && !procesado {
		slog.InfoContext(ctx, "evento repetido, se descarta", "mensaje_id", mensajeID)
	}
	return err
}

// ---------- Venta del local ----------

type PedidoLocal struct {
	Clave   string
	Medio   dominio.Medio
	Monto   *int64 // centavos cobrados; lo define el administrador (RN-22)
	Lineas  []dominio.LineaPedido
	Usuario string
}

// RegistrarVentaLocal guarda la venta y la tarea de descontar el stock físico en una transacción
// (CU-04). Nunca se bloquea por falta de stock (RN-06). Si Catálogo no responde, se registra sin los
// datos de la prenda: el monto lo carga el administrador (ARCHITECTURE §8).
func (s *Servicio) RegistrarVentaLocal(ctx context.Context, p PedidoLocal) (dominio.Venta, error) {
	if err := dominio.ValidarVentaLocal(p.Clave, p.Medio, p.Monto, p.Lineas); err != nil {
		return dominio.Venta{}, err
	}
	clave := "local:" + p.Clave
	huella := dominio.HuellaPedido([]string{"local", string(p.Medio), fmt.Sprint(*p.Monto)}, p.Lineas)
	if existente, listo, err := s.ventaExistente(ctx, clave, huella); listo || err != nil {
		return existente, err
	}

	detalles, err := s.catalogo.Variantes(ctx, idsDe(p.Lineas), p.Medio)
	if err != nil && !errors.Is(err, dominio.ErrCatalogoNoDisponible) {
		return dominio.Venta{}, err
	}
	if err != nil {
		slog.WarnContext(ctx, "catálogo no disponible: la venta del local se registra sin datos de la prenda", "error", err)
	}

	ahora := s.ahora().UTC().Truncate(time.Second)
	v := dominio.Venta{
		ID: uuid.NewSHA1(espacioVentas, []byte(clave)).String(), Clave: clave, Huella: huella,
		Canal: dominio.CanalLocal, Estado: dominio.EstadoRegistrada, Medio: p.Medio,
		Items: armarItems(p.Lineas, detalles), Total: *p.Monto, CreadaEn: ahora, ActualizadaEn: ahora,
	}
	guardada, repetida, err := s.repo.CrearVenta(ctx, v, []dominio.TipoTarea{dominio.TareaDescontar}, nil)
	if err != nil {
		return dominio.Venta{}, err
	}
	if repetida && guardada.Huella != huella {
		return dominio.Venta{}, dominio.ErrClaveReutilizada
	}
	if !repetida {
		slog.InfoContext(ctx, "venta del local registrada", "venta_id", v.ID, "medio", v.Medio, "monto", v.Total)
	}
	return guardada, nil
}

// SugerirMonto calcula el monto con la promoción vigente para el medio de pago (HU-17.3).
// El administrador puede registrar otro. Acepta variantes no publicadas (RN-04).
func (s *Servicio) SugerirMonto(ctx context.Context, medio dominio.Medio, lineas []dominio.LineaPedido) (int64, error) {
	detalles, err := s.catalogo.Variantes(ctx, idsDe(lineas), medio)
	if err != nil {
		return 0, err
	}
	return dominio.TotalDe(armarItems(lineas, detalles)), nil
}

// ---------- Consultas ----------

func (s *Servicio) ObtenerVenta(ctx context.Context, id string) (dominio.Venta, error) {
	return s.repo.ObtenerVenta(ctx, id)
}

func (s *Servicio) ListarVentas(ctx context.Context, f puertos.FiltroVentas) ([]dominio.Venta, error) {
	return s.repo.ListarVentas(ctx, f)
}

// PedidoExterno devuelve un pedido de grupo externo (contrato catalogo-pedidos-v1 1.3.0). Una venta de otro
// canal responde igual que una inexistente, para no revelar que existe.
func (s *Servicio) PedidoExterno(ctx context.Context, id string) (dominio.Venta, error) {
	v, err := s.repo.ObtenerVenta(ctx, id)
	if err != nil {
		return dominio.Venta{}, err
	}
	if v.Canal != dominio.CanalGrupoExterno {
		return dominio.Venta{}, dominio.ErrVentaNoEncontrada
	}
	return v, nil
}

// CompraDe devuelve una compra solo si es del comprador (HU-14.1).
func (s *Servicio) CompraDe(ctx context.Context, id, compradorID string) (dominio.Venta, error) {
	v, err := s.repo.ObtenerVenta(ctx, id)
	if err != nil {
		return dominio.Venta{}, err
	}
	if v.CompradorID == "" || v.CompradorID != compradorID {
		return dominio.Venta{}, dominio.ErrVentaNoEncontrada
	}
	return v, nil
}

func (s *Servicio) Listo(ctx context.Context) error { return s.repo.Ping(ctx) }

// ---------- Procesos en segundo plano ----------

func (s *Servicio) TomarTareas(ctx context.Context, limite int) ([]dominio.Tarea, error) {
	return s.repo.TomarTareas(ctx, s.ahora(), 30*time.Second, limite)
}

// EjecutarTarea hace lo que Ventas le debe a Inventario. Si la falla es transitoria, la reprograma:
// la tarea sigue hasta que Inventario responda (ADR-005, punto 4).
func (s *Servicio) EjecutarTarea(ctx context.Context, t dominio.Tarea) (bool, error) {
	v, err := s.repo.ObtenerVenta(ctx, t.VentaID)
	if err != nil {
		return false, err
	}
	var errTarea error
	switch t.Tipo {
	case dominio.TareaConfirmar:
		errTarea = s.confirmarReserva(ctx, v)
	case dominio.TareaLiberar:
		errTarea = s.inventario.Liberar(ctx, v.ReservaID)
		if errors.Is(errTarea, dominio.ErrReservaConfirmada) {
			// No debería pasar: el pago registrado impide cancelar. Reintentar no lo arregla.
			slog.ErrorContext(ctx, "se quiso liberar una reserva confirmada", "venta_id", v.ID, "reserva_id", v.ReservaID)
			errTarea = nil
		}
	case dominio.TareaDescontar:
		errTarea = s.inventario.DescontarFisico(ctx, "venta-"+v.ID, v.ID, lineasDe(v.Items))
	default:
		errTarea = fmt.Errorf("tarea desconocida: %s", t.Tipo)
	}
	if errTarea != nil {
		proximo := s.ahora().Add(dominio.EsperaTarea(t.Intentos + 1))
		slog.WarnContext(ctx, "no se pudo ejecutar la tarea; se reintenta", "tarea", t.Tipo, "venta_id", v.ID, "intentos", t.Intentos+1, "proximo_intento", proximo, "error", errTarea)
		return false, s.repo.ReprogramarTarea(ctx, t.ID, errTarea.Error(), proximo)
	}
	return true, s.repo.CompletarTarea(ctx, t.ID)
}

// confirmarReserva: Inventario decide la carrera entre el pago y el vencimiento (ADR-005, punto 4).
func (s *Servicio) confirmarReserva(ctx context.Context, v dominio.Venta) error {
	err := s.inventario.Confirmar(ctx, v.ReservaID)
	switch {
	case err == nil:
		_, err := s.repo.ModificarVenta(ctx, v.ID, func(v *dominio.Venta) ([]dominio.TipoTarea, []dominio.Mail, error) {
			if v.Estado == dominio.EstadoPagada {
				return nil, nil, nil // la tarea se repitió: los mails ya están
			}
			if err := v.ConfirmarPago(s.ahora()); err != nil {
				return nil, nil, err
			}
			return nil, dominio.MailsAlPagar(*v, s.emailAdmin), nil
		})
		if err == nil {
			slog.InfoContext(ctx, "reserva confirmada: la venta queda pagada", "venta_id", v.ID, "reserva_id", v.ReservaID)
		}
		return err
	case errors.Is(err, dominio.ErrReservaVencida), errors.Is(err, dominio.ErrReservaLiberada), errors.Is(err, dominio.ErrReservaNoEncontrada):
		slog.WarnContext(ctx, "pago con la reserva no vigente: la venta queda a verificar", "venta_id", v.ID, "reserva_id", v.ReservaID, "motivo", err)
		_, err := s.repo.ModificarVenta(ctx, v.ID, func(v *dominio.Venta) ([]dominio.TipoTarea, []dominio.Mail, error) {
			if v.Estado == dominio.EstadoAVerificar {
				return nil, nil, nil
			}
			if err := v.MarcarAVerificar(s.ahora()); err != nil {
				return nil, nil, err
			}
			return nil, dominio.MailAVerificarAdmin(*v, s.emailAdmin), nil
		})
		return err
	}
	return err
}

func lineasDe(items []dominio.Item) []dominio.LineaPedido {
	lineas := make([]dominio.LineaPedido, len(items))
	for i, it := range items {
		lineas[i] = dominio.LineaPedido{VarianteID: it.VarianteID, Cantidad: it.Cantidad}
	}
	return lineas
}

func (s *Servicio) TomarMails(ctx context.Context, limite int) ([]dominio.Mail, error) {
	return s.repo.TomarMails(ctx, s.ahora(), 60*time.Second, limite)
}

// EnviarMail no afecta la venta: agotados los intentos, el mail queda como fallido (HU-20.5).
func (s *Servicio) EnviarMail(ctx context.Context, m dominio.Mail) (bool, error) {
	err := s.mailer.Enviar(ctx, m)
	if err == nil {
		slog.InfoContext(ctx, "mail enviado", "mail_id", m.ID, "tipo", m.Tipo, "venta_id", m.VentaID)
		return true, s.repo.MarcarMailEnviado(ctx, m.ID)
	}
	intentos := m.Intentos + 1
	if intentos >= dominio.MaxIntentosMail {
		slog.ErrorContext(ctx, "el mail no se pudo enviar; queda como fallido", "mail_id", m.ID, "tipo", m.Tipo, "venta_id", m.VentaID, "error", err)
		return false, s.repo.MarcarMailFallido(ctx, m.ID, err.Error())
	}
	proximo := s.ahora().Add(dominio.EsperaMail(intentos))
	slog.WarnContext(ctx, "no se pudo enviar el mail; se reintenta", "mail_id", m.ID, "tipo", m.Tipo, "intentos", intentos, "proximo_intento", proximo, "error", err)
	return false, s.repo.ReprogramarMail(ctx, m.ID, err.Error(), proximo)
}
