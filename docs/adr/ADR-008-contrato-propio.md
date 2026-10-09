# ADR-008 · Contrato propio

| | |
|---|---|
| **Estado** | Propuesta · Entrega 1. Se acepta al elegir la opción según el grupo consumidor. |
| **Fecha** | 09/10/2026 |
| **Decisión de la consigna** | D8 · Contrato propio |
| **Relacionadas** | [ADR-001](ADR-001-limites-de-los-servicios.md) (dónde vive la capacidad), [ADR-003](ADR-003-persistencia.md) (datos de otros grupos), [ADR-005](ADR-005-comunicacion.md) (reservas y envío de mails) |
| **Contratos** | [`contracts/README.md`](../contracts/README.md), [`cupos-v1.openapi.yaml`](../contracts/cupos-v1.openapi.yaml), [`emails-v1.openapi.yaml`](../contracts/emails-v1.openapi.yaml) |

> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar.

## Contexto

La consigna (sección 4) pide publicar una capacidad para que la use otro grupo:

- con un **contrato formal y versionado**, en un formato estándar y procesable, guardado en `docs/contracts/`;
- que indique operaciones, datos, errores, autenticación, idempotencia, versión vigente y ejemplos;
- utilizable **sin explicaciones privadas**;
- incorporada por el consumidor en **un flujo importante de su negocio**, llamada desde su microservicio y no desde su frontend ni su gateway;
- con un mock en la Entrega 1 y desplegada en la nube con URL pública para la presentación.

Los profesores asignan qué grupo consume a qué grupo. **Todavía no sabemos quién nos consume**, pero sí que tiene otro dominio: probablemente no venda ropa.

## Criterios

1. **Que le sirva a un dominio distinto del nuestro,** en un flujo con consecuencias reales para su negocio.
2. **Que reutilice lo que FAV ya necesita,** sin sumar servicios (ADR-001).
3. **Que el consumidor pueda usarla solo con el contrato:** reglas claras, errores con códigos estables, ejemplos.
4. **Que sea segura de reintentar:** un error de red no duplica nada.
5. **Que se pueda quitar al terminar la materia,** sin tocar la lógica de FAV.

## Decisión

### 1. Dos opciones; se publica una sola

| | **A · Reserva de cupos con vencimiento** | **B · Envío de emails** |
|---|---|---|
| Qué ofrece | El consumidor registra cupos (un identificador y una cantidad), los reserva por un tiempo y los confirma o libera. Si no hace nada, vencen solos. Nunca se reserva más de lo disponible. | El consumidor manda destinatario, asunto y texto ya armados, para enviar ya o programado. FAV lo envía con reintentos; el consumidor consulta el estado o lo cancela antes del envío. |
| Ejemplo de uso | Una pastelería aparta un cupo de producción del día hasta que el cliente retira. | Un sistema de turnos programa el recordatorio del día anterior. |
| Servicio de FAV | Inventario, con su mecanismo de reserva | Ventas, con su envío de mails |
| Operaciones | `PUT /v1/cupos/{cupoId}`, `GET /v1/cupos/{cupoId}`, `POST /v1/reservas`, `POST /v1/reservas/{id}/confirmar`, `POST /v1/reservas/{id}/liberar` | `POST /v1/emails`, `GET /v1/emails/{id}`, `POST /v1/emails/{id}/cancelar` |
| Datos de otros grupos | Identificadores de cupos, cantidades y reservas | Destinatario, asunto, texto y estado de cada email |
| Riesgo principal | Si la operación central del consumidor ya es reservar, no la necesita. No maneja rangos de fechas. | Puede parecer secundaria como "flujo importante". El consumidor podría mandar mails por su cuenta. |

Se elige la opción cuando la cátedra asigne el grupo consumidor y se conozca su flujo. **[?]** Elección pendiente.

### 2. Diseño común a las dos opciones

| Aspecto | Decisión |
|---|---|
| Formato | **OpenAPI 3.0.3**, un archivo por opción. REST sobre HTTPS con JSON. **[?]** Formato consultado a la cátedra. |
| Independencia del dominio | Los identificadores los elige el consumidor (`cupoId`, `referenciaExterna`). FAV no interpreta qué representan. |
| Autenticación | Una **clave propia por grupo** en el header `X-API-Key` **[propuesta]**. La valida el gateway y separa los datos de cada grupo. Lo de otro grupo responde como inexistente (`404`). **[?]** Cómo se entrega y se rota la clave. |
| Idempotencia | Las operaciones que crean algo exigen `Idempotency-Key`. Misma clave y mismo cuerpo: mismo resultado, sin duplicar. Misma clave con otro cuerpo: `409 CLAVE_IDEMPOTENCIA_REUTILIZADA`. Vigencia de 48 h **[propuesta]**. Las operaciones sobre algo existente (confirmar, liberar, cancelar, registrar un cupo) son idempotentes por sí mismas. |
| Errores | **Problem Details (RFC 9457)** con un campo `codigo` estable. El contrato lista cada código, su significado y si conviene reintentar. |
| Límite de uso | Límite de pedidos por grupo en el gateway; el exceso recibe `429` con `Retry-After`. **[?]** Valor del límite. |
| Correlación | Header `X-Correlation-Id`, que se devuelve siempre (ADR-005). |
| Acciones | Cada transición tiene su propia ruta (`/confirmar`, `/liberar`, `/cancelar`) en lugar de un `PATCH` del estado: el consumidor no puede pedir transiciones inválidas y cada ruta tiene sus propios errores. |
| Separación interna | La capacidad entra por un adaptador aparte y guarda sus datos en tablas propias. Al terminar la materia se quita sin tocar la lógica de FAV (ADR-001, decisión 6). |

### 3. Publicación

| Etapa | Qué se publica |
|---|---|
| Entrega 1 | Contratos y `docs/contracts/README.md` en el repositorio. Mock con respuestas fijas en `mocks/` **[?]** (ver Pendientes). |
| Entrega 2 | La opción elegida, funcionando ("funcionalidad a compartir funcionando"). |
| Presentación grupal | Desplegada en la nube con URL pública, operativa hasta el fin de la evaluación. **[?]** Proveedor de nube. |

### 4. Versionado y compatibilidad

- **Versionado semántico.** La versión mayor va en la ruta (`/v1/...`) y la completa en `info.version`.
- **Cambios compatibles** (suben la versión menor, misma ruta): operaciones nuevas, campos opcionales nuevos en los pedidos, campos nuevos en las respuestas, valores nuevos de `codigo`. **El consumidor debe ignorar los campos que no conoce.**
- **Cambios incompatibles** (nueva versión mayor, `/v2/...`): quitar o renombrar campos u operaciones, volver obligatorio un campo opcional, cambiar el significado de un estado o de un código.
- Si sale una `v2`, **la `v1` sigue operativa** hasta el fin de la evaluación.
- Cada cambio se registra en el historial del README de contratos y actualiza este ADR (la consigna lo pide en la Entrega 2 si el contrato cambió).
- **La autenticación se define antes de publicar la URL pública:** agregar un header obligatorio después sería un cambio incompatible.

## Alternativas consideradas

| Alternativa | Por qué se descartó |
|---|---|
| **Publicar la reserva de stock de FAV (por variante)** | Era la primera propuesta. El consumidor tiene otro dominio y no tiene prendas que reservar: la integración sería forzada y no cumpliría el "flujo importante". |
| **Una sola opción genérica, sin alternativa** | No sabemos quién nos consume. Llevar dos opciones permite elegir según su flujo. |
| **Cobro con Mercado Pago** | Solo sirve a dominios con pagos. Convertiría el adaptador de pagos de Ventas en un servicio de pagos y cambia la arquitectura (ADR-001). |
| **Usuarios y roles compartidos** | Guardaríamos usuarios de otro grupo y seríamos una dependencia crítica para su inicio de sesión, que además tiene que validar su propio gateway. |
| **Búsqueda o catálogo** | Cada grupo tiene que implementar su propia búsqueda (requisito 2.6). |
| **Servicio propio para la capacidad** | Suma un servicio, una base y casos de falla para algo que existe por la materia (ADR-001). |
| **Eventos hacia el consumidor (AsyncAPI, broker compartido)** | Exige compartir RabbitMQ entre grupos, con red, credenciales y formato de mensajes en común. Es más frágil con un sistema que no controlamos. |
| **Avisos al consumidor por webhook** (reserva vencida, email enviado) | Exige que el consumidor exponga y proteja un endpoint propio. En v1 consulta el estado. Agregarlo después es un cambio compatible. |
| **JWT con credenciales de cliente** | Más piezas para los dos lados (emitir, renovar y vencer tokens) sin una ganancia clara frente a una clave por grupo, con pocos consumidores. |
| **Plantillas de email en FAV** | Obligaría a guardar contenido del consumidor y a definir un lenguaje de plantillas. En v1 el texto llega armado; agregarlas después es compatible. |

## Consecuencias

**Positivas**

- Cualquiera de las dos opciones sirve a un dominio distinto, sin sumar servicios.
- La opción A reutiliza la operación crítica de FAV y la expone con las mismas garantías: sin sobreventa, todo o nada, sin duplicados.
- La opción B reutiliza el envío de mails con reintentos y cola de fallidos.
- El consumidor tiene códigos de error estables y sabe cuándo reintentar.
- Se puede quitar al terminar la materia.

**Negativas aceptadas**

- **Se mantienen dos contratos hasta elegir.** Después, el descartado se retira del repositorio y queda registrado acá.
- **La capacidad comparte servicio con FAV:** si cae Inventario (A) o Ventas (B), cae también, y un pico del consumidor podría afectar a ese servicio. Se acota con el límite por grupo.
- **FAV guarda datos de otro grupo.** Con B son datos de personas (direcciones de email), y hay que definir cuánto tiempo se conservan.
- **Con B se despliega Ventas en la nube,** no Inventario.
- **El consumidor consulta el estado en lugar de recibir avisos.**

## Pendientes

- **Elegir la opción** según el grupo consumidor. **[?]**
- **Clave por grupo:** cómo se entrega y se rota.
- **Límite de pedidos por grupo.**
- **Duración máxima de una reserva (A):** hoy 48 horas **[propuesta]**. Un máximo menor limita cuánto puede bloquear otro grupo.
- **Proveedor de mail, remitente y conservación de los mensajes (B).**
- **Mocks:** `mocks/cupos-v1/` y `mocks/emails-v1/` con respuestas fijas según los ejemplos de cada contrato. **[?]** Si se entregan los dos o solo el de la opción elegida.
- **URL pública y proveedor de nube.**
- **Canal de contacto con el consumidor** para reportar problemas o pedir cambios.

## Validación prevista

- **Entrega 1:** los contratos pasan la validación de OpenAPI 3 y el mock responde según los ejemplos.
- **Entrega 2:** la opción elegida responde según el contrato; un test verifica que las respuestas reales cumplen el esquema.
- **Jornada de fallas:** con el servicio que expone la capacidad caído, el consumidor recibe un error o un timeout y aplica su propio tratamiento. Con el servicio de vuelta, los reintentos con la misma clave no duplican nada.
