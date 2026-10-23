# ADR-009 · Consumo del proveedor: el gimnasio

|                             |                                                                                                                                                                                                                                                                                               |
| --------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Estado**                  | Aceptada con **contrato supuesto** · Entrega 2. Se revisa cuando el grupo del gimnasio publique el suyo.                                                                                                                                                                                      |
| **Fecha**                   | 23/10/2026                                                                                                                                                                                                                                                                                    |
| **Decisión de la consigna** | D9 · Consumo del proveedor                                                                                                                                                                                                                                                                    |
| **Relacionadas**            | [ADR-001](ADR-001-limites-de-los-servicios.md) (límites), [ADR-002](ADR-002-arquitectura-interna.md) (capa anticorrupción en el hexágono), [ADR-004](ADR-004-consistencia-y-concurrencia.md) (reposiciones), [ADR-005](ADR-005-comunicacion.md) (reintentos e idempotencia), [ADR-010](ADR-010-resiliencia.md) (resiliencia) |
| **Mock**                    | [`mocks/gimnasio-consignacion`](../../mocks/gimnasio-consignacion/README.md) (WireMock)                                                                                                                                                                                                       |

> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar.

## Contexto

La cátedra asignó a FAV **consumir la capacidad de un gimnasio**: un sistema de otro grupo que vende ropa de FAV en consignación y se queda con una comisión (SPEC §11). La consigna pide justificar cómo se incorpora la capacidad externa, cómo se adapta a su contrato y qué ocurre ante errores, cambios o indisponibilidad.

Lo que FAV necesita del gimnasio:

- **Consultar** cuánto stock de FAV le queda y cuánto vendió.
- **Registrar reposiciones**: enviarle prendas. FAV descuenta su stock físico y se lo informa.

**El grupo del gimnasio todavía no publicó su contrato.** Para no frenar el trabajo, FAV supuso uno y lo simula con un mock. Todo lo que sigue sobre rutas y campos del gimnasio es **supuesto** y cambia cuando llegue el real.

Restricciones de FAV: el gimnasio es un servicio que no controlamos (puede caerse, ser lento o cambiar), y una reposición ya descontó stock físico, por lo que **no puede perderse ni duplicarse** (ADR-004).

## Criterios

1. **Que la caída del gimnasio no frene a FAV:** el panel y el resto de Inventario siguen funcionando.
2. **Que un cambio de su contrato afecte a un solo lugar del código.**
3. **Que ninguna reposición se pierda ni se duplique** aunque haya fallas y reintentos.
4. **Que sea simple:** un contrato supuesto mínimo, sin mensajería ni servicios nuevos.

## Decisión

### 1. Capa anticorrupción

Inventario no usa el modelo del gimnasio dentro del núcleo. El adaptador `internal/adaptadores/gimnasio` implementa el puerto `Gimnasio` y traduce entre los dos modelos. El resto de Inventario no conoce sus rutas, nombres de campo ni errores.

### 2. Contrato supuesto

| Operación del gimnasio | Ruta supuesta | Cuerpo y respuesta |
| --- | --- | --- |
| Consultar el stock de FAV | `GET /api/v1/consignaciones/stock?proveedorId=fav` | `200` con `items[]`: `productoCodigo`, `unidadesDisponibles`, `unidadesVendidas` |
| Registrar una reposición | `POST /api/v1/consignaciones/reposiciones` | Cuerpo: `proveedorId`, `productoCodigo`, `unidades`, `referencia`. Respuesta `201` `{reposicionId, estado: RECIBIDA}` |
| Rechazo de una reposición | (misma ruta) | `422 CAPACIDAD_EXCEDIDA` con más de 20 unidades (supuesto del mock) |

| Concepto de FAV | Concepto del gimnasio |
| --- | --- |
| `varianteId` | `productoCodigo` (FAV identifica la prenda por su variante) |
| Reposición (`id`) | `referencia` |
| Clave de idempotencia de la reposición | Encabezado `Idempotency-Key` |
| Identidad de FAV | `proveedorId` = `fav` y encabezado `X-API-Key` (valor por configuración) |

**[?]** Cómo identifica el gimnasio los productos de FAV, cómo autentica y si respeta la clave de idempotencia. Cada una de estas suposiciones es un riesgo hasta que llegue el contrato (ver Consecuencias).

### 3. Qué se hace ante cada resultado

| Situación                                                          | Qué hace Inventario                                                                                                                                                  |
| ------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| El gimnasio responde `2xx`                                         | La reposición pasa a _ENVIADA_.                                                                                                                                       |
| Timeout (2 s), error de red, `5xx` o `429`                         | Falla **transitoria**: la reposición queda _PENDIENTE_ y se reintenta con espera creciente (5 s, 10 s, 20 s… hasta 5 min), sin límite de intentos.                    |
| Respuesta `4xx` (el gimnasio entendió y rechazó)                   | Rechazo **definitivo**: la reposición pasa a _RECHAZADA_ y las unidades **vuelven al stock físico**, con su movimiento.                                              |
| Circuit breaker abierto (5 fallas seguidas)                        | Falla de inmediato, sin esperar el timeout. A los 30 s deja pasar una llamada de prueba.                                                                              |
| Consulta de stock con el gimnasio caído o con formato inesperado   | El panel recibe `503 GIMNASIO_NO_DISPONIBLE` con `Retry-After: 30`. Un cambio de formato no avisado se trata como indisponibilidad, no como error propio.             |

### 4. Cómo se envía una reposición

1. `POST /v1/reposiciones` descuenta el stock físico y guarda la reposición _PENDIENTE_ **en la misma transacción** (ADR-004). Responde `202`: el envío sigue en segundo plano.
2. Un proceso, cada 5 s y en cada réplica, toma las pendientes con `FOR UPDATE SKIP LOCKED` y las envía una por una.
3. El envío lleva como `Idempotency-Key` la clave de la reposición: si llegó pero se perdió la respuesta, el reintento no duplica la mercadería en el gimnasio (siempre que el gimnasio respete la clave).

### 5. Protecciones

| Mecanismo               | Valor                                                                                 |
| ----------------------- | ------------------------------------------------------------------------------------- |
| Timeout por llamada     | 2 s (`GIMNASIO_TIMEOUT_MS`)                                                           |
| Circuit breaker         | Se abre tras 5 fallas consecutivas; 30 s abierto; una llamada de prueba al reabrir     |
| Qué cuenta como falla   | Timeout, error de red, `5xx` y `429`. Un `4xx` **no** abre el circuito                 |
| Reintentos              | Persistidos en la base (no en memoria), con espera creciente de 5 s a 5 min            |
| Métricas                | `dependencia_llamadas_total{dependencia="gimnasio",resultado}` y `circuit_breaker_estado` |

## Alternativas consideradas

| Alternativa                                                               | Por qué se descartó                                                                                                                                                              |
| ------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Usar el modelo del gimnasio dentro de Inventario**                      | Cualquier cambio de su contrato tocaría las reglas de negocio.                                                                                                                    |
| **Llamar al gimnasio dentro de la transacción de la reposición**          | Un gimnasio lento o caído bloquearía el panel y mantendría abierta una transacción de base. Con el envío en segundo plano, FAV sigue funcionando.                                  |
| **Reintentar en memoria**                                                 | Si el servicio se reinicia, se pierde la reposición. Persistida en la base, la retoma otra réplica.                                                                                |
| **Mensajería (RabbitMQ) hacia el gimnasio**                               | El gimnasio no expone un broker: expone una API HTTP. Agregaría una pieza sin necesidad.                                                                                          |
| **Sin circuit breaker, solo timeout**                                     | Con el gimnasio caído, cada consulta del panel esperaría el timeout completo. El circuito falla rápido y libera recursos.                                                          |
| **Cachear el stock del gimnasio**                                         | Es un dato de consulta ocasional del administrador. No hay una necesidad observable que lo justifique (ver ADR-007).                                                               |
| **Esperar el contrato real antes de implementar**                         | Bloquearía el servicio. La capa anticorrupción permite avanzar con un contrato supuesto y cambiar un solo paquete después.                                                         |

## Consecuencias

**Positivas**

- La caída del gimnasio solo afecta a "ver su stock" y "enviarle reposiciones"; Inventario sigue atendiendo reservas.
- Las reposiciones sobreviven a reinicios y a la caída de una réplica.
- El contrato real, cuando llegue, impacta en `adaptadores/gimnasio` y en los _mappings_ del mock.

**Negativas aceptadas**

- **Todo el contrato es supuesto.** Si el real difiere, hay que rehacer el adaptador y el mock.
- **Un `4xx` se interpreta siempre como rechazo definitivo** y devuelve las unidades al stock. Esto es riesgoso ante errores de configuración: una `X-API-Key` mal cargada (`401`/`403`) o una ruta que cambió (`404`) rechazarían todas las reposiciones y reintegrarían el stock, cuando en realidad la mercadería quizás ya está en camino o el problema es propio. **Mejora propuesta:** tratar `401`, `403`, `404` y `408` como fallas transitorias o de configuración (reintentar y alertar), y reservar el rechazo definitivo para `409`/`422`.
- **Depende de que el gimnasio respete la clave de idempotencia.** Si no lo hace, una caída entre "enviado" y "marcado como enviado" puede duplicar el envío.
- **Entre el pedido de reposición y la confirmación, las unidades no figuran en ningún stock.**
- **Reintentos sin límite:** una reposición que el gimnasio nunca acepta ni rechaza queda pendiente para siempre. Hace falta una métrica y una alerta de reposiciones antiguas.
- **No se registran las ventas del gimnasio ni la comisión** (fuera de la v1, SPEC §11).

## Pendientes

- **Contrato real del gimnasio.** Se pidió al grupo. Cuando llegue: actualizar este ADR, el adaptador, los _mappings_ y las suposiciones de arriba.
- **Test de contrato** sobre la capacidad consumida (lo exige la consigna para la defensa): verificar que el adaptador entiende las respuestas del contrato publicado y que un cambio incompatible se detecta.
- **Revisar la clasificación de los `4xx`** (ver arriba).
- **Alerta por reposiciones pendientes con muchos intentos.**

## Validación

- **Caída del gimnasio:** `docker compose stop gimnasio-mock`. La consulta de consignación responde `503 GIMNASIO_NO_DISPONIBLE`; una reposición nueva responde `202` y queda _PENDIENTE_. Al hacer `start`, se envía sola.
- **Lentitud:** `curl -X POST localhost:8095/__admin/settings -d '{"fixedDelay": 3000}'` supera el timeout de 2 s y debe abrir el circuito tras 5 fallas (`circuit_breaker_estado{dependencia="gimnasio"} = 2`).
- **Rechazo definitivo:** una reposición de más de 20 unidades devuelve `422` y las unidades vuelven al stock físico (`select estado, ultimo_error from reposiciones`).
- Las pruebas de integración del adaptador contra el mock y el test de contrato están **pendientes**.
