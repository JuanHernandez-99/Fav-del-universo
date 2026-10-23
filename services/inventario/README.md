# Inventario

Cuántas unidades hay y dónde: stock por variante y canal (online y físico), reservas con vencimiento, transferencias, ingresos y consignación en el gimnasio.

> Estado: **funcionando** (Entrega 2). Es el servicio real de esta entrega; Catálogo, Ventas y el gimnasio están simulados ([`mocks/`](../../mocks)).

| | |
|---|---|
| **Base de datos** | MySQL ([ADR-003](../../docs/adr/ADR-003-persistencia.md)). Esquema en [`internal/adaptadores/mysql/sql`](internal/adaptadores/mysql/sql), se aplica al arrancar |
| **Estilo interno** | Hexagonal: `dominio` → `aplicacion` → `puertos` ← `adaptadores` (http, mysql, gimnasio, procesos) |
| **Réplicas** | Dos, detrás del gateway (nginx, round robin) |
| **Operaciones críticas** | Reserva de stock online y transferencia entre stocks: sin sobreventa, sin duplicados, sin stock online negativo |

## Cómo levantarlo

Desde la raíz del repositorio:

```bash
docker compose up --build
```

Levanta Inventario (2 réplicas), su MySQL con stock de ejemplo, el gateway, los mocks, Jaeger y Prometheus.

## Probarlo

Por el gateway (`http://localhost:8080`). Las rutas del panel van con el prefijo `/inventario` y la clave provisoria del administrador:

```bash
A="X-Admin-Key: clave-admin-local"

# Stock de varias variantes
curl "localhost:8080/inventario/v1/stock?variantes=var-remera-basica-blanca-m,var-jean-mom-celeste-38" -H "$A"

# Transferir 2 unidades del físico al online
curl localhost:8080/inventario/v1/transferencias -H "$A" -H "Idempotency-Key: transf-0001" \
  -d '{"varianteId":"var-remera-basica-blanca-m","origen":"FISICO","cantidad":2}'

# Reponer al gimnasio (descuenta el físico; el envío sigue en segundo plano)
curl localhost:8080/inventario/v1/reposiciones -H "$A" -H "Idempotency-Key: repo-0001" \
  -d '{"varianteId":"var-remera-basica-negra-l","cantidad":1}'

# Stock de FAV en el gimnasio
curl localhost:8080/inventario/v1/consignacion -H "$A"
```

Flujo de punta a punta de la capacidad publicada (el mock de Ventas reserva en Inventario):

```bash
curl localhost:8080/v1/pedidos -H "X-API-Key: clave-grupo-proveedores" -H "Idempotency-Key: pedido-0001" \
  -H "Content-Type: application/json" \
  -d '{"items":[{"varianteId":"var-remera-basica-blanca-m","cantidad":3}]}'
```

## API

| Método y ruta | Quién la usa | Qué hace |
|---|---|---|
| `POST /v1/reservas` | Ventas | Aparta stock online, todo o nada. `Idempotency-Key` obligatoria. Cuerpo: `referencia`, `vigenciaMinutos` (20 o 2880), `items` |
| `GET /v1/reservas/{id}` | Ventas | Estado de la reserva |
| `POST /v1/reservas/{id}/confirmar` | Ventas | Descuenta definitivamente, solo si no venció (`409 RESERVA_VENCIDA`) |
| `POST /v1/reservas/{id}/liberar` | Ventas | Devuelve las unidades (cancelación o pago rechazado) |
| `GET /v1/stock?variantes=a,b` | Panel | Stock online, físico, reservado y disponible por variante |
| `POST /v1/ingresos` | Panel | Suma unidades a un canal; da de alta la variante si no existe |
| `POST /v1/transferencias` | Panel | Mueve unidades entre canales en una transacción |
| `POST /v1/reposiciones` | Panel | Envía prendas al gimnasio (`202`) |
| `GET /v1/consignacion` | Panel | Stock de FAV en el gimnasio (`503 GIMNASIO_NO_DISPONIBLE` si no responde) |
| `GET /healthz`, `/readyz`, `/metrics` | Gateway, Prometheus | Vida, disponibilidad (con la base) y métricas |

Los errores siguen RFC 9457 (`application/problem+json`) con un `codigo` estable: `PEDIDO_INVALIDO`, `STOCK_INSUFICIENTE` (con `faltantes`), `VARIANTE_INEXISTENTE`, `CLAVE_IDEMPOTENCIA_REUTILIZADA`, `RESERVA_NO_ENCONTRADA`, `RESERVA_VENCIDA`, `RESERVA_CONFIRMADA`, `RESERVA_LIBERADA`, `GIMNASIO_NO_DISPONIBLE`, `NO_DISPONIBLE`, `ERROR_INTERNO`.

## Cómo cumple las garantías

- **Sin sobreventa:** `UPDATE stock SET reservado = reservado + n WHERE online - reservado >= n`, dentro de una transacción. Los ítems se procesan ordenados por variante para que dos reservas no se bloqueen en cruz.
- **Sin duplicados:** la clave de idempotencia tiene restricción única (`reservas`, `operaciones`, `reposiciones`). Un reintento devuelve el resultado anterior; la misma clave con otro pedido responde `409`.
- **Pago contra vencimiento:** confirmar y vencer bloquean la fila de la reserva (`FOR UPDATE`); gana una sola.
- **Procesos en dos réplicas:** el vencimiento bloquea cada reserva; las reposiciones se toman con `FOR UPDATE SKIP LOCKED`.
- **Gimnasio caído:** timeout de 2 s y circuit breaker (se abre tras 5 fallas seguidas y prueba de nuevo a los 30 s). Las reposiciones se reintentan con espera creciente (5 s a 5 min).
- **Evento `ReservaVencida`:** se guarda en la tabla `outbox` en la misma transacción. El publicador hacia RabbitMQ queda para la entrega siguiente.

## Observabilidad

- **Logs** JSON en stdout con `servicio`, `instancia`, `correlation_id`, `trace_id` y `span_id`.
- **Trazas** OpenTelemetry → Jaeger (`http://localhost:16686`): HTTP entrante y saliente y cada consulta SQL.
- **Métricas** Prometheus (`http://localhost:9090`): `http_pedidos_total`, `http_duracion_segundos`, `dependencia_llamadas_total`, `circuit_breaker_estado`, `proceso_elementos_total`.

## Tests

```bash
go test ./services/inventario/...                       # dominio
docker compose up -d mysql-inventario                   # integración y concurrencia contra MySQL
INVENTARIO_TEST_DSN="inventario:cambiar@tcp(localhost:3307)/inventario?parseTime=true&loc=UTC&multiStatements=true" \
  go test ./services/inventario/...
```

## Configuración

| Variable | Por defecto | |
|---|---|---|
| `INVENTARIO_DB_HOST`, `_NAME`, `_USER`, `_PASSWORD` | `mysql-inventario:3306`, `inventario` | Conexión a MySQL |
| `INVENTARIO_DATOS_EJEMPLO` | `false` (`true` en Compose) | Carga stock de las variantes del contrato |
| `GIMNASIO_URL`, `GIMNASIO_API_KEY`, `GIMNASIO_PROVEEDOR_ID`, `GIMNASIO_TIMEOUT_MS` | mock, —, `fav`, `2000` | Capacidad consumida |
| `VENCIMIENTO_CADA_MS`, `REPOSICIONES_CADA_MS` | `10000`, `5000` | Frecuencia de los procesos |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | — | Sin valor, no exporta trazas |
| `LOG_LEVEL` | `info` | `debug` para más detalle |

## Más información

[Arquitectura §4 y §7](../../docs/ARCHITECTURE.md) · [ADR-001](../../docs/adr/ADR-001-limites-de-los-servicios.md) · [ADR-003](../../docs/adr/ADR-003-persistencia.md) · [ADR-005](../../docs/adr/ADR-005-comunicacion.md) · [ADR-002](../../docs/adr/ADR-002-arquitectura-interna.md) · [ADR-004](../../docs/adr/ADR-004-consistencia-y-concurrencia.md) · [ADR-009](../../docs/adr/ADR-009-consumo-del-gimnasio.md) · [ADR-010](../../docs/adr/ADR-010-resiliencia.md) · [ADR-011](../../docs/adr/ADR-011-observabilidad.md) · [ADR-012](../../docs/adr/ADR-012-balanceo.md)
