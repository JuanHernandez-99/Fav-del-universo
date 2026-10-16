// Package rabbitmq implementa puertos.PublicadorEventos sobre RabbitMQ.
//
// Inventario publica sus eventos en el exchange "inventario.eventos" (topic, durable)
// con el tipo del evento como routing key (por ejemplo, "ReservaVencida"). Cada
// consumidor declara su propia cola, su DLQ y el binding (ADR-005 §5).
//
// Si un evento no tiene ninguna cola que lo reciba (por ejemplo, el consumidor
// todavía no arrancó nunca), no se pierde: el exchange alternativo lo deja en la
// cola "inventario.eventos.sin-destino" para revisarlo.
package rabbitmq

import (
	"context"
	"errors"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/puertos"
)

const (
	Exchange           = "inventario.eventos"
	ColaSinDestino     = "inventario.eventos.sin-destino"
	exchangeSinDestino = "inventario.eventos.sin-destino"
)

// Publicador mantiene una conexión con RabbitMQ y la rehace si se corta.
// Conecta recién al publicar: el servicio arranca y atiende aunque RabbitMQ
// esté caído, y los eventos esperan en el outbox.
type Publicador struct {
	url  string
	mu   sync.Mutex
	conn *amqp.Connection
	ch   *amqp.Channel
}

var _ puertos.PublicadorEventos = (*Publicador)(nil)

func NuevoPublicador(url string) *Publicador { return &Publicador{url: url} }

// Publicar envía el mensaje como persistente y espera la confirmación del broker
// (publisher confirms): si vuelve nil, RabbitMQ ya lo guardó.
func (p *Publicador) Publicar(ctx context.Context, m puertos.MensajeOutbox) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.conectar(); err != nil {
		return err
	}
	conf, err := p.ch.PublishWithDeferredConfirmWithContext(ctx, Exchange, m.Tipo, false, false, amqp.Publishing{
		ContentType:   "application/json",
		DeliveryMode:  amqp.Persistent,
		MessageId:     m.ID,
		Type:          m.Tipo,
		CorrelationId: m.CorrelacionID,
		AppId:         "inventario",
		Timestamp:     time.Now().UTC(),
		Body:          m.Cuerpo,
	})
	if err != nil {
		p.cerrar()
		return err
	}
	ok, err := conf.WaitContext(ctx)
	if err != nil {
		p.cerrar()
		return err
	}
	if !ok {
		return errors.New("RabbitMQ no aceptó el mensaje (nack)")
	}
	return nil
}

// Cerrar corta la conexión. Se llama al apagar el servicio.
func (p *Publicador) Cerrar() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cerrar()
}

func (p *Publicador) conectar() error {
	if p.conn != nil && !p.conn.IsClosed() && p.ch != nil && !p.ch.IsClosed() {
		return nil
	}
	p.cerrar()
	conn, err := amqp.DialConfig(p.url, amqp.Config{
		Dial:       amqp.DefaultDial(5 * time.Second),
		Properties: amqp.Table{"connection_name": "inventario"},
	})
	if err != nil {
		return err
	}
	ch, err := conn.Channel()
	if err == nil {
		err = declarar(ch)
	}
	if err == nil {
		err = ch.Confirm(false)
	}
	if err != nil {
		conn.Close() //nolint:errcheck
		return err
	}
	p.conn, p.ch = conn, ch
	return nil
}

func (p *Publicador) cerrar() {
	if p.conn != nil {
		p.conn.Close() //nolint:errcheck // también cierra el canal
	}
	p.conn, p.ch = nil, nil
}

// declarar crea el exchange de eventos y su alternativo. Es idempotente.
func declarar(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(exchangeSinDestino, "fanout", true, false, false, false, nil); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(ColaSinDestino, true, false, false, false, nil); err != nil {
		return err
	}
	if err := ch.QueueBind(ColaSinDestino, "", exchangeSinDestino, false, nil); err != nil {
		return err
	}
	return ch.ExchangeDeclare(Exchange, "topic", true, false, false, false,
		amqp.Table{"alternate-exchange": exchangeSinDestino})
}
