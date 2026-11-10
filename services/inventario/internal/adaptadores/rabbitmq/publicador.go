// Package rabbitmq implementa puertos.PublicadorEventos sobre RabbitMQ.
//
// Los eventos van al exchange "inventario.eventos" (topic, durable) con el tipo como routing key,
// por ejemplo "ReservaVencida". Cada consumidor declara su cola, su DLQ y el enlace (ADR-005, punto 5).
//
// Si un evento no tiene ninguna cola que lo reciba (el consumidor nunca arrancó), no se pierde:
// el exchange alternativo lo deja en la cola "inventario.eventos.sin-destino".
package rabbitmq

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/dominio"
)

const (
	Exchange           = "inventario.eventos"
	exchangeSinDestino = "inventario.eventos.sin-destino"
	colaSinDestino     = "inventario.eventos.sin-destino"
	versionEvento      = 1
)

// sobre es el formato común de los eventos (ADR-005, punto 5).
type sobre struct {
	ID            string          `json:"id"`
	Tipo          string          `json:"tipo"`
	Version       int             `json:"version"`
	OcurridoEn    time.Time       `json:"ocurridoEn"`
	CorrelationID string          `json:"correlationId"`
	Datos         json.RawMessage `json:"datos"`
}

// Publicador mantiene una conexión y la rehace si se corta. Conecta recién al publicar: Inventario
// arranca y atiende aunque RabbitMQ esté caído, y los eventos esperan en el outbox.
type Publicador struct {
	url  string
	mu   sync.Mutex
	conn *amqp.Connection
	ch   *amqp.Channel
}

func NuevoPublicador(url string) *Publicador { return &Publicador{url: url} }

// Publicar envía el evento como persistente y espera la confirmación del broker:
// si devuelve nil, RabbitMQ ya lo guardó.
func (p *Publicador) Publicar(ctx context.Context, e dominio.Evento) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.conectar(); err != nil {
		return err
	}
	cuerpo, err := json.Marshal(sobre{e.ID, e.Tipo, versionEvento, e.OcurridoEn.UTC(), e.CorrelationID, e.Datos})
	if err != nil {
		return err
	}
	ctx, cancelar := context.WithTimeout(ctx, 5*time.Second)
	defer cancelar()
	confirmacion, err := p.ch.PublishWithDeferredConfirmWithContext(ctx, Exchange, e.Tipo, false, false, amqp.Publishing{
		ContentType:   "application/json",
		DeliveryMode:  amqp.Persistent,
		MessageId:     e.ID,
		Type:          e.Tipo,
		CorrelationId: e.CorrelationID,
		AppId:         "inventario",
		Timestamp:     time.Now().UTC(),
		Body:          cuerpo,
	})
	if err != nil {
		p.cerrar()
		return err
	}
	aceptado, err := confirmacion.WaitContext(ctx)
	if err != nil {
		p.cerrar()
		return err
	}
	if !aceptado {
		return errors.New("RabbitMQ no aceptó el mensaje")
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
		_ = conn.Close()
		return err
	}
	p.conn, p.ch = conn, ch
	return nil
}

func (p *Publicador) cerrar() {
	if p.conn != nil {
		_ = p.conn.Close() // también cierra el canal
	}
	p.conn, p.ch = nil, nil
}

// declarar crea el exchange de eventos y su alternativo. Es idempotente.
func declarar(ch *amqp.Channel) error {
	if err := ch.ExchangeDeclare(exchangeSinDestino, "fanout", true, false, false, false, nil); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare(colaSinDestino, true, false, false, false, nil); err != nil {
		return err
	}
	if err := ch.QueueBind(colaSinDestino, "", exchangeSinDestino, false, nil); err != nil {
		return err
	}
	return ch.ExchangeDeclare(Exchange, "topic", true, false, false, false,
		amqp.Table{"alternate-exchange": exchangeSinDestino})
}
