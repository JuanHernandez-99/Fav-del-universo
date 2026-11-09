# ADR-011 · Observabilidad

| | |
|---|---|
| **Estado** | Aceptada · Tablero, alertas y retención implementados y probados en local. Faltan las trazas de Catálogo y el aviso real en la nube. |
| **Fecha** | 23/10/2026, actualizada el 09/11/2026 |
| **Decisión de la consigna** | D11 · Observabilidad |
| **Relacionadas** | [ADR-005](ADR-005-comunicacion.md) (identificador de correlación, DLQ con alerta), ADR-010 (resiliencia: circuit breaker hacia el gimnasio), [ADR-012](ADR-012-balanceo.md) (reparto entre réplicas) |

> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar.

> **Actualización 04/11/2026:** Ventas ya es real y usa el mismo paquete de observabilidad. Catálogo sigue sin trazas ni métricas. Ver la validación al final.

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
| **Métricas** | `prometheus/client_golang`, expuestas en `GET /metrics` | **Prometheus**, http://localhost:9090, con 15 días de historial en un volumen |
| **Tablero** | Grafana lee de Prometheus. El tablero es un JSON versionado que se carga solo al levantar | **Grafana**, http://localhost:3000 |
| **Alertas** | Prometheus evalúa las reglas (`observabilidad/alertas.yml`) y Alertmanager avisa por mail | **Alertmanager**, http://localhost:9093, y Mailpit en local |

La consigna pide tablero y alerta, sin fijar herramientas: son decisión del grupo.

### 2. Logs y correlación

- Cada línea lleva `servicio`, `instancia` (la réplica), `correlation_id` y, si hay traza, `trace_id` y `span_id`. Con el `trace_id` de un log se abre la traza en Jaeger.
- El `X-Correlation-Id` se respeta si viene en el pedido. Si no, lo genera el gateway (o el servicio, si se lo llama directo). Viaja en cada llamada saliente y vuelve en la respuesta (ADR-005, punto 7).
- El gateway registra, por pedido, la réplica que lo atendió (`upstream`), su código y su duración.
- Las sondas (`/healthz`, `/readyz`, `/metrics`) no se loguean, para no tapar el tráfico real.

### 3. Trazas

- Propagación con **W3C Trace Context** (`traceparent`). nginx reenvía el encabezado sin tocarlo, así la traza de Ventas y la de Inventario quedan unidas aunque pasen por el gateway.
- Spans: un span por pedido HTTP entrante, uno por llamada HTTP saliente (al gimnasio, a Inventario) y uno por consulta a MySQL.
- Las consultas de los procesos periódicos solo se trazan si forman parte de una operación (vencer una reserva, enviar una reposición), para no llenar Jaeger de trazas sueltas.
- Sin `OTEL_EXPORTER_OTLP_ENDPOINT` el servicio no exporta, pero igual propaga el contexto. Así los tests no necesitan Jaeger.

### 4. Métricas de Inventario y Ventas

| Métrica | Tipo | Etiquetas | Responde |
|---|---|---|---|
| `http_pedidos_total` | Contador | `ruta`, `metodo`, `codigo` | Pedidos y errores por código (por ejemplo, los 404) |
| `http_duracion_segundos` | Histograma | `ruta`, `metodo` | Latencia (p50, p95, p99) |
| `dependencia_llamadas_total` | Contador | `dependencia`, `resultado` | Fallas del gimnasio: `ok`, `error`, `rechazada`, `circuito_abierto` |
| `circuit_breaker_estado` | Gauge | `dependencia` | 0 cerrado, 1 semiabierto, 2 abierto (ADR-010) |
| `proceso_elementos_total` | Contador | `proceso` | Reservas vencidas, reposiciones y eventos enviados; en Ventas, tareas ejecutadas y mails enviados |
| `mensajes_consumidos_total` | Contador | `resultado` | Solo Ventas: `ok`, `reintento` y `fallido` (los que van a la DLQ) |
| `reposiciones_por_estado` | Gauge | `estado` | Solo Inventario: reposiciones en cada estado (ADR-018) |
| `reposicion_pendiente_antiguedad_segundos` | Gauge | — | Solo Inventario: cuánto espera la pendiente más antigua |

La etiqueta `ruta` es el **patrón** registrado (`/v1/reservas/{id}`), no la URL, para no crear una serie por cada id.

Prometheus descubre las réplicas por el DNS de Docker (`dns_sd_configs` sobre `inventario` y `ventas`) y scrapea cada una por separado cada 5 s. La etiqueta `instance` muestra el reparto de carga del ADR-012.

### 5. Objetivo de servicio **[propuesta]**

| Indicador | Objetivo |
|---|---|
| Pedidos sin error del servidor (5xx) | Al menos 99 % |
| Latencia p95 | Menos de 300 ms |

Son valores propuestos, sin medición que los respalde en Inventario y Ventas. Se contrastan con la prueba de carga (ADR-013).

### 6. Tablero y alertas

El tablero `FAV · Estado general` muestra réplicas, alertas activas, tráfico, errores (incluidos los 404 de 7 días), p95, reparto del balanceo, caché, mensajería y gimnasio.

Los umbrales son **[propuesta]**:

| Alerta | Se dispara cuando | Severidad |
|---|---|---|
| `ReplicaCaidaInventario`, `ReplicaCaidaCatalogo` | Menos de 2 réplicas responden, durante 30 s | Alta |
| `VentasCaido` | Ventas no responde, durante 30 s | Crítica |
| `UsuariosCaido`, `RabbitMQCaido` | No responde, durante 30 s | Alta |
| `ErroresDelServidor` | Más del 5 % de 5xx en un servicio, durante 1 minuto. No cuenta el `503` del stock del gimnasio, porque es una degradación prevista | Crítica |
| `LatenciaAlta` | p95 de un servicio mayor a 300 ms, durante 2 minutos | Alta |
| `CircuitoGimnasioAbierto` | Circuit breaker abierto, durante 30 s | Alta |
| `ReposicionBloqueada` | El gimnasio rechaza nuestra credencial | Crítica |
| `ReposicionARevisar` | Hay una reposición en conflicto | Alta |
| `ReposicionPendienteAntigua` | Una reposición espera hace más de 10 minutos | Alta |
| `MensajesEnColaDeFallidos` | Hay mensajes en una DLQ | Alta |
| `CacheNoDisponible` | La caché falla en las lecturas, durante 1 minuto | Media |
| `InvalidacionDeCacheFallida` | Falló una invalidación en los últimos 5 minutos | Media |

**Por qué 5 % si el objetivo es 99 %:** la alerta avisa de una falla en curso. Con el objetivo exacto, unos pocos errores con poco tráfico avisarían sin que pase nada grave.

## Alternativas consideradas

| Alternativa | Por qué se descartó |
|---|---|
| **Instrumentar directo con la librería de Jaeger** (sin OpenTelemetry) | Las librerías propias de Jaeger están discontinuadas en favor de OpenTelemetry. Con OTLP se puede cambiar Jaeger por Tempo u otro backend cambiando solo la variable de entorno. |
| **Zipkin** | Similar a Jaeger. Jaeger recibe OTLP de forma nativa y su interfaz para comparar trazas es más completa. |
| **Grafana Tempo + Loki** (trazas y logs) | Más piezas para levantar y configurar en esta entrega. Se puede reconsiderar si se suma Grafana. |
| **Guardar los logs en MySQL** (lo que se usó en clase) | Se podrían consultar con SQL, pero una caída de la base se llevaría los logs justo cuando hacen falta: en el [ensayo](../POSTMORTEM.md), los errores de conexión de MySQL se pudieron leer porque iban a stdout. Además, cada pedido sumaría una escritura a una base que atiende el negocio. **Costo aceptado:** los logs no quedan centralizados. |
| **ELK / OpenSearch para logs** | Demasiado pesado para el volumen de FAV y para una máquina de desarrollo. Con JSON en stdout, los logs pueden mandarse a cualquiera de estos más adelante sin tocar el código. |
| **Métricas por OTLP en lugar de Prometheus** | Prometheus es el estándar para métricas con historial y consultas como "errores 404 de la semana pasada". El scrape no obliga a los servicios a conocer dónde se guardan las métricas. |
| **La interfaz de Prometheus como tablero** | Sirve para consultas sueltas, no para ver el estado general de una mirada. |
| **Alertas de Grafana en lugar de Alertmanager** | Las reglas quedarían atadas al tablero. Así, si Grafana cae, se sigue evaluando y avisando. |
| **Un identificador propio en lugar del `trace_id`** | Se mantienen los dos: el `correlation_id` lo puede mandar el cliente y aparece también en eventos (ADR-005); el `trace_id` es el que entiende Jaeger. Los logs llevan ambos. |

## Consecuencias

**Positivas**

- Un pedido se sigue entre Ventas, una réplica de Inventario y MySQL con un solo `trace_id`, y también por `correlation_id` en los logs del gateway.
- Los servicios no dependen de Jaeger ni de Prometheus para funcionar: si caen, se pierden señales pero no pedidos.
- Se ve qué réplica atendió cada pedido y cómo se reparte la carga.
- Cambiar de backend de trazas no requiere cambiar código.

**Negativas aceptadas**

- **Jaeger all-in-one guarda las trazas en memoria:** se pierden al reiniciar el contenedor. Alcanza para diagnosticar en el momento, no para auditoría.
- **El historial se pierde con `docker compose down -v`,** porque borra el volumen de Prometheus.
- **Una falla puede avisar varias veces:** con MySQL caído avisaron `ErroresDelServidor` y, con el servicio ya recuperado, `LatenciaAlta`. Las alertas se agrupan por nombre y no se silencian entre sí.
- **`ReposicionARevisar` no se apaga sola:** el sistema no tiene cómo resolver ese conflicto (ADR-018) y hay que corregirlo en la base.
- **Las alertas de reposiciones tardan 1 minuto más en apagarse** (`keep_firing_for`). Así no se resuelven en falso cuando la métrica falta al reiniciar Inventario.
- **`ReposicionBloqueada` puede tardar más de 5 minutos en resolverse** después de corregir la clave, por la espera entre reintentos (ADR-018).
- **El gateway no genera spans propios.** Aparece en los logs (con correlación y réplica) pero no como un paso en la traza; su tiempo queda incluido en el del span que lo llama.
- **Los logs no se centralizan:** se consultan con `docker compose logs`, filtrando por `correlation_id`.

## Pendientes

- **Confirmar el objetivo de servicio** con la prueba de carga (ADR-013).
- **Destino real de las alertas en la nube:** **[?]** se resuelve con el despliegue. Hoy llegan a Mailpit.
- **Almacenamiento persistente de trazas** **[?]**, si hace falta revisarlas después de reiniciar.
- Trazas en Catálogo. Mientras tanto, las llamadas a Catálogo aparecen en la traza solo del lado de Ventas.

## Validación prevista

- **Entrega 2:** un pedido a `POST /v1/pedidos` muestra en Jaeger una sola traza que pasa por el mock de Ventas, Inventario y MySQL, y el mismo `correlation_id` aparece en los logs del gateway, del mock y de la réplica que atendió.
- **Entrega 2:** en Prometheus, `http_pedidos_total` muestra los pedidos separados por réplica (`instance`) y por código.
- **Entrega 2:** con el mock del gimnasio fallando, `dependencia_llamadas_total{resultado="error"}` crece y `circuit_breaker_estado` pasa a 2.
- **Entrega de observabilidad:** el tablero responde "cuántos 404 hubo la semana pasada" y la alerta se dispara provocando errores 5xx.

## Validación con Ventas real (04/11/2026)

| Comprobación | Resultado |
|---|---|
| Un `POST /v1/pedidos` con un `X-Correlation-Id` | El id aparece en los logs del gateway, de Ventas y de Inventario |
| La traza de ese pedido en Jaeger | Una sola traza con Ventas e Inventario (24 spans). Catálogo no aparece: no está instrumentado |
| Prometheus | Inventario (cada réplica por separado) y Ventas en estado `up`. Catálogo no expone métricas |
| Id generado por el gateway | Llega a Inventario desde el panel. En Catálogo no se puede comprobar: no lo usa todavía |

## Validación de tablero y alertas (09/11/2026)

`observabilidad/probar_alertas.sh` provoca fallas reales en local y comprueba cada ciclo: la alerta se dispara, llega el mail y, al restaurar, llega el aviso de resuelta.

| Comprobación | Resultado |
|---|---|
| Las 14 alertas | Ciclo completo. 11 en la corrida completa; `CacheNoDisponible`, `InvalidacionDeCacheFallida` y `ReposicionBloqueada`, en una repetición tras corregirlas |
| Tiempo hasta dispararse | Entre unos 30 s y 2,5 minutos, según la alerta |
| `CircuitoGimnasioAbierto` | Se disparó con el gimnasio caído: el circuit breaker llegó a abierto |
| Historial | Sobrevivió a recrear el contenedor de Prometheus (08/11) |
| Grafana | Responde y carga el tablero. Sus consultas no dieron error (08/11) |

**Encontrado y corregido en la prueba:**

- **`InvalidacionDeCacheFallida` no veía una falla aislada:** la métrica nacía en 1 y Prometheus no lo cuenta como aumento. Ahora arranca en 0.
- **`ReposicionBloqueada` se resolvía en falso** al reiniciar Inventario. Se agregó `keep_firing_for`. `ReposicionARevisar` y `ReposicionPendienteAntigua` se repitieron con ese cambio y pasaron.

**Sin explicar:** en la corrida completa no llegó el mail de disparo de `CacheNoDisponible`, sin errores de envío en Alertmanager. En la repetición llegó. **La causa no se confirmó.**

**Sin probar:** el tablero en el navegador, los 15 días reales de historial y el aviso fuera de Mailpit. Las pruebas de `ReposicionARevisar` y `ReposicionPendienteAntigua` cambian datos en la base, porque no se puede provocar de otra forma el final del conflicto ni esperar 10 minutos.

---

**Si la decisión cambia:** no se borra este archivo. Se marca como *Reemplazada por ADR-YYY* y el nuevo ADR indica a cuál reemplaza y por qué hubo que revisarla.
