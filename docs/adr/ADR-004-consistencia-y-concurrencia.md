# ADR-004 · Consistencia y concurrencia

|                             |                                                                                                                                                                                                                                                                 |
| --------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Estado**                  | Aceptada · Entrega 2, para las operaciones de Inventario. Las de Ventas están _previstas_.                                                                                                                                                                      |
| **Fecha**                   | 23/10/2026                                                                                                                                                                                                                                                      |
| **Decisión de la consigna** | D4 · Consistencia y concurrencia                                                                                                                                                                                                                                |
| **Relacionadas**            | [ADR-002](ADR-002-arquitectura-interna.md) (dónde vive cada regla), [ADR-003](ADR-003-persistencia.md) (MySQL y garantías), [ADR-005](ADR-005-comunicacion.md) (idempotencia, outbox y reintentos), [ADR-009](ADR-009-consumo-del-gimnasio.md) (reposiciones) |

> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar · _previsto_: forma parte del diseño pero todavía no está implementado.

## Contexto

La consigna (requisito 2.8) pide al menos una operación que **no pueda duplicarse ni perder información**. En FAV son dos y viven en Inventario (ARCHITECTURE §7): la **reserva de stock online** y la **transferencia entre stocks**. Las reglas de negocio que las gobiernan:

- RN-07: al pasar a pagar se reservan las unidades; las reservadas no están disponibles para nadie más.
- RN-08 y RN-09: la reserva vence a los 20 minutos (Mercado Pago) o a las 48 horas (transferencia).
- RN-10: una transferencia no puede mover más unidades que las disponibles, sin contar las reservadas.
- RN-14: si el pago llega con la reserva vencida, la venta no se confirma sola.
- RN-06 y RN-25: el stock **online** nunca queda negativo; el **físico** sí puede (venta del local).

Hay cuatro fuentes de concurrencia: compradores que piden las últimas unidades al mismo tiempo, el pago que coincide con el vencimiento, dos réplicas de Inventario ejecutando los mismos procesos periódicos y los reintentos del cliente cuando se pierde una respuesta.

## Criterios

1. **Nunca vender de más** ni dejar stock online negativo.
2. **Todo o nada:** una reserva con varias variantes se aparta completa o no se aparta.
3. **Un reintento nunca duplica el efecto.**
4. **Una sola autoridad por carrera:** si dos cosas pueden pasar a la vez, decide un solo lugar (ADR-005, criterio 3).
5. **Que se pueda probar con concurrencia real**, no solo razonar sobre el código.

## Decisión

La **base de datos decide cada carrera**, con transacciones locales de MySQL. No hay coordinación distribuida dentro de Inventario.

| Operación                                          | Garantía                                          | Mecanismo                                                                                                                                                                                                                                    |
| -------------------------------------------------- | ------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Reservar** (`POST /v1/reservas`)                 | No vender de más; todo o nada; idempotente        | Una transacción: inserta la reserva (la restricción única de la clave frena un duplicado concurrente) y, por cada ítem, `UPDATE stock SET reservado = reservado + ? WHERE variante_id = ? AND online - reservado >= ?`. Si algún ítem no alcanza, se hace rollback de todo. Los ítems se procesan **ordenados por variante**, así dos reservas bloquean las filas en el mismo orden y no se cruzan (sin deadlock). |
| **Confirmar** (`…/confirmar`)                      | Descuento definitivo solo si no venció            | `SELECT … FOR UPDATE` sobre la reserva, transición en el dominio y actualización del stock en la misma transacción. Si ya pasó `venceEn`, la reserva pasa a _VENCIDA_, se libera lo apartado y responde `409 RESERVA_VENCIDA` (Ventas marca la venta _A verificar_).                                       |
| **Liberar** (`…/liberar`)                          | Devuelve lo apartado una sola vez                 | Mismo bloqueo de fila. Liberar una reserva ya liberada o vencida no hace nada; una confirmada responde `409`.                                                                                                                                  |
| **Vencer** (proceso cada 10 s)                     | Una sola réplica vence cada reserva               | Cada reserva se vence con el mismo `FOR UPDATE`. Si otra réplica la venció antes, el cambio es nulo. Guarda el evento `ReservaVencida` en el **outbox en la misma transacción**.                                                              |
| **Transferir** (`POST /v1/transferencias`)         | Nunca mueve más que lo disponible; atómica        | Una sola `UPDATE` condicional (`online - reservado >= ?` desde el online, `fisico >= ?` desde el físico) más dos movimientos, en una transacción. Si no afecta ninguna fila, se informa cuánto había.                                          |
| **Ingresar** (`POST /v1/ingresos`)                 | Idempotente                                       | `INSERT … ON DUPLICATE KEY UPDATE` más el movimiento, dentro de la transacción de idempotencia.                                                                                                                                              |
| **Reponer al gimnasio** (`POST /v1/reposiciones`)  | Descuenta el físico una sola vez                  | La reposición pendiente y el descuento del stock físico van en la misma transacción. Un proceso toma las pendientes con `FOR UPDATE SKIP LOCKED` y las aparta 30 s para que dos réplicas no envíen la misma.                                  |

### Idempotencia

| Aspecto                                  | Decisión                                                                                                                                                                                     |
| ---------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Quién genera la clave                    | Quien llama (`Idempotency-Key`, de 8 a 100 caracteres). Es obligatoria en reservas, ingresos, transferencias y reposiciones.                                                                  |
| Dónde se guarda                          | Con restricción única: `reservas.clave_idempotencia`, `operaciones.clave_idempotencia` y `reposiciones.clave_idempotencia`. La clave se registra **en la misma transacción** que el efecto.   |
| Misma clave, mismo pedido                | Devuelve el resultado anterior sin volver a aplicar el efecto (en reservas, `Idempotent-Replayed: true`).                                                                                    |
| Misma clave, **otro** pedido             | `409 CLAVE_IDEMPOTENCIA_REUTILIZADA`. Se detecta comparando una huella (SHA-256) del contenido.                                                                                              |
| Dos intentos simultáneos con la misma clave | El segundo espera el bloqueo de la restricción única y, cuando el primero confirma, devuelve la misma reserva.                                                                            |
| Una operación que falla                  | Hace rollback también de la clave: el reintento se evalúa de nuevo, no devuelve el error anterior.                                                                                            |
| Cuánto se conserva                       | **[?]** Hoy no vence. Falta definir una política de limpieza.                                                                                                                                |

### Fallas parciales

| Falla                                                                                  | Qué pasa                                                                                                                                                                                  |
| -------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| El servicio se cae después de confirmar la transacción y antes de responder            | El cliente no recibe respuesta y reintenta con la misma clave: obtiene la reserva ya creada. No se duplica.                                                                                |
| El cliente da timeout y no sabe si la reserva se hizo                                  | Reintenta con la misma clave (ADR-005, punto 3). Si nunca llegó, se crea; si ya existía, se devuelve. Si no reintenta, la reserva **vence sola** y el stock no queda bloqueado para siempre. |
| Pago y vencimiento coinciden                                                           | Las dos operaciones bloquean la misma fila: gana una sola. Si gana el vencimiento, `confirmar` responde `409 RESERVA_VENCIDA`.                                                              |
| Dos réplicas intentan vencer o enviar lo mismo                                         | El bloqueo de la fila y `SKIP LOCKED` hacen que una procese y la otra salte.                                                                                                                |
| La réplica se cae mientras envía una reposición al gimnasio                            | La reposición quedó apartada 30 s; pasado ese plazo otra réplica la toma. El gimnasio recibe la misma clave de idempotencia, así que no duplica la mercadería **si respeta la clave** (contrato supuesto, ADR-009). |
| La base está caída                                                                     | Ninguna operación se aplica a medias: no hay transacción confirmada. Al volver, el servicio sigue sin intervención.                                                                       |
| El evento `ReservaVencida` no se llega a publicar                                      | Está en el outbox, guardado en la misma transacción que el cambio de estado. Se publicará cuando exista el publicador (ver Pendientes); no se pierde.                                       |

### Aislamiento y restricciones de la base

Se usa el nivel por defecto de MySQL (`REPEATABLE READ`). Las condiciones críticas no dependen de lecturas previas: van dentro de la `UPDATE` condicional o de un `SELECT … FOR UPDATE`, que leen la última versión confirmada. Además, la base impone invariantes que ningún bug de la aplicación puede saltear:

```sql
CHECK (online >= 0)
CHECK (reservado >= 0 AND reservado <= online)
```

El stock físico no tiene esa restricción a propósito (RN-06, RN-25).

## Alternativas consideradas

| Alternativa                                                               | Por qué se descartó                                                                                                                                                                      |
| ------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Leer el stock, decidir en la aplicación y escribir**                    | Dos reservas simultáneas pueden leer el mismo valor y vender de más. Habría que bloquear de todos modos.                                                                                  |
| **Bloqueo optimista con un número de versión**                            | Con las últimas unidades, la mayoría de los intentos fallaría y habría que reintentar. La actualización condicional resuelve lo mismo en una sola sentencia.                              |
| **Bloqueo de la tabla o un `SERIALIZABLE` general**                       | Serializa todo Inventario, incluidas variantes distintas.                                                                                                                                |
| **Un bloqueo distribuido (Redis o similar)**                              | Agrega una dependencia y una nueva forma de fallar. La base ya es la fuente de verdad y ya tiene transacciones.                                                                           |
| **Confirmar con el evento `VentaPagada`**                                 | Repartía la carrera entre pago y vencimiento en dos servicios y obligaba a compensar (ADR-005, punto 4).                                                                                  |
| **Saga con compensaciones entre Ventas e Inventario**                     | La reserva con vencimiento ya cumple el papel de compensación: lo que no se confirma se libera solo.                                                                                       |

## Consecuencias

**Positivas**

- La corrección no depende de cuántas réplicas haya ni de en qué orden lleguen los pedidos.
- Las invariantes están en la base: aunque haya un error de código, el stock online no puede quedar negativo.
- Se pueden probar con concurrencia real (sección de Validación).

**Negativas aceptadas**

- **Los pedidos sobre la misma variante se serializan** por el bloqueo de la fila. Una variante muy pedida a la vez es un cuello de botella (ADR-013).
- **El resultado de un reintento de ingreso o transferencia es el stock actual**, no la respuesta original. Para reservas sí se devuelve la reserva guardada, pero con su estado actual, que puede haber cambiado.
- **Las claves de idempotencia no se limpian.** La tabla crece con el uso.
- **Reponer descuenta el stock físico antes de que el gimnasio confirme.** Mientras la reposición está pendiente, esas unidades no están en ningún stock. Si el gimnasio la rechaza, vuelven.
- **La entrega del evento `ReservaVencida` todavía no existe** (no hay publicador), por lo que Ventas no se entera del vencimiento. Mientras tanto, la reserva vence y libera su stock igual.

## Pendientes

- **Publicador del outbox hacia RabbitMQ y consumidor idempotente en Ventas** (ADR-005, punto 5).
- **Política de limpieza** de claves de idempotencia y del outbox ya publicado.
- **Ventas:** transiciones condicionales de la venta (`UPDATE … WHERE estado = …`) y procesamiento idempotente del aviso de Mercado Pago. Se documentan cuando se implemente el servicio.
- **Descuento del stock físico por venta del local** y ajustes de stock (HU-08, HU-17): todavía no existen en Inventario.

## Validación

Tests de integración contra un **MySQL real**, en `services/inventario/internal/adaptadores/mysql/repositorio_test.go`. Se corrieron 7 pruebas con el detector de condiciones de carrera de Go (`-race`) y se repitieron 15 veces sin fallas:

| Test                                          | Qué comprueba                                                                                      |
| --------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `TestReservasConcurrentesSobreLaUltimaUnidad` | Diez compradores piden la última unidad a la vez: una sola reserva, el stock no queda negativo.     |
| `TestReservaTodoONada`                        | Si una variante no alcanza, no se aparta nada de las demás.                                        |
| `TestReservaIdempotenteConcurrente`           | Varios intentos simultáneos con la misma clave producen una sola reserva.                          |
| `TestConfirmarYLiberarMuevenElStock`          | Confirmar descuenta definitivamente; liberar devuelve lo apartado.                                 |
| `TestTransferenciaYReservaConcurrentes`       | Una transferencia y una reserva sobre las últimas unidades: solo una usa esas unidades.            |
| `TestTransferenciaNoMueveLoReservado`         | No se transfiere lo que está reservado.                                                            |
| `TestVencimientoGuardaEventoEnOutbox`         | Al vencer una reserva se guarda el evento en el outbox, en la misma transacción.                   |

Se ejecutan con:

```bash
docker compose up -d mysql-inventario
INVENTARIO_TEST_DSN="inventario:cambiar@tcp(localhost:3307)/inventario?parseTime=true&loc=UTC&multiStatements=true" \
  go test -race ./services/inventario/...
```

Las pruebas se corrieron contra MySQL 8.0 y el Compose usa MySQL 8.4; conviene repetirlas contra el contenedor del proyecto y dejar el resultado en el README del servicio.

**Lo que los tests todavía no cubren** (el comportamiento está implementado, pero sin prueba automática):

- La carrera entre `confirmar` y el vencimiento sobre la misma reserva (hoy solo se prueba la regla en el dominio, sin concurrencia).
- Dos réplicas venciendo o enviando reposiciones a la vez (`FOR UPDATE` y `SKIP LOCKED`).
- Dos transferencias simultáneas sobre la misma variante.
- La respuesta `409 CLAVE_IDEMPOTENCIA_REUTILIZADA` de punta a punta.

**[propuesta]** Para la defensa: repetir las pruebas de concurrencia contra el servicio completo (varios pedidos simultáneos por el gateway sobre una variante con una sola unidad) y mostrar un solo `201` con el resto en `409 STOCK_INSUFICIENTE`.
