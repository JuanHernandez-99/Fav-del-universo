package rabbitmq_test

// Tests contra un RabbitMQ real. Se ejecutan solo si está definida
// INVENTARIO_TEST_AMQP_URL, por ejemplo:
//
//	INVENTARIO_TEST_AMQP_URL='amqp://fav:clave@127.0.0.1:5672/' go test ./...
//
// Crean una cola temporal y vacían la cola de eventos sin destino.

import (
	"context"
	"os"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/adaptadores/rabbitmq"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/puertos"
)

func conectar(t *testing.T) (*rabbitmq.Publicador, *amqp.Channel) {
	t.Helper()
	url := os.Getenv("INVENTARIO_TEST_AMQP_URL")
	if url == "" {
		t.Skip("INVENTARIO_TEST_AMQP_URL no definida: se omiten los tests contra RabbitMQ")
	}
	pub := rabbitmq.NuevoPublicador(url)
	t.Cleanup(pub.Cerrar)
	// Publicar una vez declara el exchange, así el test puede crear su cola.
	if err := pub.Publicar(context.Background(), puertos.MensajeOutbox{ID: "inicial", Tipo: "Prueba", Cuerpo: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	conn, err := amqp.Dial(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ch.QueuePurge(rabbitmq.ColaSinDestino, false); err != nil {
		t.Fatal(err)
	}
	return pub, ch
}

func recibir(t *testing.T, ch *amqp.Channel, cola string) amqp.Delivery {
	t.Helper()
	limite := time.Now().Add(5 * time.Second)
	for time.Now().Before(limite) {
		d, ok, err := ch.Get(cola, true)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			return d
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no llegó ningún mensaje a %s", cola)
	return amqp.Delivery{}
}

func TestPublicaConElTipoComoRoutingKey(t *testing.T) {
	pub, ch := conectar(t)
	q, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ch.QueueBind(q.Name, "ReservaVencida", rabbitmq.Exchange, false, nil); err != nil {
		t.Fatal(err)
	}

	m := puertos.MensajeOutbox{ID: "e1", Tipo: "ReservaVencida", CorrelacionID: "corr-1", Cuerpo: []byte(`{"id":"e1"}`)}
	if err := pub.Publicar(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	d := recibir(t, ch, q.Name)
	if d.MessageId != "e1" || d.Type != "ReservaVencida" || d.CorrelationId != "corr-1" ||
		d.DeliveryMode != amqp.Persistent || d.ContentType != "application/json" || string(d.Body) != `{"id":"e1"}` {
		t.Errorf("mensaje incorrecto: %+v", d)
	}
}

func TestSinColaQueLoRecibaQuedaEnSinDestino(t *testing.T) {
	pub, ch := conectar(t)
	m := puertos.MensajeOutbox{ID: "e2", Tipo: "EventoSinConsumidor", Cuerpo: []byte(`{}`)}
	if err := pub.Publicar(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if d := recibir(t, ch, rabbitmq.ColaSinDestino); d.MessageId != "e2" {
		t.Errorf("se esperaba e2 en %s, vino %q", rabbitmq.ColaSinDestino, d.MessageId)
	}
}

func TestBrokerInalcanzableDevuelveError(t *testing.T) {
	pub := rabbitmq.NuevoPublicador("amqp://nadie:nada@127.0.0.1:1/")
	defer pub.Cerrar()
	if err := pub.Publicar(context.Background(), puertos.MensajeOutbox{ID: "e3", Tipo: "X"}); err == nil {
		t.Error("sin broker, Publicar debe devolver error para que el evento quede en el outbox")
	}
}
