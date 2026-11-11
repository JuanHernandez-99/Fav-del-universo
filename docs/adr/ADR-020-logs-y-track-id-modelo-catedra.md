# ADR-020 · Logs en MySQL y track id (modelo de la cátedra)

| | |
|---|---|
| **Estado** | Aceptada · Implementada en local y en la nube. Validada en local |
| **Fecha** | 10/11/2026 |
| **Decisión de la consigna** | D11 · Observabilidad. Reemplaza la parte de trazas y de dónde se ven los logs de [ADR-011](ADR-011-observabilidad.md) |
| **Relacionadas** | [ADR-011](ADR-011-observabilidad.md) (métricas, tablero y alertas siguen vigentes), [ADR-005](ADR-005-comunicacion.md) (identificador de correlación y outbox), [ADR-003](ADR-003-persistencia.md) (una base por servicio), [ADR-013](ADR-013-capacidad-y-costos.md) (prueba de carga) |

## Contexto

La consigna (2.10) pide entender el estado de una operación sin revisar cada componente a mano, con logs estructurados, métricas y un tablero. **Primero se implementó con trazas:** OpenTelemetry en los cuatro servicios y en el gateway, exportando a Jaeger, y logs JSON en stdout (ADR-011). Funcionó: una compra se veía como una sola traza, con cada llamada y cada consulta. Este ADR lo reemplaza por algo **más simple y unificado**: un solo identificador y una sola tabla de logs, en lugar de dos lugares (Jaeger para las trazas, `docker compose logs` para los logs).

En la cátedra se vio otro modelo: un **track id** que viaja en cada llamada, y los logs de todos los servicios en una tabla `app_logs` de MySQL, escrita con GORM y consultada desde Grafana. Ese modelo:

- **Se defiende con lo visto en clase.** Jaeger y OpenTelemetry no están en esa lista.
- **Se muestra en una sola pantalla:** se pega el track id en Grafana y aparece todo lo que pasó con ese pedido, en todos los servicios.

Además, las trazas dejaban dos problemas abiertos: los logs no estaban centralizados (había que usar `docker compose logs`), y Jaeger guardaba las trazas en memoria sin límite.

**Lo que mostró la prueba de carga** (ADR-013, 10/11/2026, dos réplicas, en una notebook con 7,6 GB para Docker):

| Jaeger | 600 pedidos/s: p95 · compra · errores | 800 pedidos/s: p95 · compra · logrados | Memoria de Jaeger |
|---|---|---|---|
| Con trazas acumuladas de las corridas anteriores | **527 ms** · 2,4 s · 0,15 % | **473 ms** · 2,0 s · **289 de 800** | 2,7 → 3,7 → **4,3 GB** |
| Recién reiniciado | 33 ms · 171 ms · 0 % | 65 ms · 262 ms · 793 | 0,4 → 1,2 GB |
| Apagado | 55 ms · 307 ms · 0 % | 49 ms · 263 ms · 793 | — |

Trazar costaba poco: con Jaeger vacío, los resultados eran parecidos a tenerlo apagado. **Lo que rompía el sistema era la memoria:** Jaeger all-in-one guardaba las trazas en memoria y sin límite, unos 18 KB por pedido. Con 4,3 GB de los 7,6 GB de Docker, al resto le quedaba poca memoria (Elasticsearch bajó de 1,1 a 0,7 GB) y todo el sistema se ponía lento. Fue lo primero que degradó el sistema, antes que la CPU. En reposo, Jaeger usaba 0,9 GB.

## Criterios

1. Seguir una compra de punta a punta con un solo identificador, visible en Grafana.
2. Que una falla de la base de logs no afecte a las compras: ni las frene ni las haga fallar.
3. Respetar una base por servicio (ADR-003).
4. Usar lo visto en clase: MySQL, GORM, Grafana y el track id.

## Decisión

### 1. Base propia para logs: `mysql-logs`

- **Un contenedor `mysql:8.4` propio.** Catálogo usa MongoDB y ningún servicio escribe en la base de otro (ADR-003). Esta base es infraestructura de observabilidad, como Prometheus, no datos de negocio.
- **Tabla `app_logs`** ([`observabilidad/mysql-logs/001_app_logs.sql`](../../observabilidad/mysql-logs/001_app_logs.sql)):
  - Los campos del modelo de la cátedra: `id`, `track_id`, `stage` (el servicio), `level`, `message` y `created_at` (en milisegundos).
  - Dos más, para no perder lo que ya tenían los logs: `instancia` (la réplica) y `atributos` (JSON).
  - Índices por `track_id`, por `created_at` y por `(stage, level)`.
- **Dos usuarios** ([`002_usuarios.sh`](../../observabilidad/mysql-logs/002_usuarios.sh)): `logs_escritor`, que solo puede hacer `INSERT` (lo usan los servicios), y `logs_lector`, que solo puede hacer `SELECT` (lo usa Grafana). Nadie puede borrar ni cambiar logs.
- **Puertos y contraseñas.** En local escucha en `127.0.0.1:3310`. En la nube no publica puertos y exige las contraseñas, como las otras bases.

### 2. Escritura asíncrona y por tandas

El mismo código en los cuatro servicios: `internal/observabilidad/logs_mysql.go`. Cada servicio es su propio módulo Go y su imagen se construye solo con su carpeta, así que no se puede compartir.

- **Un manejador de slog que envuelve al de stdout.** La línea sale por stdout igual que antes. Si es INFO o más, además se encola, en una cola de 10.000.
- **Las sondas no se loguean** (`/healthz`, `/readyz`, `/metrics`), para no tapar el tráfico real.
- **Una goroutine guarda tandas** cada 1 s o cada 200 líneas, con `CreateInBatches` de GORM.
- **Se descarta en lugar de esperar.** Si la cola está llena o la base no responde, la línea se pierde para la tabla (sigue en stdout) y se cuenta en `logs_descartados_total{motivo}`. Los motivos son `cola_llena`, `base_no_disponible` y `apagado`.
- **Sin bucles.** Los avisos del propio escritor van a stderr con `fmt`, nunca por slog. El logger del driver de MySQL y el de GORM tampoco pasan por slog. Si pasaran, una base caída generaría logs que se volverían a intentar guardar en la misma base.
- **El servicio arranca aunque `mysql-logs` esté caído.** GORM se abre sin conectarse, y cada tanda vuelve a intentar la conexión.
- **Al apagar**, se guarda lo pendiente con un máximo de 5 s.

### 3. Interruptor

Con `LOGS_MYSQL_ACTIVO=false`, los logs quedan solo en stdout. Por defecto es `true`.

### 4. Track id

- **El track id es el `X-Correlation-Id` que ya existía** (ADR-005), y en la tabla se guarda en `track_id`. No se renombró nada.
- **`X-Track-ID` como alias.** El gateway y los servicios lo aceptan si no viene `X-Correlation-Id`, y el gateway devuelve los dos encabezados.
- **Viaja en todas las llamadas:**

| Por dónde | Cómo |
|---|---|
| HTTP entre servicios, al gimnasio, a Mercado Pago y a Elasticsearch | Un transporte que agrega `X-Correlation-Id` a cada llamada saliente |
| Eventos de Inventario (`ReservaVencida`) | Se guarda en el outbox y viaja en el sobre del evento |
| Eventos de Catálogo (indexar un producto) | El producto guarda el track id del pedido que lo cambió (`correlacionCambio`), el evento lo lleva y la indexación lo usa. Antes llevaba uno inventado (`evento-<id>`) |
| Pago, tareas y mails de una venta | La venta guarda el track id de la compra (columna `track_id` en `ventas`, `tareas` y `mails`). El aviso de Mercado Pago y la confirmación de una transferencia siguen con ese track id |

- **El pago y la transferencia llegan en otro pedido.** El aviso de Mercado Pago lo manda el simulador cuando se paga, y la confirmación de una transferencia es una acción del administrador desde el panel. Ese pedido llega con su propio id. Ventas deja una línea con ese id ("aviso de pago recibido" o "transferencia confirmada por el administrador"), con `track_id_venta` en los atributos, y sigue con el track id de la venta. Así una compra se ve completa, hasta los mails de pago aprobado.

### 5. Tablero "FAV · Logs"

- **Datasource MySQL**, con el usuario de solo lectura.
- **Tablero versionado** ([`fav-logs.json`](../../observabilidad/grafana/tableros/fav-logs.json)):
  - Variables `track_id` (texto libre), `servicio` y `nivel`.
  - Una tabla de hasta 500 líneas, con hora en milisegundos, servicio, réplica, nivel, mensaje y atributos.
  - Un gráfico de advertencias y errores por servicio.
  - Con track id muestra todas sus líneas, sin importar el rango de tiempo. Sin track id, las últimas del rango.
- **Un link** desde "FAV · Estado general".

### 6. Sin Jaeger ni OpenTelemetry

- **Se sacaron** el contenedor de Jaeger, el SDK de OpenTelemetry, `otelhttp`, `otelsql`, la instrumentación de MongoDB y el `trace_id`/`span_id` de los logs.
- **El gateway** volvió a la imagen `nginx:1.28-alpine`, que trae njs para validar el token.
- **La traza distribuida es el track id:** la línea de tiempo de una compra en todos los servicios.

## Alternativas consideradas

| Alternativa | Por qué se descartó |
|---|---|
| **Mantener Jaeger** junto con los logs en MySQL | Muestra cuánto tardó cada paso, pero no es lo visto en clase, duplica el seguimiento y no tiene límite de memoria (ver Contexto) |
| **Mantener Jaeger con un límite** (`--memory.max-traces`) o con muestreo | Resuelve la memoria, pero deja dos lugares para seguir un pedido y los logs sin centralizar |
| **Loki** para los logs | Otra pieza fuera de lo visto en clase. MySQL ya se usa y Grafana lo lee directo |
| **Escribir en la base de cada servicio** | Catálogo usa MongoDB, así que no habría una sola tabla, y se mezclarían logs con datos de negocio. Una consulta por track id tendría que unir cuatro bases |
| **Escritura síncrona** (un `INSERT` por línea, en el pedido) | Cada pedido esperaría a la base de logs, y con ella caída fallarían o se frenarían las compras. Por esta razón, al principio se habían descartado los logs en MySQL |
| **Que la venta no guarde el track id** (el pago con su propio id) | La compra se seguía solo hasta crear la venta y la reserva. El pago, la confirmación en Inventario y los mails quedaban sueltos, unidos solo por el `venta_id` |

## Consecuencias

**Positivas**

- Un track id muestra una compra completa en una sola pantalla: precios en Catálogo, reserva en Inventario, venta, pago, confirmación de la reserva y mails.
- Los logs quedan centralizados y se consultan con SQL, también hacia atrás.
- Con `mysql-logs` caído, las compras siguen. Se probó: la compra respondió 201 en 31 ms.
- Desaparece el riesgo de memoria de Jaeger (ver Contexto).

**Negativas aceptadas**

- **No se ve cuánto tardó cada paso entre servicios.** Cada línea `pedido http` tiene su `duracion_ms`, pero no hay un árbol que muestre qué parte del pedido fue cada llamada ni cada consulta a la base.
- **El gateway no escribe en la tabla.** Su log de acceso, con la réplica que atendió, sigue en stdout (`docker compose logs gateway`).
- **Se descartan líneas** bajo una carga mayor a la que se aguanta, o mientras `mysql-logs` no responde. Las líneas de mientras estuvo caído no se recuperan, aunque quedan en stdout.
- **La tabla crece sin límite,** como crecía la memoria de Jaeger, aunque en disco y no en memoria: en un minuto a 600 pedidos/s sumó unas 44.000 filas. Ver Pendientes.
- **Dos cambios seguidos de un producto salen en un solo evento,** con el track id del último. Es lo normal del outbox: avisa la versión más nueva.
- **El pedido HTTP del aviso de pago queda con su propio id.** Se une a la compra por la línea "aviso de pago recibido". Lo mismo pasa con el pedido del administrador que confirma una transferencia.
- **Las ventas guardadas antes de este cambio** no tienen track id: su pago y sus tareas usan uno propio.
- **GORM se usa solo para los logs.** Las bases de negocio siguen con `database/sql` (ADR-003).

## Pendientes

- **Retención de los logs [?].** Hay que borrar lo que tenga más de N días. Se decide después de la presentación.

## Validación (10/11/2026, en local)

| Comprobación | Resultado |
|---|---|
| Tests del escritor con un destino falso (tandas, base caída, cola llena, cierre con tiempo máximo, sin bucle) | Pasan, también con `-race` (3 corridas, en un contenedor de Go) |
| Test de integración contra MySQL real, con el esquema de `mysql-logs` | Pasa en los cuatro servicios |
| Tests de integración existentes (las 3 MySQL, MongoDB, Elasticsearch) y pruebas del gateway | Pasan. `token` 42/42, `errores` 35/35, `tienda` 44/44 |
| Compra con Mercado Pago, pagada en el simulador, buscada por su track id en "FAV · Logs" | 12 líneas en orden: Catálogo, Inventario, Ventas, aviso de pago, confirmación de la reserva y mails. En el navegador se probó antes de sacar Jaeger; sin Jaeger, con `curl` y la consulta del tablero en Grafana |
| Transferencia confirmada por el administrador | Lo mismo, y la acción del administrador con su propio id |
| Cambio de un producto con `X-Track-ID` | La línea "índice actualizado" lleva ese track id |
| `docker compose stop mysql-logs` | La compra sigue (201). Al volver, los logs nuevos aparecen sin reiniciar ningún servicio y los descartes quedan en `logs_descartados_total` |
| Sin Jaeger ni OpenTelemetry | Ningún log con `trace_id`. Los `go.mod` no tienen dependencias de OpenTelemetry |
| Repetido el 11/11/2026, en local, sin `-race` | `go test` de los cuatro servicios, con la integración activada (MySQL de cada servicio y de logs; MongoDB y Memcached descartables; Elasticsearch del Compose): 391 pasan, ninguno salteado. `go vet` sin avisos. Gateway: `token` 42/42, `tienda` 44/44 y `errores` 35/35. Frontend: 125/125 |

**Prueba de carga** (`TASAS="600" bash loadtest/pico_tienda.sh`, 60 s, en la notebook):

| `LOGS_MYSQL_ACTIVO` | Pedidos/s | Errores | p95 total | p99 | p95 de la compra | Descartes | CPU de todos los contenedores |
|---|---|---|---|---|---|---|---|
| `true` | 597 | 0 % | 18 ms | 27 ms | 34 ms | 0 (unas 44.000 filas guardadas) | 321 % |
| `false` | 597 | 0 % | 15 ms | 24 ms | 31 ms | — | 295 % |

Es una corrida de cada una: alcanza para decir que a 600 pedidos/s el objetivo (p95 < 300 ms) se cumple holgado con los logs en MySQL y sin descartes, pero no para atribuirle los 3 ms de diferencia al escritor.

**Sin probar:** la nube, y una carga mayor que la de este escalón.

---

**Si la decisión cambia:** no se borra este archivo. Se marca como *Reemplazada por ADR-YYY* y el nuevo ADR indica a cuál reemplaza y por qué hubo que revisarla.
