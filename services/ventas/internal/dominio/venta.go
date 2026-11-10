// Package dominio tiene las reglas de la venta: sus estados, qué transiciones admite y qué datos exige.
// No conoce HTTP, MySQL, Inventario ni Mercado Pago.
package dominio

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"time"
)

type Estado string

const (
	EstadoPendiente  Estado = "PENDIENTE_DE_PAGO"
	EstadoPagada     Estado = "PAGADA"
	EstadoEnCorreo   Estado = "ENTREGADA_AL_CORREO"
	EstadoEntregada  Estado = "ENTREGADA"
	EstadoCancelada  Estado = "CANCELADA"
	EstadoVencida    Estado = "VENCIDA"
	EstadoAVerificar Estado = "A_VERIFICAR"
	EstadoRegistrada Estado = "REGISTRADA" // venta del local: no tiene estados (SPEC §7)
)

func (e Estado) Valido() bool {
	switch e {
	case EstadoPendiente, EstadoPagada, EstadoEnCorreo, EstadoEntregada, EstadoCancelada, EstadoVencida, EstadoAVerificar, EstadoRegistrada:
		return true
	}
	return false
}

type Canal string

const (
	CanalOnline       Canal = "ONLINE"
	CanalLocal        Canal = "LOCAL"
	CanalGrupoExterno Canal = "GRUPO_EXTERNO"
)

type Medio string

const (
	MedioMercadoPago   Medio = "MERCADO_PAGO"
	MedioTransferencia Medio = "TRANSFERENCIA"
	MedioEfectivo      Medio = "EFECTIVO"
)

// ValidoEn aplica RN-18: online se paga con Mercado Pago o transferencia; en el local, además, efectivo.
func (m Medio) ValidoEn(c Canal) bool {
	switch m {
	case MedioMercadoPago, MedioTransferencia:
		return true
	case MedioEfectivo:
		return c == CanalLocal
	}
	return false
}

// Vigencia de la reserva según el medio de pago (RN-08, RN-09).
func (m Medio) Vigencia() time.Duration {
	if m == MedioMercadoPago {
		return 20 * time.Minute
	}
	return 48 * time.Hour
}

// MaxItems es el límite por reserva que impone Inventario (ADR-008).
const MaxItems = 20

// Item es una línea de la venta, con la copia de los datos del catálogo al momento de vender:
// una venta registrada conserva su precio aunque el catálogo cambie (HU-05.1).
type Item struct {
	VarianteID string
	Nombre     string
	Talle      string
	Color      string
	Cantidad   int
	Precio     int64 // centavos
}

func (i Item) Subtotal() int64 { return i.Precio * int64(i.Cantidad) }

// Pedido es lo que pide el comprador, antes de saber precios ni stock.
type LineaPedido struct {
	VarianteID string
	Cantidad   int
}

// Venta es una compra online, un pedido de un grupo externo o una venta del local.
type Venta struct {
	ID                string
	Clave             string // clave de idempotencia
	Huella            string // resumen del pedido, para detectar una clave reutilizada con otro cuerpo
	Canal             Canal
	Estado            Estado
	Medio             Medio
	CompradorID       string // vacío si es invitado, grupo externo o local
	Email             string // al que van los mails de la compra (HU-20.6)
	ReferenciaExterna string
	ReservaID         string
	Items             []Item
	Total             int64 // centavos; en el local, el monto cobrado (RN-22)
	UrlPago           string
	PagoAprobadoEn    *time.Time // pago registrado, a la espera de que Inventario confirme (ADR-005, punto 4)
	VenceEn           *time.Time
	CreadaEn          time.Time
	ActualizadaEn     time.Time
}

func TotalDe(items []Item) int64 {
	var total int64
	for _, it := range items {
		total += it.Subtotal()
	}
	return total
}

// ---------- Validaciones de entrada ----------

// ValidarCompra revisa una compra online antes de llamar a nadie (RN-11, RN-18).
func ValidarCompra(clave, email string, medio Medio, lineas []LineaPedido) error {
	var ev ErrorValidacion
	validarClave(&ev, clave)
	if _, err := mail.ParseAddress(email); err != nil || len(email) > 254 {
		ev.agregar("email", "es obligatorio y debe ser un email válido")
	}
	if medio != MedioMercadoPago && medio != MedioTransferencia {
		ev.agregar("medioPago", "debe ser MERCADO_PAGO o TRANSFERENCIA")
	}
	validarLineas(&ev, lineas, 0)
	return ev.comoError()
}

// ValidarPedidoExterno aplica los límites del contrato catalogo-pedidos-v1.
func ValidarPedidoExterno(clave, referencia string, lineas []LineaPedido) error {
	var ev ErrorValidacion
	validarClave(&ev, clave)
	if len(referencia) > 100 {
		ev.agregar("referenciaExterna", "hasta 100 caracteres")
	}
	validarLineas(&ev, lineas, 10)
	return ev.comoError()
}

// ValidarVentaLocal exige monto y medio de pago (HU-17.4, HU-17.5).
func ValidarVentaLocal(clave string, medio Medio, monto *int64, lineas []LineaPedido) error {
	var ev ErrorValidacion
	validarClave(&ev, clave)
	if !medio.ValidoEn(CanalLocal) {
		ev.agregar("medioPago", "debe ser EFECTIVO, TRANSFERENCIA o MERCADO_PAGO")
	}
	if monto == nil || *monto < 0 {
		ev.agregar("monto", "es obligatorio y no puede ser negativo")
	}
	validarLineas(&ev, lineas, 0)
	return ev.comoError()
}

func validarClave(ev *ErrorValidacion, clave string) {
	if len(clave) < 8 || len(clave) > 100 {
		ev.agregar("Idempotency-Key", "es obligatoria y debe tener entre 8 y 100 caracteres")
	}
}

// validarLineas revisa los ítems. maxCantidad en 0 significa sin máximo por variante (RN-13).
func validarLineas(ev *ErrorValidacion, lineas []LineaPedido, maxCantidad int) {
	if len(lineas) == 0 || len(lineas) > MaxItems {
		ev.agregar("items", "debe tener entre 1 y %d variantes", MaxItems)
	}
	vistas := make(map[string]bool, len(lineas))
	for i, l := range lineas {
		campo := fmt.Sprintf("items[%d]", i)
		if strings.TrimSpace(l.VarianteID) == "" || len(l.VarianteID) > 100 {
			ev.agregar(campo+".varianteId", "es obligatorio y de hasta 100 caracteres")
		}
		if l.Cantidad < 1 {
			ev.agregar(campo+".cantidad", "debe ser mayor a 0")
		} else if maxCantidad > 0 && l.Cantidad > maxCantidad {
			ev.agregar(campo+".cantidad", "debe estar entre 1 y %d", maxCantidad)
		}
		if vistas[l.VarianteID] {
			ev.agregar(campo+".varianteId", "variante repetida")
		}
		vistas[l.VarianteID] = true
	}
}

// HuellaPedido resume el pedido sin depender del orden de los ítems.
func HuellaPedido(partes []string, lineas []LineaPedido) string {
	ordenadas := append([]LineaPedido(nil), lineas...)
	sort.Slice(ordenadas, func(a, b int) bool { return ordenadas[a].VarianteID < ordenadas[b].VarianteID })
	var b strings.Builder
	b.WriteString(strings.Join(partes, "|"))
	for _, l := range ordenadas {
		fmt.Fprintf(&b, "|%s:%d", l.VarianteID, l.Cantidad)
	}
	suma := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(suma[:])
}

// ---------- Transiciones (SPEC §7) ----------

// ResultadoPago dice qué hacer con un pago aprobado según el estado de la venta.
type ResultadoPago int

const (
	PagoSinEfecto  ResultadoPago = iota // ya estaba registrado, o la venta ya está pagada
	PagoAConfirmar                      // queda registrado; Inventario decide si la reserva sigue vigente
	PagoAVerificar                      // llegó con la venta vencida o cancelada (RN-14)
)

// RegistrarPago aplica un pago aprobado (aviso de Mercado Pago o transferencia confirmada).
func (v *Venta) RegistrarPago(ahora time.Time) ResultadoPago {
	switch v.Estado {
	case EstadoPendiente:
		if v.PagoAprobadoEn != nil {
			return PagoSinEfecto
		}
		momento := ahora.UTC().Truncate(time.Second)
		v.PagoAprobadoEn = &momento
		v.ActualizadaEn = momento
		return PagoAConfirmar
	case EstadoVencida, EstadoCancelada:
		v.cambiar(EstadoAVerificar, ahora)
		return PagoAVerificar
	}
	return PagoSinEfecto
}

// ConfirmarPago: Inventario confirmó la reserva.
func (v *Venta) ConfirmarPago(ahora time.Time) error {
	if v.Estado == EstadoPagada {
		return nil // reintento: mismo resultado
	}
	if v.Estado != EstadoPendiente || v.PagoAprobadoEn == nil {
		return fmt.Errorf("%w: confirmar pago desde %s", ErrTransicionInvalida, v.Estado)
	}
	v.cambiar(EstadoPagada, ahora)
	return nil
}

// MarcarAVerificar: Inventario rechazó la confirmación porque la reserva ya no está vigente (RN-14).
func (v *Venta) MarcarAVerificar(ahora time.Time) error {
	if v.Estado == EstadoAVerificar {
		return nil
	}
	if v.Estado != EstadoPendiente {
		return fmt.Errorf("%w: a verificar desde %s", ErrTransicionInvalida, v.Estado)
	}
	v.cambiar(EstadoAVerificar, ahora)
	return nil
}

// Cancelar: solo una venta pendiente y sin pago registrado (RN-20). Cancelar dos veces no hace nada.
func (v *Venta) Cancelar(ahora time.Time) (cambio bool, err error) {
	if v.Estado == EstadoCancelada {
		return false, nil
	}
	if v.Estado != EstadoPendiente || v.PagoAprobadoEn != nil {
		return false, fmt.Errorf("%w: cancelar desde %s", ErrTransicionInvalida, v.Estado)
	}
	v.cambiar(EstadoCancelada, ahora)
	return true, nil
}

// Vencer aplica el evento ReservaVencida. Si ya no está pendiente, no hace nada (ADR-005, punto 5).
// Con un pago ya registrado tampoco: la confirmación en curso la llevará a A verificar.
func (v *Venta) Vencer(ahora time.Time) bool {
	if v.Estado != EstadoPendiente || v.PagoAprobadoEn != nil {
		return false
	}
	v.cambiar(EstadoVencida, ahora)
	return true
}

// Avanzar sigue el circuito fijo: Pagada → Entregada al correo → Entregada (HU-23).
func (v *Venta) Avanzar(ahora time.Time) error {
	switch v.Estado {
	case EstadoPagada:
		v.cambiar(EstadoEnCorreo, ahora)
	case EstadoEnCorreo:
		v.cambiar(EstadoEntregada, ahora)
	default:
		return fmt.Errorf("%w: avanzar desde %s", ErrTransicionInvalida, v.Estado)
	}
	return nil
}

func (v *Venta) cambiar(estado Estado, ahora time.Time) {
	v.Estado = estado
	v.ActualizadaEn = ahora.UTC().Truncate(time.Second)
}

// ---------- Tareas y mails ----------

// TipoTarea es lo que Ventas le debe a Inventario. Se guarda en la misma transacción que el cambio de
// la venta y un proceso la ejecuta después, con reintentos (ADR-005, punto 4).
type TipoTarea string

const (
	TareaConfirmar TipoTarea = "CONFIRMAR"
	TareaLiberar   TipoTarea = "LIBERAR"
	TareaDescontar TipoTarea = "DESCONTAR"
)

type Tarea struct {
	ID       string
	VentaID  string
	Tipo     TipoTarea
	Intentos int
}

// EsperaTarea crece con cada intento, hasta un minuto: la tarea se reintenta hasta que Inventario responda.
func EsperaTarea(intentos int) time.Duration {
	espera := time.Second << min(intentos, 6)
	return min(espera, time.Minute)
}

type TipoMail string

const (
	MailCompraRealizada        TipoMail = "COMPRA_REALIZADA"        // HU-20.1
	MailPagoAprobado           TipoMail = "PAGO_APROBADO"           // HU-20.2
	MailPedidoDespachado       TipoMail = "PEDIDO_DESPACHADO"       // HU-20.3
	MailVentaNueva             TipoMail = "VENTA_NUEVA"             // HU-21.1
	MailTransferenciaPendiente TipoMail = "TRANSFERENCIA_PENDIENTE" // HU-21.2
	MailAVerificar             TipoMail = "A_VERIFICAR"             // HU-21.3
)

// Mail es un envío pendiente. Se guarda en la misma transacción que el cambio de estado (ADR-005, punto 6).
type Mail struct {
	ID           string
	VentaID      string
	Tipo         TipoMail
	Destinatario string
	Asunto       string
	Cuerpo       string
	Intentos     int
}

// MaxIntentosMail: el primer envío más 5 reintentos (ADR-005, punto 2). Agotados, queda como fallido (HU-20.5).
const MaxIntentosMail = 6

// EsperaMail devuelve la espera antes del reintento: 1, 5, 15, 30 y 60 minutos.
func EsperaMail(intentos int) time.Duration {
	esperas := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute, time.Hour}
	return esperas[min(max(intentos-1, 0), len(esperas)-1)]
}

// MailsAlCrear devuelve los avisos de una venta recién creada. Un grupo externo no deja email (ADR-008).
func MailsAlCrear(v Venta, admin string) []Mail {
	var mails []Mail
	if v.Email != "" {
		mails = append(mails, mailDe(v, MailCompraRealizada, v.Email,
			"Recibimos tu compra en FAV", fmt.Sprintf("Tu compra %s por %s quedó pendiente de pago.%s", v.ID, Pesos(v.Total), detalle(v))))
	}
	if v.Medio == MedioTransferencia {
		mails = append(mails, mailDe(v, MailTransferenciaPendiente, admin,
			"Transferencia pendiente de confirmar", fmt.Sprintf("La venta %s por %s espera la transferencia.%s", v.ID, Pesos(v.Total), detalle(v))))
	}
	return mails
}

// MailsAlPagar: al comprador y al administrador (HU-20.2, HU-21.1).
func MailsAlPagar(v Venta, admin string) []Mail {
	mails := []Mail{mailDe(v, MailVentaNueva, admin, "Venta nueva", fmt.Sprintf("La venta %s por %s está pagada.%s", v.ID, Pesos(v.Total), detalle(v)))}
	if v.Email != "" {
		mails = append(mails, mailDe(v, MailPagoAprobado, v.Email, "Tu pago fue aprobado", fmt.Sprintf("Recibimos el pago de tu compra %s por %s.%s", v.ID, Pesos(v.Total), detalle(v))))
	}
	return mails
}

// MailDespacho avisa que el pedido salió (HU-20.3). "Pedido listo" se informa junto con esto (SPEC §7).
func MailDespacho(v Venta) []Mail {
	if v.Email == "" {
		return nil
	}
	return []Mail{mailDe(v, MailPedidoDespachado, v.Email, "Tu pedido fue despachado", fmt.Sprintf("Tu compra %s ya fue despachada.%s", v.ID, detalle(v)))}
}

// MailAVerificarAdmin avisa de un pago que llegó con la reserva vencida (RN-14, HU-21.3).
func MailAVerificarAdmin(v Venta, admin string) []Mail {
	return []Mail{mailDe(v, MailAVerificar, admin, "Venta a verificar", fmt.Sprintf("La venta %s por %s recibió un pago con la reserva vencida: hay que resolverla a mano.", v.ID, Pesos(v.Total)))}
}

func mailDe(v Venta, tipo TipoMail, destinatario, asunto, cuerpo string) Mail {
	return Mail{VentaID: v.ID, Tipo: tipo, Destinatario: destinatario, Asunto: asunto, Cuerpo: cuerpo}
}

func detalle(v Venta) string {
	var b strings.Builder
	for _, it := range v.Items {
		fmt.Fprintf(&b, "\n- %d x %s %s %s", it.Cantidad, it.Nombre, it.Talle, it.Color)
	}
	return b.String()
}

// Pesos formatea centavos para los mails: 1599990 → $15999,90.
func Pesos(centavos int64) string {
	return fmt.Sprintf("$%d,%02d", centavos/100, centavos%100)
}
