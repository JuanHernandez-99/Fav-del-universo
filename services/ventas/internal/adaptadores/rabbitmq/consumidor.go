// Package rabbitmq consume el evento ReservaVencida que publica Inventario (ADR-005, punto 5).
//
// Colas de Ventas:
//   - ventas.reserva-vencida: la principal.
//   - ventas.reserva-vencida.reintento: espera unos segundos y devuelve el mensaje a la principal.
//   - ventas.reserva-vencida.dlq: mensajes no procesables o con los reintentos agotados.
//
// La idempotencia no está acá: la da el repositorio con la tabla de mensajes procesados.
package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/observabilidad"
)

const (
	Exchange           = "inventario.eventos"
	exchangeSinDestino = "inventario.eventos.sin-destino"
	RoutingKey         = "ReservaVencida"
	Cola               = "ventas.reserva-vencida"
	ColaReintento      = "ventas.reserva-vencida.reintento"
	ColaFallidos       = "ventas.reserva-vencida.dlq"
	cabeceraIntentos   = "x-intentos"
	maxReintentos      = 3
)

// Manejador aplica el evento. Un error es transitorio: el mensaje se reintenta.
type Manejador func(ctx context.Context, mensajeID, reservaID, referencia string) error

type Consumidor struct {
	url             string
	manejador       Manejador
	esperaReintento time.Duration
}

func NuevoConsumidor(url string, manejador Manejador, esperaReintento time.Duration) *Consumidor {
	return &Consumidor{url, manejador, esperaReintento}
}

// Correr consume hasta que se cancele el contexto y se reconecta si se corta. Ventas sigue atendiendo
// sin RabbitMQ; solo se atrasa el paso a Vencida (ADR-005, consecuencias).
func (c *Consumidor) Correr(ctx context.Context) {
	espera := time.Second
	for ctx.Err() == nil {
		err := c.consumir(ctx)
		if ctx.Err() != nil {
			return
		}
		slog.WarnContext(ctx, "se cortó la conexión con RabbitMQ; se reintenta", "espera", espera, "error", err)
		select {
		case <-time.After(espera):
		case <-ctx.Done():
			return
		}
		espera = min(espera*2, 30*time.Second)
	}
}

func (c *Consumidor) consumir(ctx context.Context) error {
	conn, err := amqp.DialConfig(c.url, amqp.Config{
		Dial:       amqp.DefaultDial(5 * time.Second),
		Properties: amqp.Table{"connection_name": "ventas"},
	})
	if err != nil {
		return err
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	if err := Declarar(ch, c.esperaReintento); err != nil {
		return err
	}
	if err := ch.Qos(10, 0, false); err != nil {
		return err
	}
	entregas, err := ch.Consume(Cola, "", false, false, false, false, nil)
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "consumiendo eventos", "cola", Cola)
	for {
		select {
		case <-ctx.Done():
			return nil
		case d, abierto := <-entregas:
			if !abierto {
				return errors.New("el canal de entregas se cerró")
			}
			if err := c.atender(ctx, ch, d); err != nil {
				return err // sin ack: RabbitMQ lo vuelve a entregar al reconectar
			}
		}
	}
}

type sobre struct {
	ID            string `json:"id"`
	Tipo          string `json:"tipo"`
	Version       int    `json:"version"`
	CorrelationID string `json:"correlationId"`
	Datos         struct {
		ReservaID  string `json:"reservaId"`
		Referencia string `json:"referencia"`
	} `json:"datos"`
}

// atender decide el destino del mensaje: hecho, a reintentar o a la cola de fallidos.
func (c *Consumidor) atender(ctx context.Context, ch *amqp.Channel, d amqp.Delivery) error {
	var e sobre
	if err := json.Unmarshal(d.Body, &e); err != nil || e.ID == "" || e.Datos.ReservaID == "" {
		slog.ErrorContext(ctx, "mensaje no procesable: va a la cola de fallidos", "mensaje_id", d.MessageId, "error", err)
		return c.aFallidos(ctx, ch, d, "no procesable")
	}

	id := e.CorrelationID
	if id == "" {
		id = "evento-" + uuid.NewString()
	}
	ctxMensaje := observabilidad.ConCorrelationID(ctx, id)
	// El track id viene en el sobre del evento: el del proceso de Inventario que venció la reserva (ADR-020).
	err := c.manejador(ctxMensaje, e.ID, e.Datos.ReservaID, e.Datos.Referencia)
	if err == nil {
		observabilidad.MensajesConsumidos.WithLabelValues("ok").Inc()
		return d.Ack(false)
	}

	intentos := intentosDe(d) + 1
	if intentos > maxReintentos {
		slog.ErrorContext(ctxMensaje, "reintentos agotados: el mensaje va a la cola de fallidos", "mensaje_id", e.ID, "error", err)
		return c.aFallidos(ctx, ch, d, err.Error())
	}
	slog.WarnContext(ctxMensaje, "no se pudo procesar el mensaje; se reintenta", "mensaje_id", e.ID, "intento", intentos, "error", err)
	observabilidad.MensajesConsumidos.WithLabelValues("reintento").Inc()
	return c.republicar(ctx, ch, d, ColaReintento, amqp.Table{cabeceraIntentos: int32(intentos)})
}

func (c *Consumidor) aFallidos(ctx context.Context, ch *amqp.Channel, d amqp.Delivery, motivo string) error {
	observabilidad.MensajesConsumidos.WithLabelValues("fallido").Inc()
	return c.republicar(ctx, ch, d, ColaFallidos, amqp.Table{"x-motivo": motivo})
}

// republicar deja el mensaje en otra cola y recién después confirma el original. Si se cae en el medio
// el mensaje puede duplicarse, pero no perderse; el consumidor es idempotente.
func (c *Consumidor) republicar(ctx context.Context, ch *amqp.Channel, d amqp.Delivery, cola string, cabeceras amqp.Table) error {
	ctx, cancelar := context.WithTimeout(ctx, 5*time.Second)
	defer cancelar()
	if err := ch.PublishWithContext(ctx, "", cola, false, false, amqp.Publishing{
		ContentType:   d.ContentType,
		DeliveryMode:  amqp.Persistent,
		MessageId:     d.MessageId,
		Type:          d.Type,
		CorrelationId: d.CorrelationId,
		Timestamp:     d.Timestamp,
		Headers:       cabeceras,
		Body:          d.Body,
	}); err != nil {
		return fmt.Errorf("republicando en %s: %w", cola, err)
	}
	return d.Ack(false)
}

func intentosDe(d amqp.Delivery) int {
	switch n := d.Headers[cabeceraIntentos].(type) {
	case int32:
		return int(n)
	case int64:
		return int(n)
	}
	return 0
}

// Declarar crea el exchange (con los mismos argumentos que usa Inventario, o RabbitMQ lo rechaza),
// las colas y el enlace. Es idempotente y no depende de quién arranque primero.
func Declarar(ch *amqp.Channel, esperaReintento time.Duration) error {
	if err := ch.ExchangeDeclare(Exchange, "topic", true, false, false, false, amqp.Table{"alternate-exchange": exchangeSinDestino}); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(ColaFallidos, true, false, false, false, nil); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(ColaReintento, true, false, false, false, amqp.Table{
		"x-message-ttl":             esperaReintento.Milliseconds(),
		"x-dead-letter-exchange":    "",
		"x-dead-letter-routing-key": Cola,
	}); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(Cola, true, false, false, false, nil); err != nil {
		return err
	}
	return ch.QueueBind(Cola, RoutingKey, Exchange, false, nil)
}
