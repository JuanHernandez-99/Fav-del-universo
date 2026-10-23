# ADR-011 · Observabilidad

|                             |                                                                                                                                                                                                              |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Estado**                  | Aceptada (primera versión) · Entrega 2. Logs, trazas y métricas funcionan en Inventario y el gateway. **Faltan** el tablero, las alertas y los objetivos de servicio medidos.                                  |
| **Fecha**                   | 23/10/2026                                                                                                                                                                                                   |
| **Decisión de la consigna** | D11 · Observabilidad                                                                                                                                                                                         |
| **Relacionadas**            | [ADR-005](ADR-005-comunicacion.md) (correlación), [ADR-010](ADR-010-resiliencia.md) (qué mecanismos se miden), [ADR-012](ADR-012-balanceo-de-carga.md) (evidencia del reparto), [ADR-013](ADR-013-capacidad-y-costos.md) (pruebas de carga) |

> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar · _previsto_: forma parte del diseño pero todavía no está implementado.

## Contexto

La consigna (requisito 2.10) pide poder **comprender el estado de una operación sin inspeccionar cada componente a mano**, con como mínimo logs estructurados, métricas y un tablero. Para esta entrega pide logs estructurados y correlacionados y una **primera traza distribuida** del flujo principal; para la presentación, logs, métricas y trazas completos, con tablero y alerta. El SPEC lo traduce en RNF-02 (diagnóstico) y RNF-03 (métricas con historial, por ejemplo cuántos `404` hubo la semana pasada).

El flujo principal de esta entrega es el pedido de la capacidad publicada: **consumidor → gateway → mock de Ventas → Inventario → MySQL**.

## Criterios

1. **Seguir una operación entre servicios** con un identificador común.
2. **Pasar del log a la traza** y de la traza al log.
3. **Estándares abiertos** (OpenTelemetry, Prometheus) en lugar de formatos propios.
4. **Que se levante con el mismo comando** que el sistema (RNF-07), sin configuración manual.
5. **Poco costo operativo** para un volumen bajo.

## Decisión

### Logs

JSON por la salida estándar (`slog`). Cada línea de Inventario lleva:

| Campo                              | Qué es                                                         |
| ---------------------------------- | -------------------------------------------------------------- |
| `servicio`, `instancia`            | Quién escribió (la instancia es el nombre del contenedor)      |
| `correlation_id`                   | Identificador del pedido, común a todos los servicios          |
| `trace_id`, `span_id`              | Enlace a la traza                                              |
| `metodo`, `ruta`, `codigo`, `duracion_ms` | Una línea por pedido HTTP (las sondas `/healthz`, `/readyz` y `/metrics` no se registran ni se cuentan en las métricas) |

- El gateway escribe su propio log de acceso en JSON, con `correlation_id`, `codigo`, `duracion_s` y la **réplica que atendió** (`upstream`).
- Los pedidos con código `5xx` se registran con nivel `ERROR`; el resto, con nivel `INFO`.

### Correlación

El gateway toma el `X-Correlation-Id` del cliente o, si falta, usa el identificador del pedido de nginx (`$request_id`). Lo reenvía a los servicios y lo devuelve en la respuesta, también en sus propios errores (`401`, `429`). Inventario y el mock de Ventas lo toman del encabezado, lo ponen en el contexto del pedido, lo agregan a cada llamada saliente y lo guardan en el **outbox**. Los procesos periódicos generan uno propio por elemento (`proceso-<uuid>`).

### Trazas

OpenTelemetry con exportación OTLP/HTTP a **Jaeger** (`http://localhost:16686`). Se instrumenta el servidor HTTP, los clientes HTTP salientes (propagan el encabezado de la traza) y **cada consulta SQL**. Los procesos periódicos abren una traza (`proceso <nombre>`) por cada elemento que procesan; las búsquedas de trabajo que no encuentran nada no se trazan, para no llenar Jaeger de trazas vacías. Cada span del pedido lleva el atributo `correlation_id`, así que se puede buscar una traza por el mismo identificador del log.

Una traza del flujo principal muestra: `ventas-mock` (`POST /v1/pedidos`) → llamada saliente → `inventario` (`POST /v1/reservas`) → consultas SQL.

### Métricas

Prometheus recolecta cada 5 s. Descubre **todas las réplicas** de Inventario por el DNS de Docker, así cada una es un objetivo distinto.

| Métrica                                         | Etiquetas                    | Para qué                                                  |
| ----------------------------------------------- | ---------------------------- | --------------------------------------------------------- |
| `http_pedidos_total`                            | `ruta`, `metodo`, `codigo`   | Pedidos y errores por código (por ejemplo, cuántos `404`) |
| `http_duracion_segundos` (histograma)           | `ruta`, `metodo`             | Latencia y percentiles                                    |
| `dependencia_llamadas_total`                    | `dependencia`, `resultado`   | Llamadas al gimnasio: `ok`, `error`, `rechazada`, `circuito_abierto` |
| `circuit_breaker_estado`                        | `dependencia`                | 0 cerrado, 1 semiabierto, 2 abierto                       |
| `proceso_elementos_total`                       | `proceso`                    | Reservas vencidas y reposiciones enviadas                 |

La ruta se toma del patrón registrado (por ejemplo `/v1/reservas/{id}`), no de la URL real, para no crear una serie por cada identificador.

### Objetivos de servicio y alertas **[propuesta]**

Los objetivos se confirman con las mediciones de la prueba de carga (ADR-013). Valores iniciales:

| Indicador                                         | Objetivo **[propuesta]**                                  |
| ------------------------------------------------- | --------------------------------------------------------- |
| Disponibilidad de las operaciones de Inventario   | 99 % de las respuestas sin `5xx` en una ventana de 30 días |
| Latencia de reservar y confirmar                  | p95 menor a 300 ms                                        |
| Reposiciones al gimnasio                          | Ninguna pendiente por más de 1 hora                       |

Reglas de alerta propuestas (todavía no están cargadas en Prometheus):

```yaml
groups:
  - name: inventario
    rules:
      - alert: ReplicaCaida
        # Un objetivo caído desaparece de la lista del DNS, por eso se cuenta cuántos hay arriba.
        expr: (count(up{job="inventario"} == 1) or vector(0)) < 2
        for: 1m
      - alert: ErroresAltos
        expr: sum(rate(http_pedidos_total{codigo=~"5.."}[2m])) / sum(rate(http_pedidos_total[2m])) > 0.05
        for: 2m
      - alert: LatenciaAlta
        expr: histogram_quantile(0.95, sum by (le) (rate(http_duracion_segundos_bucket[5m]))) > 1
        for: 5m
      - alert: CircuitoDelGimnasioAbierto
        expr: circuit_breaker_estado{dependencia="gimnasio"} == 2
        for: 1m
```

Falta una métrica de reposiciones pendientes y antiguas para el tercer objetivo. Cuando exista la cola de mensajes fallidos se suma una alerta por mensajes en ella.

### Tablero **[propuesta]**

**Grafana**, con el origen de datos y los paneles definidos en archivos del repositorio, para que se levante con el sistema. Paneles: pedidos por segundo y por código, latencia p95, estado de cada réplica, reparto de pedidos entre réplicas, estado del circuito del gimnasio, reservas vencidas y reposiciones enviadas. **No está implementado.**

## Alternativas consideradas

| Alternativa                                                      | Por qué se descartó                                                                                                                                                 |
| ---------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Logs de texto libre**                                          | No se pueden filtrar por `correlation_id` ni por código sin parsearlos.                                                                                              |
| **Pila ELK o Loki para los logs**                                | Para esta entrega alcanza con `docker compose logs`. Se evalúa Loki si la lectura de logs se vuelve un problema real.                                                |
| **Zipkin o Tempo en lugar de Jaeger**                            | Equivalentes para este uso. Jaeger se levanta con un solo contenedor (`all-in-one`) y OpenTelemetry exporta a cualquiera de ellos.                                   |
| **Formato propio de trazas o solo `correlation_id`**             | El identificador permite unir logs, pero no muestra la duración de cada paso ni las consultas SQL.                                                                   |
| **Instrumentar con el agente de un proveedor (Datadog, etc.)**   | Dependencia de un servicio externo pago, innecesaria para el alcance.                                                                                               |
| **Métricas empujadas (push) en lugar de recolectadas**           | Prometheus recolecta, y descubrir las réplicas por DNS permite ver el reparto de carga.                                                                              |

## Consecuencias

**Positivas**

- Un pedido se puede seguir desde el gateway hasta la consulta SQL con un solo identificador.
- Las métricas por réplica son la evidencia del balanceo (ADR-012).
- Todo se levanta con `docker compose up`, sin configuración manual.

**Negativas aceptadas**

- **El gateway no crea un span.** nginx de código abierto reenvía el encabezado de la traza, pero no genera la suya; la traza empieza en el primer servicio. El gateway se sigue por su log y el `correlation_id`.
- **Jaeger y Prometheus no tienen volumen:** se pierde el historial al apagar el Compose. Para cumplir RNF-03 (historial de al menos dos semanas) hay que agregarles almacenamiento persistente y definir la retención.
- **Jaeger guarda las trazas en memoria** (`all-in-one`).
- **No hay tablero ni alertas cargadas todavía.**
- **El `circuit_breaker_estado` recién aparece cuando el circuito cambia de estado.**
- **Los logs no están centralizados:** hay que pedirlos a cada contenedor.

## Pendientes

- **Tablero en Grafana y alertas cargadas** (para la presentación; la consigna las exige).
- **Persistencia y retención** de Prometheus (y de Jaeger, si hace falta).
- **Instrumentar el resto de los servicios** con el mismo paquete de observabilidad cuando existan.
- **Métrica de reposiciones pendientes y antiguas.**
- **Confirmar los objetivos de servicio** con las pruebas de carga.
- **[?]** Herramientas definitivas: el SPEC las deja abiertas hasta el teórico de observabilidad.

## Validación

- Mandar un pedido con `X-Correlation-Id: demo-trazas-1` y buscar ese valor en `docker compose logs`: aparece en el gateway, en el mock de Ventas y en Inventario.
- En Jaeger, buscar por `correlation_id=demo-trazas-1`: una traza con ambos servicios y las consultas SQL.
- En Prometheus: `up{job="inventario"}` devuelve las dos réplicas, y `sum by (instance) (http_pedidos_total)` muestra el reparto.
- Provocar un `404` y un `409` y verlos en `http_pedidos_total` con su código.
