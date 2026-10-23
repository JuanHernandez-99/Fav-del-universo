# ADR-005 · Comunicación entre servicios

| | |
|---|---|
| **Estado** | Aceptada (versión inicial) · Entrega 1. Se valida en la Entrega 2. |
| **Fecha** | 09/10/2026 |
| **Decisión de la consigna** | D5 · Comunicación entre servicios |
| **Relacionadas** | [ADR-001](ADR-001-limites-de-los-servicios.md) (relaciones entre servicios), [ADR-003](ADR-003-persistencia.md) (outbox e idempotencia en la base), ADR-004 (concurrencia, Entrega 2), [ADR-008](ADR-008-contrato-propio.md) (capacidad publicada), ADR-010 (resiliencia, Entrega 2) |

> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar. Los valores numéricos son **[propuesta]** hasta medirlos en la Entrega 2.

> **Actualización 14/10/2026:** la capacidad publicada quedó definida en el [ADR-008](ADR-008-contrato-propio.md): **catálogo y pedidos**, servida por Catálogo y Ventas. Además, Inventario consume la capacidad de un gimnasio (ADR-009, en preparación). Las menciones a las opciones A y B son de la Entrega 1 y se revisan al validar este ADR en la Entrega 2.

## Contexto

La consigna (requisito 2.5) pide comunicación síncrona y asíncrona:

- **Síncrona:** timeout, manejo de errores, respuestas tardías o ausentes y política de reintentos.
- **Asíncrona:** mensajería con publicación y consumo de un evento de dominio, y tratamiento de mensajes fallidos.

ADR-001 fija qué servicios se hablan y dejó abierto si la confirmación de la reserva es un evento (`VentaPagada`) o una llamada síncrona. El SPEC deja abierto cómo se envían los mails sin bloquear la venta.

## Criterios

1. **Síncrono cuando quien llama necesita la respuesta para seguir** (por ejemplo, saber si hay stock antes de cobrar).
2. **Asíncrono cuando el efecto puede demorarse** y conviene que el emisor no dependa de que el receptor esté disponible.
3. **Una sola fuente de verdad para cada carrera.** Si dos cosas pueden pasar a la vez (pago y vencimiento), decide un solo servicio.
4. **Todo reintento es seguro:** nada se aplica dos veces.

## Decisión

### 1. Mapa de interacciones

| Interacción | Tipo | Motivo |
|---|---|---|
| Frontend → Gateway → servicios | Síncrona (HTTP/JSON) | El usuario espera la respuesta. |
| Ventas → Catálogo: precio vigente y datos de la variante | Síncrona | Hace falta para crear la venta (RN-22). |
| Ventas → Inventario: reservar, liberar, **confirmar** y descontar | Síncrona | Ventas necesita saber el resultado para seguir. **Confirmar pasa a ser síncrono** (ver punto 4). |
| Ventas → Mercado Pago: crear el pago | Síncrona | El comprador espera ser redirigido. |
| Mercado Pago → Gateway → Ventas: aviso del pago | Síncrona (webhook) | Lo inicia Mercado Pago. |
| Ventas → proveedor de mail | Asíncrona dentro de Ventas | No debe bloquear la venta (punto 6). |
| Inventario → Ventas: `ReservaVencida` | **Evento** (RabbitMQ) | El vencimiento ocurre sin que nadie lo pida; Ventas reacciona cuando puede. |
| Catálogo → Inventario: `VarianteCreada` **[propuesta]** | **Evento** | Crear un producto no debe depender de que Inventario esté disponible. |
| Catálogo e Inventario → índice de búsqueda | **Evento** **[propuesta]** | Se tolera un retraso acotado (ADR-006). |
| Grupo consumidor → Gateway → Inventario (A) o Ventas (B) | Síncrona | Según el contrato publicado (ADR-008). |

El evento de dominio que pide la consigna es **`ReservaVencida`**: lo publica Inventario y lo consume Ventas.

### 2. Llamadas síncronas: tiempos y reintentos **[propuesta]**

| Llamada | Timeout | Reintentos | Cuándo se reintenta | Protección extra |
|---|---|---|---|---|
| Gateway → cualquier servicio | 5 s | No | — | Rate limiting por cliente |
| Ventas → Catálogo (precio) | 1 s | 1 | Timeout, conexión rechazada, 502, 503, 504 | Es una lectura: reintentar no tiene efectos |
| Ventas → Inventario (reservar, liberar, confirmar, descontar) | 2 s | 2 (3 intentos en total), con espera de 200 ms y 400 ms más un componente aleatorio | Timeout, conexión rechazada, 502, 503, 504 | Clave de idempotencia en cada operación |
| Ventas → Mercado Pago (crear pago) | 5 s | No **[?]** | — | Circuit breaker: se abre tras 5 fallas seguidas y prueba de nuevo a los 30 s |
| Ventas → proveedor de mail | 10 s | 5, con espera creciente (1, 5, 15, 30 y 60 min) | Cualquier falla | Fuera de la transacción de la venta |

**Nunca se reintenta un error de negocio** (`4xx`, como sin stock o reserva vencida): la respuesta es definitiva.

**[?]** Mercado Pago: se reintentará la creación del pago solo si su API acepta una clave de idempotencia. Se verifica al implementar.

### 3. Respuestas tardías o ausentes

- **Timeout al reservar:** la reserva pudo haberse hecho o no. El reintento usa **la misma clave de idempotencia**, así que si ya existía, Inventario devuelve la misma reserva.
- **Si se agotan los reintentos:** Ventas no crea la venta y muestra un error claro (HU-11.9). Si la reserva igual se había hecho, **vence sola** a los 20 minutos o a las 48 horas: nunca queda stock bloqueado para siempre.
- **Respuesta que llega después del timeout:** se descarta. Quien llamó ya decidió con el reintento, que es idempotente.

### 4. Confirmación de la reserva: síncrona, decidida por Inventario

Al aprobarse el pago (aviso de Mercado Pago o confirmación de la transferencia por el administrador):

1. Ventas registra el pago aprobado y una tarea "confirmar reserva" **en la misma transacción**. Al webhook le responde enseguida.
2. Un proceso de Ventas toma la tarea y llama a Inventario: `confirmar(reserva)`, con la clave de idempotencia de la venta.
3. Inventario confirma **solo si la reserva no venció**, en una transacción. Si coinciden la confirmación y el vencimiento, gana una sola (CU-02.b).
4. Según la respuesta:
   - **Confirmada:** la venta pasa a *Pagada* y se envían los mails.
   - **`RESERVA_VENCIDA`:** la venta pasa a *A verificar* y se avisa al administrador (RN-14).
   - **Inventario no responde:** la tarea se reintenta (punto 2) hasta que responda. La venta sigue *Pendiente de pago* mientras tanto.

**Por qué no el evento `VentaPagada`:** con el evento, Ventas marcaría la venta como *Pagada* antes de saber si la reserva seguía vigente. Si la reserva vencía mientras el evento esperaba en la cola, el sistema quedaba con una venta pagada y el stock liberado, y había que compensar. Con la llamada síncrona, Inventario es el único que decide la carrera entre pago y vencimiento.

### 5. Eventos asíncronos

| Aspecto | Decisión |
|---|---|
| Broker | RabbitMQ. Mensajes persistentes y colas durables. |
| Publicación | **Transactional outbox:** el evento se guarda en la tabla outbox en la misma transacción que el cambio, y un proceso lo publica después **[propuesta: cada 1 s]**. Si RabbitMQ está caído, los eventos esperan en el outbox. |
| Garantía de entrega | **Al menos una vez.** Un evento puede llegar repetido. |
| Consumidores | **Idempotentes:** guardan el id del mensaje en una tabla de procesados, en la misma transacción que su efecto. Si el id ya está, lo descartan. |
| Mensajes fallidos | 3 reintentos con espera **[propuesta]**. Después van a una **cola de mensajes fallidos (DLQ)** por consumidor, donde quedan para revisarlos. La cantidad de mensajes en la DLQ es una métrica con alerta (ADR-011). |
| Formato | Sobre común: `id`, `tipo`, `version`, `ocurridoEn`, `correlationId` y `datos`. Agregar campos es compatible; quitar o cambiar uno exige una versión nueva del evento. |

| Evento | Publica | Consume | Datos | Efecto |
|---|---|---|---|---|
| `ReservaVencida` | Inventario | Ventas | `reservaId` y la referencia que Ventas envió al reservar (Inventario no conoce las ventas) | La venta pasa a *Vencida*. Si ya no está *Pendiente de pago*, no hace nada. |
| `VarianteCreada` **[propuesta]** | Catálogo | Inventario | `varianteId` | Crea el stock en 0 en ambos canales. Si ya existe, no hace nada. |
| Cambios de producto y de stock **[propuesta]** | Catálogo, Inventario | Actualizador del índice | Se definen en ADR-006 | Mantienen la búsqueda sincronizada. |

### 6. Mails sin bloquear la venta

Ventas no envía el mail dentro de la transacción de la venta. Guarda el mail pendiente en el **registro de mails**, en la misma transacción que el cambio de estado. Un proceso de Ventas lo envía después, con los reintentos del punto 2. Agotados los intentos, queda como fallido y la venta no se afecta (HU-20.5).

Si el proceso se cae entre enviar el mail y marcarlo como enviado, al volver lo reenvía: puede llegar **un mail duplicado**. Es la limitación aceptada en HU-20.4.

Con la opción B de la capacidad publicada, los emails de otros grupos usan el mismo mecanismo, en su propia tabla (ADR-008).

### 7. Correlación

Cada pedido lleva un identificador de correlación (`X-Correlation-Id`). Si no viene, lo crea el gateway. Viaja en cada llamada síncrona y en el sobre de cada evento, y aparece en todos los logs (ADR-011).

## Alternativas consideradas

| Alternativa | Por qué se descartó |
|---|---|
| **Confirmar la reserva con el evento `VentaPagada`** | Era la propuesta inicial. Deja la carrera entre pago y vencimiento repartida en dos servicios y obliga a compensar (punto 4). |
| **Todo síncrono** (también vencimientos y variantes nuevas) | Inventario tendría que llamar a Ventas al vencer una reserva, y Catálogo a Inventario al crear una variante. La caída de uno bloquearía al otro. Además, la consigna exige mensajería. |
| **Reservar por evento** | El comprador tiene que saber en el momento si hay stock, antes de pagar. |
| **Publicar directo a RabbitMQ, sin outbox** | Si el broker falla después de guardar el cambio, el evento se pierde. |
| **Garantía "exactamente una vez"** | RabbitMQ no la ofrece de punta a punta. Se obtiene el mismo efecto con entrega al menos una vez y consumidores idempotentes. |
| **Reintentos sin límite o sin espera** | Ante una caída, multiplican la carga sobre el servicio que ya está fallando. |
| **Enviar el mail dentro de la transacción de la venta** | Si el proveedor de mail tarda o falla, la venta queda bloqueada o se pierde. |
| **Kafka en lugar de RabbitMQ** | Con un volumen tan bajo, no justifica su costo de operación. RabbitMQ resuelve colas, reintentos y DLQ. |

## Consecuencias

**Positivas**

- Una compra no queda a medias: los reintentos son idempotentes y las reservas huérfanas vencen solas.
- Inventario decide sola la carrera entre pago y vencimiento.
- La caída de RabbitMQ no frena lo síncrono: los eventos esperan en el outbox.
- La caída del proveedor de mail no afecta las ventas.

**Negativas aceptadas**

- **Si Inventario está caído cuando llega un pago aprobado,** la venta queda *Pendiente de pago* hasta que Inventario vuelva. Si mientras tanto vence la reserva, termina en *A verificar* (RN-14).
- **Ventas depende de forma síncrona de Catálogo e Inventario para vender.** Se acota con timeouts y errores claros (ADR-010).
- **Efectos asíncronos demorados:** si RabbitMQ cae, se atrasan el paso a *Vencida*, las variantes nuevas en Inventario y el índice.
- **Mails posiblemente duplicados** en una ventana de falla (HU-20.4).
- **Más piezas para operar y probar:** outbox, procesos de publicación y envío, tablas de procesados y DLQ en cada servicio que publica o consume.

## Pendientes

- **Cómo recibe el entorno local los avisos de Mercado Pago.** **[?]** Un túnel público hacia la máquina local, o un simulador del aviso para las pruebas.
- **Si la API de Mercado Pago acepta clave de idempotencia** para reintentar la creación del pago.
- **Proveedor de mail.** **[?]**
- **Eventos del índice de búsqueda y retraso máximo:** ADR-006 (Entrega 2).
- **Valores de timeouts y reintentos:** se ajustan con las mediciones de la Entrega 2.

## Validación prevista (Entrega 2)

- Test de integración: el mismo evento entregado dos veces produce un solo efecto.
- Un mensaje que falla siempre termina en la DLQ y aparece en las métricas.
- Con RabbitMQ detenido se puede comprar; al volver, los eventos del outbox se publican.
- Con Inventario lento (por encima de 2 s), Ventas reintenta, no duplica la reserva y, agotados los intentos, muestra un error claro.
- Las trazas de una compra muestran el mismo identificador de correlación en todos los servicios.

## Validación (Entrega 2)

Contrastado con el código de Inventario, el gateway y el mock de Ventas.

| Punto del ADR                                          | Resultado                                                                                                                                                                  |
| ------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Confirmación síncrona entre Ventas e Inventario        | **Implementada** en Inventario (`POST /v1/reservas/{id}/confirmar`). Falta Ventas real.                                                                                      |
| Timeouts                                               | Gateway: 5 s de lectura y 1 s de conexión. Inventario: 4 s por pedido, 3 s de conexión y 5 s de lectura/escritura a la base. Gimnasio: 2 s. Mock de Ventas: 2 s por intento. |
| Reintentos                                             | Mock de Ventas: 3 intentos con 0, 200 y 400 ms más variación, solo ante 502/503/504 o error de red, nunca ante 4xx. Gimnasio: reintentos persistidos en la base (5 s hasta 5 min). |
| Idempotencia                                           | **Implementada** con encabezado `Idempotency-Key` y huella del pedido; clave reutilizada con otro cuerpo devuelve `409`.                                                      |
| Identificador de correlación                           | **Implementado:** lo toma o genera el gateway (`$request_id`), lo propagan los servicios y se guarda en el outbox. Ver ADR-011.                                                |
| Outbox                                                 | **Parcial:** se escribe en la misma transacción, pero **no hay publicador** a RabbitMQ y el outbox **no tiene columna `version`**; el sobre del evento se completa al publicar. |
| Consumo idempotente y DLQ                              | **No implementados** (Inventario aún no consume eventos).                                                                                                                    |

**Problema detectado:** los reintentos del mock de Ventas y los del gateway se suman, y el tiempo total de Ventas puede superar los 5 s del gateway (ADR-010). Hay que alinear los tiempos al implementar Ventas.
