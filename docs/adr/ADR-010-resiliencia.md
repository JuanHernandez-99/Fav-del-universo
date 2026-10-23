# ADR-010 · Resiliencia

|                             |                                                                                                                                                                                                                                                                                          |
| --------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Estado**                  | Aceptada (primera versión) · Entrega 2. Cubre Inventario y la entrada por el gateway. La caída controlada y su informe (`docs/POSTMORTEM.md`) están **pendientes**.                                                                                                                       |
| **Fecha**                   | 23/10/2026                                                                                                                                                                                                                                                                               |
| **Decisión de la consigna** | D10 · Resiliencia                                                                                                                                                                                                                                                                        |
| **Relacionadas**            | [ADR-004](ADR-004-consistencia-y-concurrencia.md) (idempotencia), [ADR-005](ADR-005-comunicacion.md) (timeouts y reintentos), [ADR-009](ADR-009-consumo-del-gimnasio.md) (gimnasio), [ADR-011](ADR-011-observabilidad.md) (métricas y alertas), [ADR-012](ADR-012-balanceo-de-carga.md) (réplicas) |

> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar · _previsto_: forma parte del diseño pero todavía no está implementado.

## Contexto

La consigna (requisito 2.9) pide identificar las dependencias de cada servicio, definir qué ocurre cuando una falla o responde con lentitud, implementar **al menos un mecanismo de protección** y demostrar el comportamiento con una **caída provocada**. El apunte de resiliencia plantea el criterio: una falla parcial no debe derribar lo que todavía puede funcionar, la lentitud suele ser peor que un error inmediato (retiene recursos), un timeout no demuestra que el efecto remoto no ocurrió, y cada reintento necesita un dueño.

ARCHITECTURE §8 describe el comportamiento esperado de todo el sistema. Este ADR registra lo que **está implementado** en esta entrega y lo que falta.

### Dependencias de Inventario

| Dependencia                                         | Para qué                                              |
| --------------------------------------------------- | ----------------------------------------------------- |
| MySQL de Inventario                                 | Todo el estado del servicio                           |
| Gimnasio (HTTP, de otro grupo)                      | Consultar su stock y enviar reposiciones              |
| RabbitMQ                                            | Publicar `ReservaVencida` (_previsto_)                |
| Gateway (nginx)                                     | Único camino de entrada y balanceo                    |
| Quien llama: Ventas (hoy un mock) y el panel        | Origen de los pedidos                                 |

## Criterios

1. **Contener la falla:** que una dependencia caída o lenta no tire abajo lo que puede seguir funcionando.
2. **Decir la verdad:** nada de respuestas de éxito falsas ni de stock inventado.
3. **Reintentos seguros:** solo donde hay idempotencia y con un dueño claro.
4. **Observable y demostrable:** cada mecanismo tiene una métrica y se puede provocar.
5. **Costo proporcional:** pocos mecanismos, bien elegidos, para un equipo de dos personas.

## Decisión

### Mecanismos implementados

| Mecanismo (apunte de resiliencia) | Dónde | Valor | Qué protege |
| --- | --- | --- | --- |
| **Deadline / timeout** | Gateway → servicio | 5 s de lectura y envío, 1 s para conectar | Que el gateway no espere indefinidamente |
| | Inventario, cada pedido HTTP | 4 s (menor que el del gateway) | Que Inventario corte antes de que el gateway ya haya desistido |
| | Inventario → MySQL | 3 s para conectar, 5 s de lectura y escritura, y el contexto del pedido | Que una base lenta no retenga el pedido |
| | Inventario → gimnasio | 2 s | Que un gimnasio lento no retenga recursos |
| **Reintento con idempotencia** | Ventas (mock) → Inventario | 2 s por intento, hasta 2 reintentos (200 ms y 400 ms más aleatoriedad), solo ante timeout, conexión rechazada, 502, 503 o 504; nunca ante `4xx` | Superar fallas transitorias sin duplicar la reserva (misma `Idempotency-Key`) |
| | Inventario → gimnasio | Persistido en la base, espera de 5 s duplicándose hasta 5 min | Que una reposición no se pierda si el gimnasio no responde |
| **Circuit breaker** | Inventario → gimnasio | Se abre tras 5 fallas seguidas; 30 s abierto; una llamada de prueba | Fallar rápido en lugar de esperar el timeout en cada consulta |
| **Bulkhead (aislamiento)** | Pool de conexiones de Inventario | 20 conexiones (`INVENTARIO_DB_MAX_CONEXIONES`) | Que un pico no agote las conexiones de la base |
| | Envío de reposiciones | Un trabajador secuencial por réplica, lotes de 50 | Que el gimnasio no reciba ráfagas de llamadas simultáneas |
| **Degradación honesta (fallback)** | Panel → consignación | `503 GIMNASIO_NO_DISPONIBLE` con `Retry-After: 30`; el resto del panel sigue | No inventar el stock del gimnasio |
| | Inventario ante pedidos que no llegan a tiempo | `503 NO_DISPONIBLE` con `Retry-After: 1` | El cliente sabe que puede reintentar |
| **Balanceo con verificación pasiva** | Gateway → 2 réplicas de Inventario | Una réplica que falla 2 veces en 10 s sale del reparto 10 s | Que la caída de una réplica no se note (ADR-012) |
| **Límite de pedidos (admisión)** | Gateway, capacidad publicada | 60 pedidos por minuto por grupo, ráfagas de 20; el exceso recibe `429` | Que un pico del consumidor externo no sature a FAV |
| **Idempotencia** | Reservas, ingresos, transferencias y reposiciones | Clave única persistida (ADR-004) | Que cualquier reintento sea seguro |
| **Vencimiento de reservas** | Proceso cada 10 s | Plazos de 20 min o 48 h | Que una venta a medias nunca deje stock bloqueado para siempre |
| **Cierre ordenado** | Inventario | 10 s para terminar los pedidos en curso al recibir `SIGTERM` | Que un reinicio no corte pedidos a la mitad |

### Qué pasa cuando cae cada dependencia

| Se cae o se vuelve lenta               | Sigue funcionando                                                                                    | Deja de funcionar                                                              | Cómo se contiene                                                                                              |
| -------------------------------------- | ---------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------- |
| **Una réplica de Inventario**          | Todo: la otra atiende                                                                                | —                                                                              | Balanceo; el gateway reintenta contra la otra si no pudo conectar o recibió `502`                              |
| **MySQL de Inventario**                | Los demás servicios (una base por servicio, RNF-08)                                                  | Reservar, confirmar, consultar y mover stock. `/readyz` responde `503`          | Timeouts de la base y del pedido. Sin transacciones a medias. Se recupera sola al volver                       |
| **El gimnasio**                        | Todo lo de FAV, incluidas las reservas                                                               | Ver su stock y enviarle reposiciones                                           | Timeout, circuit breaker y reintento persistido. Las reposiciones pendientes se envían al volver              |
| **El gateway**                         | Nada desde afuera. Los servicios entre sí siguen funcionando                                         | Toda entrada al sistema                                                        | **Sin protección:** hay una sola instancia (ver Consecuencias)                                                  |
| **Quien llama es lento y reintenta**   | Inventario                                                                                           | —                                                                              | Idempotencia: el reintento devuelve la misma reserva                                                          |
| **RabbitMQ** (_previsto_)              | Todo lo síncrono                                                                                     | Los efectos asíncronos se demoran                                              | Outbox: los eventos esperan en la tabla y se publican cuando vuelve                                           |

### Qué se comunica al que llama

Los errores usan el formato Problem Details con un `codigo` estable, y distinguen lo que se puede reintentar de lo que no:

| Código                            | HTTP | ¿Se reintenta?                                |
| --------------------------------- | ---- | --------------------------------------------- |
| `STOCK_INSUFICIENTE`, `RESERVA_VENCIDA`, `PEDIDO_INVALIDO`, `CLAVE_IDEMPOTENCIA_REUTILIZADA` | 4xx | No: la respuesta es definitiva |
| `NO_DISPONIBLE`, `GIMNASIO_NO_DISPONIBLE` | 503 | Sí, con `Retry-After`            |
| `ERROR_INTERNO`                   | 500  | A criterio del cliente; el detalle queda en el log, no en la respuesta |

## Alternativas consideradas

| Alternativa                                                     | Por qué se descartó                                                                                                                                                      |
| --------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Reintentar en todas las capas** (gateway, Ventas, Inventario) | Multiplica los intentos (el apunte muestra 3 × 3 × 3 = 27). Se asigna un dueño del reintento: quien llama (Ventas) hacia Inventario, y Inventario hacia el gimnasio.      |
| **Circuit breaker también en Ventas → Inventario**              | Inventario es una dependencia crítica sin alternativa: abrir el circuito solo cambiaría la espera por un error inmediato. Se evalúa cuando exista Ventas real.             |
| **Bulkhead por semáforo en cada operación**                     | El volumen no lo justifica; el pool de conexiones y los trabajadores secuenciales ya acotan la concurrencia.                                                              |
| **Health check activo con retiro de réplicas**                  | nginx de código abierto solo ofrece verificación pasiva. Además, si la base cae, las dos réplicas fallan igual: retirarlas no ayuda (ADR-012).                             |
| **Respuesta de reserva "optimista" cuando la base no responde** | Mentiría: se prometería stock sin haberlo apartado.                                                                                                                       |
| **Sin límite de pedidos por grupo**                             | Un pico del consumidor externo podría afectar a las compras de FAV, porque comparten servicios.                                                                            |

## Consecuencias

**Positivas**

- La caída del gimnasio, la de una réplica o un reinicio no producen pérdida de datos ni respuestas falsas.
- Cada mecanismo tiene una métrica y se puede provocar a mano (ver Validación).

**Negativas aceptadas y problemas conocidos**

- **El gateway es una sola instancia.** Es un punto único de falla. El apunte lo marca como anti-patrón. Se acepta en esta entrega; para la presentación final se evalúa una segunda instancia (el límite de pedidos es por instancia: con dos, habría que coordinarlo o aceptar la aproximación).
- **El presupuesto de tiempo no se propaga de punta a punta.** El mock de Ventas llama a Inventario con 2 s por intento y hasta 3 intentos: en el peor caso, con Inventario colgado (no caído), son unos 6,6 s. El gateway corta la llamada del consumidor a los 5 s, así que este recibiría el `504` genérico de nginx en lugar del `503 NO_DISPONIBLE` con Problem Details que promete el contrato. El mock cancela sus reintentos al detectar que la conexión se cerró, pero la respuesta ya se perdió. Si Inventario está caído (conexión rechazada), los tres intentos terminan en menos de un segundo y el consumidor sí recibe el `503` del contrato. Hay que alinear los tiempos (un deadline total menor al del gateway) cuando se implemente Ventas.
- **Reintentos en dos lugares.** El gateway reintenta contra la otra réplica ante error de conexión, timeout o `502` (hasta 2 intentos), y el mock de Ventas reintenta además hasta 2 veces. Es seguro por la idempotencia, pero amplifica la carga durante una degradación. nginx no reenvía un `POST` que ya llegó a una réplica.
- **La verificación de réplicas es pasiva.** Una réplica con la base caída sigue en el reparto porque su `503` no cuenta como falla para nginx. Se decidió así a propósito: ese `503` también lo usa Inventario para avisar que el gimnasio no está disponible, y contarlo sacaba del reparto a réplicas sanas.
- **El circuito del gimnasio es por réplica.** Cada réplica abre el suyo con su propia muestra.
- **Sin bulkhead para las consultas al gimnasio del panel**, que corren en el hilo del pedido (con timeout de 2 s).
- **El rechazo definitivo ante cualquier `4xx` del gimnasio** puede devolver stock por error de configuración (ADR-009).
- **Sin cola de mensajes fallidos todavía:** llega con RabbitMQ.

## Pendientes

- **Caída controlada y `docs/POSTMORTEM.md`.** La consigna pide una caída provocada con su informe. Plan de ensayo: (1) detener una réplica de Inventario durante carga; (2) detener el gimnasio y enviar reposiciones; (3) detener la base. Para cada una: hipótesis, métricas observadas, comportamiento real, tiempo de recuperación.
- Alinear tiempos y reintentos de Ventas con el gateway.
- Alerta por circuito abierto y por reposiciones antiguas (ADR-011).
- Mensajes fallidos (DLQ) cuando exista el consumidor de `ReservaVencida`.
- Segunda instancia del gateway.

## Validación

Provocar y observar (los pasos están en el README del servicio):

| Prueba                                         | Cómo                                                                             | Resultado esperado                                                                                                     |
| ---------------------------------------------- | -------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| Caída de una réplica                           | `docker stop <réplica>` y repetir consultas                                      | Todas responden; en el log del gateway deja de aparecer su IP                                                          |
| Caída del gimnasio                             | `docker compose stop gimnasio-mock`                                              | `503 GIMNASIO_NO_DISPONIBLE`; las reservas siguen; la reposición queda _PENDIENTE_ y se envía al volver                |
| Gimnasio lento                                 | `fixedDelay` de 3 s en el mock                                                   | Timeout de 2 s y, tras 5 fallas, `circuit_breaker_estado = 2`                                                          |
| Reintento de un pedido                         | Repetir un pedido con la misma clave                                             | Misma reserva, sin descontar dos veces                                                                                 |
| Exceso de pedidos de un grupo                  | Más de 80 pedidos en un minuto por `/v1/pedidos`                                 | `429` en el exceso                                                                                                     |
| Caída de la base **[propuesta]**               | `docker compose stop mysql-inventario`                                           | `/readyz` responde `503`; los pedidos fallan sin dejar datos a medias; se recupera al volver. A confirmar en el ensayo |

Esta tabla se completa con los resultados reales en `docs/POSTMORTEM.md`.
