# Ventas

Ventas online y del local, sus estados, los pagos (Mercado Pago y transferencia) y los mails.

> Estado: **funcionando**, con Mercado Pago simulado y mails en Mailpit ([ADR-014](../../docs/adr/ADR-014-pagos-mails-y-orden-de-la-compra.md)).

| | |
|---|---|
| **Base de datos** | MySQL ([ADR-003](../../docs/adr/ADR-003-persistencia.md)). Esquema en [`internal/adaptadores/mysql/sql`](internal/adaptadores/mysql/sql), se aplica al arrancar |
| **Estilo interno** | Hexagonal: `dominio` → `aplicacion` → `puertos` ← `adaptadores` (http, mysql, catalogo, inventario, mercadopago, mail, rabbitmq, procesos) |
| **Réplicas** | Una. Los procesos ya admiten varias (`SKIP LOCKED`) |
| **Estados de la venta** | Ver [`SPEC.md` §7](../../SPEC.md). Una venta del local queda `REGISTRADA`: no tiene circuito |

## Qué no hace

No modifica el stock: se lo pide a [Inventario](../inventario). No consulta a Usuarios: el gateway valida el token e informa la identidad en `X-User-Id`, `X-User-Email` y `X-User-Role` ([ADR-016](../../docs/adr/ADR-016-validacion-del-token-en-el-gateway.md)).

## Cómo levantarlo

Desde la raíz: `docker compose up --build`.

| Qué | Dónde |
|---|---|
| Gateway | http://localhost:8080 |
| Mailpit (mails enviados) | http://localhost:8025 |
| Simulador de Mercado Pago | http://localhost:8096 |
| RabbitMQ (colas; `fav` / `cambiar`) | http://localhost:15672 |

## Probarlo

```bash
# Token de administrador (valores por defecto de Docker Compose). Vence en 1 hora.
TOKEN=$(curl -s localhost:8080/usuarios/v1/sesiones -H "Content-Type: application/json" \
  -d '{"email":"admin@fav.example","contrasena":"cambiar-admin-local"}' | sed 's/.*"token":"\([^"]*\)".*/\1/')
A="Authorization: Bearer $TOKEN"
J="Content-Type: application/json"

# Comprar con Mercado Pago: devuelve pago.urlPago. Abrila y aprobá o rechazá el pago.
curl localhost:8080/ventas/v1/compras -H "$J" -H "Idempotency-Key: compra-0001" \
  -d '{"email":"ana@example.com","medioPago":"MERCADO_PAGO","items":[{"varianteId":"var-remera-basica-blanca-m","cantidad":2}]}'

# Comprar por transferencia y confirmarla desde el panel (reemplazar {id} por el ventaId)
curl localhost:8080/ventas/v1/compras -H "$J" -H "Idempotency-Key: compra-0002" \
  -d '{"email":"ana@example.com","medioPago":"TRANSFERENCIA","items":[{"varianteId":"var-remera-basica-negra-l","cantidad":1}]}'
curl -X POST localhost:8080/ventas/v1/admin/ventas/{id}/confirmar-transferencia -H "$A"

# Ver las ventas pagadas y avanzar el estado
curl "localhost:8080/ventas/v1/admin/ventas?estado=PAGADA" -H "$A"
curl -X POST localhost:8080/ventas/v1/admin/ventas/{id}/avanzar -H "$A"

# Venta del local: se registra aunque no alcance el stock físico (RN-06)
curl localhost:8080/ventas/v1/admin/ventas-local -H "$A" -H "$J" -H "Idempotency-Key: local-0001" \
  -d '{"medioPago":"EFECTIVO","monto":14000,"items":[{"varianteId":"var-jean-mom-celeste-38","cantidad":1}]}'

# Pedido de un grupo externo (contrato catalogo-pedidos-v1)
curl localhost:8080/v1/pedidos -H "X-API-Key: clave-grupo-proveedores" -H "$J" -H "Idempotency-Key: pedido-0001" \
  -d '{"items":[{"varianteId":"var-remera-basica-blanca-m","cantidad":3}]}'
```

**Caídas:** `docker compose stop mercadopago-simulador`, `stop inventario` o `stop rabbitmq`.

## API

Por el gateway, con el prefijo `/ventas`, salvo `/v1/pedidos`.

| Método y ruta | Quién la usa | Qué hace |
|---|---|---|
| `POST /v1/compras` | Comprador | Reserva y crea la venta pendiente. `Idempotency-Key` obligatoria. Con Mercado Pago, devuelve `pago.urlPago`; con transferencia, `pago.datosTransferencia` |
| `POST /v1/compras/{id}/cancelar` | Comprador | Cancela una venta pendiente y sin pago (RN-20) |
| `GET /v1/mis-compras`, `/v1/mis-compras/{id}` | Comprador registrado | Solo sus compras. `401` sin sesión |
| `POST /v1/pagos/aviso` | Mercado Pago | Aviso del pago, con firma. `200` si quedó procesado |
| `GET /v1/admin/ventas?estado=&limite=&desde=` | Administrador | Ventas por estado |
| `GET /v1/admin/ventas/{id}` | Administrador | Una venta |
| `POST /v1/admin/ventas/{id}/confirmar-transferencia` | Administrador | `202` mientras Inventario confirma. Con la venta vencida, queda *A verificar* |
| `POST /v1/admin/ventas/{id}/avanzar` | Administrador | *Pagada* → *Entregada al correo* → *Entregada* |
| `POST /v1/admin/ventas-local` | Administrador | Registra la venta con el monto cobrado |
| `GET /v1/admin/ventas-local/sugerencia?medioPago=&variantes=a:2,b:1` | Administrador | Monto con la promoción vigente |
| `POST /v1/pedidos` | Grupo externo | Capacidad publicada ([contrato](../../docs/contracts/catalogo-pedidos-v1.openapi.yaml)) |
| `GET /healthz`, `/readyz`, `/metrics` | Internas | No se exponen por el gateway |

Los errores siguen RFC 9457 con un `codigo` estable. A los del contrato se suman `VENTA_NO_ENCONTRADA`, `TRANSICION_INVALIDA`, `PAGO_NO_DISPONIBLE` y `NO_AUTORIZADO`.

## Se comunica con

| Con | Cómo | Protección |
|---|---|---|
| Catálogo | `GET http://gateway:8081/catalogo/v1/variantes` | Timeout de 1 s y un reintento. Caído: no se puede comprar; la venta del local se registra igual |
| Inventario | HTTP por `http://gateway:8081`: reservar, confirmar, liberar y `POST /v1/descuentos-fisicos` | 2 s por intento y 2 reintentos, solo ante fallas transitorias. Nunca ante un error de negocio |
| Mercado Pago (simulador) | HTTP | Timeout y circuit breaker: se abre tras 5 fallas seguidas y prueba de nuevo a los 30 s |
| Mailpit | SMTP, en segundo plano | 10 s. Reintentos a 1, 5, 15, 30 y 60 min; después queda fallido sin afectar la venta |
| Inventario → Ventas | Evento `ReservaVencida` por RabbitMQ | 3 reintentos cada 5 s; después, cola `ventas.reserva-vencida.dlq` |

Cada pedido tiene 4,5 s en total, por debajo de los 5 s del gateway ([ADR-014](../../docs/adr/ADR-014-pagos-mails-y-orden-de-la-compra.md)).

**Variantes no publicadas.** En la compra online y en los pedidos del contrato se rechazan con `400 VARIANTE_INEXISTENTE`. En la venta del local se aceptan: publicado solo habilita la web (RN-04).

## Cómo cumple las garantías

- **Sin duplicados:** la clave de idempotencia es única en la base y el id de la venta se deriva de ella. Un reintento devuelve la misma venta; la misma clave con otro pedido responde `409`.
- **Aviso de pago repetido:** el id del pago es único y se guarda en la misma transacción que su efecto (HU-11.6).
- **Evento repetido:** el id del mensaje se guarda en `mensajes_procesados`, en la misma transacción que su efecto.
- **Pago contra vencimiento:** Inventario decide. Ventas registra el pago y una tarea de confirmar en una transacción, y un proceso la ejecuta. Si la reserva venció, la venta queda *A verificar* (RN-14).
- **Inventario caído al pagar o cancelar:** la tarea se reintenta, con esperas de hasta 1 min, hasta que responda.
- **Mails:** se guardan en la misma transacción que el cambio de estado. Si el proceso se cae entre enviar y marcar, puede llegar un duplicado (limitación aceptada en HU-20.4).

## Observabilidad

Logs JSON con `correlation_id` (el track id), también en `app_logs` de `mysql-logs` ([ADR-020](../../docs/adr/ADR-020-logs-y-track-id-modelo-catedra.md)). La venta guarda el track id de la compra: el aviso de pago, la confirmación de una transferencia, las tareas y los mails lo siguen usando, así una compra se ve completa en "FAV · Logs". Métricas en Prometheus: `http_pedidos_total`, `http_duracion_segundos`, `dependencia_llamadas_total`, `circuit_breaker_estado`, `proceso_elementos_total` y `mensajes_consumidos_total`.

## Tests

```bash
go test ./services/ventas/...                   # dominio, casos de uso y adaptadores
docker compose up -d mysql-ventas               # integración y concurrencia contra MySQL
VENTAS_TEST_DSN="root:cambiar@tcp(localhost:3308)/ventas_test?parseTime=true&loc=UTC&multiStatements=true" \
  go test ./services/ventas/...
```

Los tests de integración usan la base `ventas_test`, no la del servicio.

**Verificado el 04/11/2026** con el sistema levantado por Docker: compra con los dos medios, pedido externo, aviso duplicado, pago rechazado, firma inválida, cancelación, avance de estados, venta del local con físico negativo, vencimiento por RabbitMQ, mensaje inválido a la DLQ, y caídas de Mercado Pago, Inventario y RabbitMQ.

**Sin probar:** la API real de Mercado Pago; el circuit breaker abierto con el sistema levantado (solo con un test del adaptador); y el reintento de mails con Mailpit caído.

## Configuración

| Variable | Por defecto | |
|---|---|---|
| `VENTAS_DB_HOST`, `_NAME`, `_USER`, `_PASSWORD` | `mysql-ventas:3306`, `ventas` | Conexión a MySQL |
| `CATALOGO_URL`, `INVENTARIO_URL` | `http://gateway:8081/catalogo`, `http://gateway:8081` | Pasan por el balanceo |
| `RABBITMQ_URL` | — | Sin valor, las ventas no pasan a *Vencida* |
| `CAPACIDAD_PUBLICA_ACTIVA` | activa | Con `false`, apaga `POST /v1/pedidos` ([ADR-008](../../docs/adr/ADR-008-contrato-propio.md)) |
| `MERCADOPAGO_URL`, `_ACCESS_TOKEN`, `_WEBHOOK_SECRET`, `_URL_AVISO`, `_TIMEOUT_MS` | simulador, —, —, gateway, `5000` | Proveedor de pagos |
| `SMTP_DIRECCION`, `SMTP_TIMEOUT_MS`, `MAIL_FROM`, `MAIL_ADMIN` | `mailpit:1025`, `10000`, `ventas@fav.example`, `admin@fav.example` | Mails |
| `TRANSFERENCIA_ALIAS`, `TRANSFERENCIA_TITULAR` | ficticios | **[?]** Los define el cliente |
| `TAREAS_CADA_MS`, `MAILS_CADA_MS`, `REINTENTO_MENSAJE_MS` | `1000`, `2000`, `5000` | Frecuencia de los procesos |

## Más información

[Arquitectura §4 y §6](../../docs/ARCHITECTURE.md) · [ADR-014](../../docs/adr/ADR-014-pagos-mails-y-orden-de-la-compra.md) · [ADR-001](../../docs/adr/ADR-001-limites-de-los-servicios.md) · [ADR-004](../../docs/adr/ADR-004-consistencia-y-concurrencia.md) · [ADR-005](../../docs/adr/ADR-005-comunicacion.md) · [ADR-008](../../docs/adr/ADR-008-contrato-propio.md)
