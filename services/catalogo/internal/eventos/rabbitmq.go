// Package eventos publica y consume ProductoCambiado por RabbitMQ (ADR-006). El evento solo avisa qué producto
// cambió: el indexador lee su estado actual en MongoDB, así que uno repetido o fuera de orden no hace daño.
//
// Colas:
//   - catalogo.indexador: la principal.
//   - catalogo.indexador.reintento: espera unos segundos y devuelve el mensaje a la principal.
//   - catalogo.indexador.dlq: mensajes no procesables o con los reintentos agotados.
package eventos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	Exchange         = "catalogo.eventos"
	Tipo             = "ProductoCambiado"
	Cola             = "catalogo.indexador"
	ColaReintento    = "catalogo.indexador.reintento"
	ColaFallidos     = "catalogo.indexador.dlq"
	cabeceraIntentos = "x-intentos"
	maxReintentos    = 3
	versionEvento    = 1
)

// ErrTransitorio marca una falla de infraestructura, como Elasticsearch caído. El mensaje espera sin consumir
// reintentos: si fuera a la cola de fallidos, el índice quedaría atrasado aunque el evento fuera válido.
var ErrTransitorio = errors.New("falla transitoria")

// sobre es el formato común de los eventos (ADR-005, punto 5).
type sobre struct {
	ID            string    `json:"id"`
	Tipo          string    `json:"tipo"`
	Version       int       `json:"version"`
	OcurridoEn    time.Time `json:"ocurridoEn"`
	CorrelationID string    `json:"correlationId"`
	Datos         datos     `json:"datos"`
}

type datos struct {
	ProductoID string `json:"productoId"`
	Version    int    `json:"version"`
}

func declarar(ch *amqp.Channel, esperaReintento time.Duration) error {
	if err := ch.ExchangeDeclare(Exchange, "topic", true, false, false, false, nil); err != nil {
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
	return ch.QueueBind(Cola, Tipo, Exchange, false, nil)
}

func conectar(urlBroker, nombre string) (*amqp.Connection, *amqp.Channel, error) {
	conn, err := amqp.DialConfig(urlBroker, amqp.Config{
		Dial:       amqp.DefaultDial(5 * time.Second),
		Properties: amqp.Table{"connection_name": nombre},
	})
	if err != nil {
		return nil, nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return conn, ch, nil
}

// Publicador mantiene una conexión y la rehace si se corta. Conecta recién al publicar: Catálogo atiende aunque
// RabbitMQ esté caído, y los cambios esperan marcados en cada producto.
type Publicador struct {
	url             string
	esperaReintento time.Duration
	mu              sync.Mutex
	conn            *amqp.Connection
	ch              *amqp.Channel
}

func NuevoPublicador(urlBroker string, esperaReintento time.Duration) *Publicador {
	return &Publicador{url: urlBroker, esperaReintento: esperaReintento}
}

// Publicar devuelve nil solo si el broker ya guardó el mensaje. Recién ahí se marca el producto como publicado.
func (p *Publicador) Publicar(ctx context.Context, productoID string, version int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.asegurarConexion(); err != nil {
		return err
	}
	id := uuid.NewString()
	cuerpo, err := json.Marshal(sobre{id, Tipo, versionEvento, time.Now().UTC(), "evento-" + id, datos{productoID, version}})
	if err != nil {
		return err
	}
	ctx, cancelar := context.WithTimeout(ctx, 5*time.Second)
	defer cancelar()
	confirmacion, err := p.ch.PublishWithDeferredConfirmWithContext(ctx, Exchange, Tipo, false, false, amqp.Publishing{
		ContentType: "application/json", DeliveryMode: amqp.Persistent, MessageId: id, Type: Tipo,
		CorrelationId: "evento-" + id, AppId: "catalogo", Timestamp: time.Now().UTC(), Body: cuerpo,
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

func (p *Publicador) Cerrar() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cerrar()
}

func (p *Publicador) asegurarConexion() error {
	if p.conn != nil && !p.conn.IsClosed() && p.ch != nil && !p.ch.IsClosed() {
		return nil
	}
	p.cerrar()
	conn, ch, err := conectar(p.url, "catalogo-publicador")
	if err != nil {
		return err
	}
	err = declarar(ch, p.esperaReintento)
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
		_ = p.conn.Close()
	}
	p.conn, p.ch = nil, nil
}

// Manejador aplica el evento. ErrTransitorio reintenta sin límite; otro error, hasta maxReintentos.
type Manejador func(ctx context.Context, productoID string) error

type Consumidor struct {
	url             string
	manejador       Manejador
	esperaReintento time.Duration
}

func NuevoConsumidor(urlBroker string, manejador Manejador, esperaReintento time.Duration) *Consumidor {
	return &Consumidor{urlBroker, manejador, esperaReintento}
}

// Correr consume hasta que se cancele el contexto y se reconecta si se corta.
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
	conn, ch, err := conectar(c.url, "catalogo-indexador")
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := declarar(ch, c.esperaReintento); err != nil {
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
			destino, cabeceras := c.decidir(ctx, d.Body, d.MessageId, intentosDe(d.Headers))
			if destino == "" {
				if err := d.Ack(false); err != nil {
					return err
				}
				continue
			}
			if err := republicar(ctx, ch, d, destino, cabeceras); err != nil {
				return err // sin ack: RabbitMQ lo vuelve a entregar al reconectar
			}
		}
	}
}

// decidir aplica el evento y devuelve a qué cola va el mensaje. Vacío: terminó bien.
func (c *Consumidor) decidir(ctx context.Context, cuerpo []byte, mensajeID string, intentosPrevios int) (string, amqp.Table) {
	var e sobre
	if err := json.Unmarshal(cuerpo, &e); err != nil || e.ID == "" || e.Datos.ProductoID == "" {
		slog.ErrorContext(ctx, "mensaje no procesable: va a la cola de fallidos", "mensaje_id", mensajeID, "error", err)
		return ColaFallidos, amqp.Table{"x-motivo": "no procesable"}
	}
	err := c.manejador(ctx, e.Datos.ProductoID)
	if err == nil {
		return "", nil
	}
	if errors.Is(err, ErrTransitorio) {
		slog.WarnContext(ctx, "no se pudo indexar; se reintenta", "producto_id", e.Datos.ProductoID, "error", err)
		return ColaReintento, amqp.Table{cabeceraIntentos: int32(intentosPrevios)}
	}
	intentos := intentosPrevios + 1
	if intentos > maxReintentos {
		slog.ErrorContext(ctx, "reintentos agotados: el mensaje va a la cola de fallidos", "producto_id", e.Datos.ProductoID, "error", err)
		return ColaFallidos, amqp.Table{"x-motivo": err.Error()}
	}
	slog.WarnContext(ctx, "no se pudo procesar el mensaje; se reintenta", "producto_id", e.Datos.ProductoID, "intento", intentos, "error", err)
	return ColaReintento, amqp.Table{cabeceraIntentos: int32(intentos)}
}

// republicar deja el mensaje en otra cola y recién después confirma el original. Si se cae en el medio el
// mensaje puede duplicarse, pero no perderse; el consumidor es idempotente.
func republicar(ctx context.Context, ch *amqp.Channel, d amqp.Delivery, cola string, cabeceras amqp.Table) error {
	ctx, cancelar := context.WithTimeout(ctx, 5*time.Second)
	defer cancelar()
	if err := ch.PublishWithContext(ctx, "", cola, false, false, amqp.Publishing{
		ContentType: d.ContentType, DeliveryMode: amqp.Persistent, MessageId: d.MessageId, Type: d.Type,
		CorrelationId: d.CorrelationId, Timestamp: d.Timestamp, Headers: cabeceras, Body: d.Body,
	}); err != nil {
		return fmt.Errorf("republicando en %s: %w", cola, err)
	}
	return d.Ack(false)
}

func intentosDe(cabeceras amqp.Table) int {
	switch n := cabeceras[cabeceraIntentos].(type) {
	case int32:
		return int(n)
	case int64:
		return int(n)
	}
	return 0
}
