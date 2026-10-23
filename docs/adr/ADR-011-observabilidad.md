# ADR-011 · Observabilidad

| | |
|---|---|
| **Estado** | Aceptada (versión inicial) · Entrega 2. Tablero, alerta y retención se completan en la entrega de observabilidad. |
| **Fecha** | 23/10/2026 |
| **Decisión de la consigna** | D11 · Observabilidad |
| **Relacionadas** | [ADR-005](ADR-005-comunicacion.md) (identificador de correlación, DLQ con alerta), ADR-010 (resiliencia: circuit breaker hacia el gimnasio), [ADR-012](ADR-012-balanceo.md) (reparto entre réplicas) |

> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar.

## Contexto

El SPEC pide dos cosas que no se resuelven mirando cada servicio por separado:

- **RNF-02 · Diagnóstico:** ante una falla, poder seguir la operación entre servicios. Logs estructurados con identificador de correlación y trazas del flujo de compra.
- **RNF-03 · Métricas con historial:** pedidos, errores por código HTTP, latencia y dependencias, consultables hacia atrás ("cuántos 404 hubo la semana pasada") en un tablero con al menos una alerta.

En la Entrega 2 corren Inventario (real, dos réplicas), el gateway (nginx) y los mocks de Catálogo/Ventas y del gimnasio. Un pedido de la capacidad publicada atraviesa gateway → mock de Ventas → gateway → una réplica de Inventario → MySQL. La consigna de esta entrega pide **logs y traza**; el tablero y la alerta corresponden a la entrega de observabilidad.

## Criterios

1. **Seguir un pedido de punta a punta** con un solo identificador, desde el log hasta la traza.
2. **Estándares abiertos:** que cambiar de herramienta no obligue a tocar el código de los servicios.
3. **Herramientas existentes**, sin implementaciones propias (mismo criterio que RNF-05).
4. **Se levanta con `docker compose up`**, sin configuración manual.
5. **Distinguir réplicas:** ver qué réplica atendió cada pedido (ADR-012).

## Decisión

### 1. Herramientas

| Señal | Cómo se genera | Dónde se ve |
|---|---|---|
| **Logs** | JSON en stdout (`log/slog` en Go, `log_format json` en nginx) | `docker compose logs` |
| **Trazas** | **OpenTelemetry** (SDK de Go, `otelhttp` y `otelsql`), exportadas por OTLP/HTTP | **Jaeger** all-in-one, http://localhost:16686 |
| **Métricas** | `prometheus/client_golang`, expuestas en `GET /metrics` | **Prometheus**, http://localhost:9090 |

### 2. Logs y correlación

- Cada línea lleva `servicio`, `instancia` (la réplica), `correlation_id` y, si hay traza, `trace_id` y `span_id`. Con el `trace_id` de un log se abre la traza en Jaeger.
- El `X-Correlation-Id` se respeta si viene en el pedido. Si no, lo genera el gateway (o el servicio, si se lo llama directo). Viaja en cada llamada saliente y vuelve en la respuesta (ADR-005, punto 7).
- El gateway registra, por pedido, la réplica que lo atendió (`upstream`), su código y su duración.
- Las sondas (`/healthz`, `/readyz`, `/metrics`) no se loguean, para no tapar el tráfico real.

### 3. Trazas

- Propagación con **W3C Trace Context** (`traceparent`). nginx reenvía el encabezado sin tocarlo, así la traza del mock de Ventas y la de Inventario quedan unidas aunque pasen por el gateway.
- Spans: un span por pedido HTTP entrante, uno por llamada HTTP saliente (al gimnasio, a Inventario) y uno por consulta a MySQL.
- Las consultas de los procesos periódicos solo se trazan si forman parte de una operación (vencer una reserva, enviar una reposición), para no llenar Jaeger de trazas sueltas.
- Sin `OTEL_EXPORTER_OTLP_ENDPOINT` el servicio no exporta, pero igual propaga el contexto. Así los tests no necesitan Jaeger.

### 4. Métricas de Inventario

| Métrica | Tipo | Etiquetas | Responde |
|---|---|---|---|
| `http_pedidos_total` | Contador | `ruta`, `metodo`, `codigo` | Pedidos y errores por código (por ejemplo, los 404) |
| `http_duracion_segundos` | Histograma | `ruta`, `metodo` | Latencia (p50, p95, p99) |
| `dependencia_llamadas_total` | Contador | `dependencia`, `resultado` | Fallas del gimnasio: `ok`, `error`, `rechazada`, `circuito_abierto` |
| `circuit_breaker_estado` | Gauge | `dependencia` | 0 cerrado, 1 semiabierto, 2 abierto (ADR-010) |
| `proceso_elementos_total` | Contador | `proceso` | Reservas vencidas y reposiciones enviadas |

La etiqueta `ruta` es el **patrón** registrado (`/v1/reservas/{id}`), no la URL, para no crear una serie por cada id.

Prometheus descubre las réplicas por el DNS de Docker (`dns_sd_configs` sobre `inventario`) y scrapea cada una por separado cada 5 s. La etiqueta `instance` muestra el reparto de carga del ADR-012.

## Alternativas consideradas

| Alternativa | Por qué se descartó |
|---|---|
| **Instrumentar directo con la librería de Jaeger** (sin OpenTelemetry) | Las librerías propias de Jaeger están discontinuadas en favor de OpenTelemetry. Con OTLP se puede cambiar Jaeger por Tempo u otro backend cambiando solo la variable de entorno. |
| **Zipkin** | Similar a Jaeger. Jaeger recibe OTLP de forma nativa y su interfaz para comparar trazas es más completa. |
| **Grafana Tempo + Loki** (trazas y logs) | Más piezas para levantar y configurar en esta entrega. Se puede reconsiderar si se suma Grafana. |
| **ELK / OpenSearch para logs** | Demasiado pesado para el volumen de FAV y para una máquina de desarrollo. Con JSON en stdout, los logs pueden mandarse a cualquiera de estos más adelante sin tocar el código. |
| **Métricas por OTLP en lugar de Prometheus** | Prometheus es el estándar para métricas con historial y consultas como "errores 404 de la semana pasada". El scrape no obliga a los servicios a conocer dónde se guardan las métricas. |
| **Un identificador propio en lugar del `trace_id`** | Se mantienen los dos: el `correlation_id` lo puede mandar el cliente y aparece también en eventos (ADR-005); el `trace_id` es el que entiende Jaeger. Los logs llevan ambos. |

## Consecuencias

**Positivas**

- Un pedido se sigue entre gateway, mock de Ventas, réplica de Inventario, MySQL y gimnasio con un solo `trace_id`.
- Los servicios no dependen de Jaeger ni de Prometheus para funcionar: si caen, se pierden señales pero no pedidos.
- Se ve qué réplica atendió cada pedido y cómo se reparte la carga.
- Cambiar de backend de trazas no requiere cambiar código.

**Negativas aceptadas**

- **Jaeger all-in-one guarda las trazas en memoria:** se pierden al reiniciar el contenedor. Alcanza para diagnosticar en el momento, no para auditoría.
- **Prometheus sin volumen:** el historial se pierde al bajar el contenedor. Hasta resolverlo, RNF-03 queda cumplido a medias.
- **El gateway no genera spans propios.** Aparece en los logs (con correlación y réplica) pero no como un paso en la traza; su tiempo queda incluido en el del span que lo llama.
- **Los logs no se centralizan:** se consultan con `docker compose logs`, filtrando por `correlation_id`.

## Pendientes

Para la entrega de observabilidad:

- **Tablero:** Grafana **[propuesta]** con Prometheus como fuente: pedidos por código, latencia p95, estado del circuit breaker y reparto entre réplicas.
- **Alerta:** al menos una. **[propuesta]** tasa de 5xx de Inventario mayor al 5 % durante 1 minuto, o circuit breaker del gimnasio abierto. Más adelante, mensajes en la DLQ (ADR-005).
- **Retención:** volumen para Prometheus y `--storage.tsdb.retention.time` de al menos 15 días **[propuesta]** (RNF-03 pide dos semanas).
- **Almacenamiento persistente de trazas** **[?]**, si hace falta revisarlas después de reiniciar.
- **Métricas de caché** (RNF-05, ADR-007) y de RabbitMQ, cuando existan.
- Instrumentar Catálogo, Ventas y Usuarios con el mismo paquete cuando dejen de ser mocks.

## Validación prevista

- **Entrega 2:** un pedido a `POST /v1/pedidos` muestra en Jaeger una sola traza que pasa por el mock de Ventas, Inventario y MySQL, y el mismo `correlation_id` aparece en los logs del gateway, del mock y de la réplica que atendió.
- **Entrega 2:** en Prometheus, `http_pedidos_total` muestra los pedidos separados por réplica (`instance`) y por código.
- **Entrega 2:** con el mock del gimnasio fallando, `dependencia_llamadas_total{resultado="error"}` crece y `circuit_breaker_estado` pasa a 2.
- **Entrega de observabilidad:** el tablero responde "cuántos 404 hubo la semana pasada" y la alerta se dispara provocando errores 5xx.

---

**Si la decisión cambia:** no se borra este archivo. Se marca como *Reemplazada por ADR-YYY* y el nuevo ADR indica a cuál reemplaza y por qué hubo que revisarla.
