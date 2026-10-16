# Inventario

Cuántas unidades hay y dónde: stock por variante y canal (online y físico) y reservas con vencimiento. Más adelante: transferencias, ingresos, ajustes y movimientos.

> Estado: **en construcción** (Entrega 2). Ya funcionan la consulta de stock, las reservas (reservar, confirmar, liberar y vencer) y la publicación del evento `ReservaVencida` en RabbitMQ. **[propuesta]** marca lo no confirmado.

| | |
|---|---|
| **Base de datos** | MySQL ([ADR-003](../../docs/adr/ADR-003-persistencia.md)) |
| **Estilo interno** | Hexagonal **[propuesta]** ([ARCHITECTURE §4](../../docs/ARCHITECTURE.md)) |
| **Operaciones críticas** | Reserva de stock online y transferencia entre stocks: sin sobreventa, sin duplicados, sin stock online negativo |
| **Lenguaje** | Go 1.22, librería estándar para HTTP y logs; drivers `go-sql-driver/mysql` y `rabbitmq/amqp091-go` |

## Cómo está organizado

```
cmd/inventario/main.go        arma las piezas y arranca (servidor HTTP + procesos de vencimiento y de publicación)
internal/
├── dominio/                  reglas: reserva, estados, validaciones, errores y eventos. No depende de nada externo.
├── aplicacion/               casos de uso: reservar, confirmar, liberar, vencer, consultar stock; relevo del outbox
├── puertos/                  lo que el núcleo necesita: repositorio, outbox, publicador, reloj, generador de ids
└── adaptadores/
    ├── http/                 API HTTP: rutas, JSON, errores (Problem Details), logs con correlación
    ├── mysql/                repositorio y outbox: transacciones, actualizaciones condicionales, formato de los eventos
    └── rabbitmq/             publicador de eventos con confirmación del broker
migraciones/                  esquema y datos de ejemplo (los carga el contenedor de MySQL)
```

**Quién garantiza qué.** El dominio decide si una operación corresponde (por ejemplo, que una reserva vencida no se confirma). La base garantiza que se aplique de forma atómica aunque haya operaciones simultáneas: reservar usa `UPDATE … WHERE cantidad - reservado >= ?` dentro de una transacción, y cada cambio de estado es un `UPDATE … WHERE estado = 'ACTIVA'`. Si la reserva cambió en el medio, el caso de uso la vuelve a leer y el dominio decide de nuevo.

## API (interna)

La usan Ventas y el panel de administración; no es la API pública de la capacidad compartida. Errores en formato Problem Details con `codigo` estable, como en el [contrato publicado](../../docs/contracts/README.md).

| Método y ruta | Qué hace | Errores (`codigo`) |
|---|---|---|
| `GET /health` | `200` si la base responde, `503` si no | — |
| `GET /v1/stock?varianteId=a&varianteId=b` | Stock de las variantes en los dos canales: cantidad, reservado y disponible | `PEDIDO_INVALIDO` |
| `POST /v1/reservas` | Aparta stock online de todas las variantes o de ninguna. Header `Idempotency-Key` obligatorio. Cuerpo: `referencia`, `vencimientoMinutos` (1 a 2880) e `items` (`varianteId`, `cantidad`) | `PEDIDO_INVALIDO`, `STOCK_INSUFICIENTE` (409, con `faltantes`), `VARIANTE_INEXISTENTE` (422), `CLAVE_IDEMPOTENCIA_REUTILIZADA` (409) |
| `GET /v1/reservas/{id}` | Estado de una reserva | `RESERVA_INEXISTENTE` (404) |
| `POST /v1/reservas/{id}/confirmar` | Descuenta el stock en forma definitiva. Confirmar dos veces no descuenta dos veces | `RESERVA_VENCIDA`, `RESERVA_LIBERADA` (409) |
| `POST /v1/reservas/{id}/liberar` | Devuelve las unidades. Liberar dos veces no tiene efecto | `RESERVA_CONFIRMADA` (409) |

Los campos que la API no conoce se ignoran: agregar un campo es un cambio compatible.

**Vencimiento.** Cada 30 segundos (variable `INVENTARIO_VENCIMIENTO_INTERVALO`) se vencen las reservas activas con el plazo cumplido: se devuelven las unidades y se guarda el evento `ReservaVencida` en la tabla `outbox`, en la misma transacción. El evento lo arma el dominio; el repositorio solo lo guarda.

**Logs.** Una línea JSON por pedido con método, ruta, status, duración y `correlation_id`. Se toma de `X-Correlation-Id` (o se genera) y se devuelve en la respuesta. La reserva guarda el `correlation_id` del pedido que la creó, y el evento de su vencimiento lo lleva.

## Eventos que publica

Un proceso lee el outbox cada segundo (`INVENTARIO_OUTBOX_INTERVALO`) y publica los eventos en orden, como mensajes persistentes y esperando la confirmación de RabbitMQ (*publisher confirms*). Solo después los marca como publicados. Si RabbitMQ está caído, los eventos esperan en el outbox y la API sigue atendiendo. La entrega es **al menos una vez**: el consumidor descarta los repetidos por `id` ([ADR-005 §5](../../docs/adr/ADR-005-comunicacion.md)).

| | |
|---|---|
| **Exchange** | `inventario.eventos` (topic, durable). Lo declara Inventario. |
| **Routing key** | El tipo del evento: `ReservaVencida` |
| **Propiedades** | `message_id` = `id`, `type` = `tipo`, `correlation_id`, `content_type: application/json`, persistente |
| **Colas** | Las declara cada consumidor, con su DLQ. Lo que se publica sin ninguna cola que lo reciba queda en `inventario.eventos.sin-destino` (exchange alternativo), no se pierde. |

```json
{
  "id": "6f1c…",
  "tipo": "ReservaVencida",
  "version": 1,
  "ocurridoEn": "2026-10-16T15:20:00.123Z",
  "correlationId": "9b2e…",
  "datos": { "reservaId": "a41d…", "referencia": "venta-1", "vencidaEn": "2026-10-16T15:20:00.123Z" }
}
```

`referencia` es la que envió quien reservó (Ventas manda el id de la venta). Agregar campos es compatible; quitar o cambiar uno exige subir `version`.

## Levantarlo

Desde la raíz del repositorio:

```bash
cp .env.example .env                                  # la primera vez
docker compose up --build mysql-inventario inventario # también levanta RabbitMQ
```

Queda en `http://localhost:8082` **[propuesta]**; los eventos se ven en la consola de RabbitMQ (`http://localhost:15672`). La base arranca con datos de ejemplo (las variantes del contrato publicado). Si cambia el esquema: `docker compose down -v` para recrear la base.

```bash
curl localhost:8082/health
curl "localhost:8082/v1/stock?varianteId=var-remera-basica-blanca-m&varianteId=var-jean-mom-celeste-38"
curl -X POST localhost:8082/v1/reservas \
  -H "Idempotency-Key: venta-1" -H "Content-Type: application/json" \
  -d '{"referencia":"venta-1","vencimientoMinutos":20,"items":[{"varianteId":"var-jean-mom-celeste-38","cantidad":1}]}'
```

## Tests

```bash
cd services/inventario
go test ./...                     # dominio, casos de uso y API HTTP
```

Los tests contra la base real (concurrencia, todo o nada, idempotencia, vencimiento con su evento en el outbox, relevo y la carrera entre confirmar y vencer) se ejecutan si está definida `INVENTARIO_TEST_DSN`. **La base indicada se borra.** Los del publicador, si está definida `INVENTARIO_TEST_AMQP_URL`.

```bash
INVENTARIO_TEST_DSN='usuario:clave@tcp(127.0.0.1:3306)/inventario_test?parseTime=true&loc=UTC&multiStatements=true' \
INVENTARIO_TEST_AMQP_URL='amqp://usuario:clave@127.0.0.1:5672/' go test ./...
```

## Variables de entorno

| Variable | Por defecto |
|---|---|
| `INVENTARIO_PUERTO` | `8080` |
| `INVENTARIO_DB_HOST` / `INVENTARIO_DB_PORT` | `localhost` / `3306` |
| `INVENTARIO_DB_USER` / `INVENTARIO_DB_PASSWORD` / `INVENTARIO_DB_NAME` | `inventario` / — / `inventario` |
| `INVENTARIO_VENCIMIENTO_INTERVALO` | `30s` |
| `INVENTARIO_RABBITMQ_URL` | — (sin ella, los eventos quedan en el outbox sin publicar) |
| `INVENTARIO_OUTBOX_INTERVALO` | `1s` |

## Qué no hace

No conoce precios, compradores ni nombres de productos: solo ids de variante, cantidades y canales.

## Se comunica con

| Con | Cómo | Para qué | Estado |
|---|---|---|---|
| Ventas → Inventario | HTTP, con clave de idempotencia | Reservar, liberar, confirmar al pagar | API lista |
| Inventario → Ventas | Evento `ReservaVencida` | Pasar la venta a *Vencida* | Se publica; falta el consumidor en Ventas |
| Catálogo → Inventario | Evento `VarianteCreada` **[propuesta]** | Registrar la variante con stock 0 | Pendiente |
| Inventario → Gimnasio | HTTP, con timeout, reintentos e idempotencia | Consultar su stock de FAV y registrar reposiciones | Pendiente (ADR-009) |

## Integración con otros grupos

- **Pedidos del grupo de proveedores:** no entran directo a Inventario. Ventas le pide reservar el stock online, igual que en una compra por transferencia ([ADR-008](../../docs/adr/ADR-008-contrato-propio.md)).
- **Consignación en el gimnasio (capacidad consumida):** Inventario llama a la API del gimnasio para consultar cuánto stock de FAV le queda y registrar reposiciones. Al reponer, descuenta el stock físico y guarda una *reposición pendiente* que un proceso envía con reintentos. Se hace con un adaptador de salida que traduce el modelo del gimnasio al de FAV. **[?]** Contrato del gimnasio, pendiente (ADR-009).

## Pendiente

- Consumir `VarianteCreada`.
- Con más de una instancia, dos relevos pueden publicar el mismo evento: es aceptable (entrega al menos una vez), pero conviene repartir el outbox entre instancias si se escala.
- Transferencias entre canales, ingresos y ajustes (CU-03, HU-07, HU-08).
- Descuento directo de la venta del local, que puede dejar el físico en negativo (CU-04, RN-25).
- Adaptador del gimnasio.
