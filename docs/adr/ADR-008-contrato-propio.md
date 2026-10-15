# ADR-008 · Contrato propio

|                             |                                                                                                                                                                                           |
| --------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Estado**                  | Aceptada · Entrega 2 · Actualizada el 15/10/2026: contrato 1.1.0 con avisos por webhook (punto 5)                                                                                         |
| **Fecha**                   | 14/10/2026 (la propuesta inicial es del 09/10/2026; ver _Historia de la decisión_)                                                                                                        |
| **Decisión de la consigna** | D8 · Contrato propio                                                                                                                                                                      |
| **Relacionadas**            | [ADR-001](ADR-001-limites-de-los-servicios.md) (límites de los servicios), [ADR-003](ADR-003-persistencia.md) (persistencia), [ADR-005](ADR-005-comunicacion.md) (comunicación y reserva) |
| **Contrato**                | [`contracts/README.md`](../contracts/README.md), [`catalogo-pedidos-v1.openapi.yaml`](../contracts/catalogo-pedidos-v1.openapi.yaml)                                                      |

> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar.

## Contexto

La consigna (sección 4) pide publicar una capacidad para que la use otro grupo:

- con un **contrato formal y versionado**, en un formato estándar y procesable, guardado en `docs/contracts/`;
- que indique operaciones, datos, errores, autenticación, idempotencia, versión vigente y ejemplos;
- utilizable **sin explicaciones privadas**;
- incorporada por el consumidor en **un flujo importante de su negocio**;
- con mock en la Entrega 1, funcionando en la Entrega 2 y desplegada con URL pública para la presentación.

La cátedra asignó como consumidor al **grupo de proveedores**: un sistema que compra a sus proveedores. FAV es uno de ellos. Les sirve ver el catálogo de FAV y hacerle pedidos, que descuenten stock y generen ventas. La cátedra pidió como mínimo dos operaciones.

Además, FAV es un sistema para un cliente real. La capacidad existe por la materia, así que no tiene que agregar trabajo que la tienda no vaya a usar después.

## Criterios

1. **Que le sirva al consumidor** en un flujo con consecuencias reales: comprar.
2. **Que reutilice lo que FAV ya necesita** (catálogo, reserva de stock, venta por transferencia) sin servicios nuevos ni flujos que la tienda no use.
3. **Que se pueda usar solo con el contrato:** reglas claras, códigos de error estables y ejemplos.
4. **Que sea seguro reintentar:** un error de red no duplica pedidos.
5. **Que se pueda desactivar** cuando el sistema pase al cliente, sin tocar la lógica de FAV.

## Decisión

### 1. Qué se publica: catálogo y pedidos

| Operación       | Ruta                | Servicio | Qué hace                                                                                       |
| --------------- | ------------------- | -------- | ---------------------------------------------------------------------------------------------- |
| Ver el catálogo | `GET /v1/productos` | Catálogo | Devuelve todos los productos publicados con sus variantes (talle y color) y el precio vigente. |
| Hacer un pedido | `POST /v1/pedidos`  | Ventas   | Aparta el stock y registra la venta, pendiente de pago por transferencia.                      |

**El catálogo:**

- **No informa stock.** Si una variante no alcanza, el pedido lo dice. Mostrar disponibilidad obligaría a Catálogo a consultar a Inventario en cada llamada, o a mantener una copia por eventos, y hoy no hace falta.
- **Sin paginación.** El catálogo de FAV es del orden de cientos de productos. Va dentro de `items` para poder sumar una paginación opcional sin romper el contrato.
- **Foto y descripción opcionales,** para no depender de dónde se guarden las fotos (ADR-003).

**El pedido:**

- **Lo hace el grupo para sí mismo.** No se envían datos de clientes finales; el comprador es el grupo, identificado por su clave.
- **Todo o nada:** si una variante no alcanza, no se aparta ninguna (`409 STOCK_INSUFICIENTE`, con las faltantes).
- **El precio lo calcula FAV** al momento del pedido (RN-22) y se devuelve en la respuesta.
- **Queda pendiente de pago por transferencia,** con el stock **online [propuesta]** apartado por 48 horas. Desde ahí sigue como cualquier compra por transferencia (CU-02): el administrador confirma la transferencia, o la venta vence y el stock se libera. Se reutiliza el flujo que la tienda ya tiene.
- **En un solo paso para el consumidor.** Por dentro, Ventas usa la misma reserva de Inventario que la compra online.
- **Límites [propuesta]:** hasta 20 variantes por pedido y hasta 10 unidades por variante.

### 2. Reglas comunes

| Aspecto            | Decisión                                                                                                                                                                                                           |
| ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Formato            | **OpenAPI 3.0.3**, REST sobre HTTPS con JSON.                                                                                                                                                                      |
| Entrada            | Por el gateway de FAV, no directo a los servicios.                                                                                                                                                                 |
| Autenticación      | Una **clave propia por grupo** en el header `X-API-Key` **[propuesta]**, validada en el gateway. **[?]** Cómo se entrega y se rota.                                                                                |
| Idempotencia       | `POST /v1/pedidos` exige `Idempotency-Key`. Misma clave y mismo cuerpo: misma respuesta, sin otro pedido. Misma clave con otro cuerpo: `409 CLAVE_IDEMPOTENCIA_REUTILIZADA`. Vigencia de 48 horas **[propuesta]**. |
| Errores            | **Problem Details (RFC 9457)** con un campo `codigo` estable. El contrato indica qué códigos conviene reintentar.                                                                                                  |
| Límite de uso      | 60 pedidos por minuto por grupo **[propuesta]**; el exceso recibe `429` con `Retry-After`.                                                                                                                         |
| Correlación        | Header `X-Correlation-Id`, que se devuelve siempre (ADR-005).                                                                                                                                                      |
| Separación interna | El catálogo y los pedidos entran por adaptadores aparte en Catálogo y en Ventas, que se pueden desactivar por configuración. Las ventas de grupos externos se registran con su propio canal.                       |

### 3. Publicación

| Etapa               | Qué se publica                                                                                                             |
| ------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| Entrega 2           | Contrato, `docs/contracts/README.md` y mock en `mocks/catalogo-pedidos-v1/`. La capacidad funcionando en el entorno local. |
| Presentación grupal | Desplegada en la nube con URL pública, operativa hasta el fin de la evaluación. **[?]** Proveedor de nube.                 |

### 4. Versionado y compatibilidad

- **Versionado semántico.** La versión mayor va en la ruta (`/v1/...`) y la completa en `info.version`.
- **Cambios compatibles** (suben la versión menor, misma ruta): operaciones nuevas, campos opcionales nuevos en los pedidos, campos nuevos en las respuestas, valores nuevos de `codigo`. **El consumidor debe ignorar los campos que no conoce.**
- **Cambios incompatibles** (nueva versión mayor, `/v2/...`): quitar o renombrar campos u operaciones, volver obligatorio un campo opcional, cambiar el tipo o el significado de un campo, de un estado o de un código, y cambiar la forma de autenticar o de firmar los avisos.
- **Si cambia un endpoint, se publica la versión nueva en otra ruta y la anterior tiene fecha de baja** (actualizado el 15/10/2026). La `v1` no se modifica: la `v2` convive con ella, con su propio contrato y mock, y la `v1` sigue habilitada hasta una fecha anunciada (deadline) con 1 semana de anticipación, nunca antes del fin de la evaluación. Desde el anuncio, las respuestas de la `v1` llevan los headers `Deprecation` y `Sunset` (RFC 8594) **[propuesta]**; pasada la fecha, responden `410 VERSION_RETIRADA` indicando la ruta nueva. La fecha se publica en la tabla de versiones del README de contratos. Se eligió la versión en la ruta, y no en un header o por fecha, porque es la más visible para un grupo que no participó del diseño y la más simple de rutear en el gateway (apunte _Comunicación entre servicios_, §2.1).
- Agregados compatibles previstos si el consumidor los necesita: disponibilidad en el catálogo y paginación opcional (sin parámetros sigue devolviendo todo). La consulta del estado de un pedido se agregó en la 1.1.0 (punto 5).
- Cada cambio se registra en el historial del README de contratos y en este ADR.

### 5. Avisos por webhook (contrato 1.1.0)

**Problema.** Con la 1.0.0, después de hacer el pedido el consumidor no podía saber por la API si FAV había verificado la transferencia o si el pedido había vencido (era una consecuencia negativa aceptada). Esos cambios los detecta FAV: el pago lo verifica el administrador y el vencimiento ocurre solo, a las 48 horas.

**Decisión.** Cuando el pedido cambia de estado, FAV le avisa al consumidor con un **webhook**, el patrón del apunte de comunicación para cuando el proveedor se entera del cambio antes que el consumidor. Se agrega además una consulta del pedido, porque el aviso informa un cambio pero no reemplaza a la fuente.

| Aspecto               | Decisión                                                                                                                                                                                                                                                              |
| --------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Registro del destino  | Campo opcional `notificacionUrl` en `POST /v1/pedidos`, documentado como `callback` de OpenAPI 3.0.3. No hay registro de URLs por grupo: el destino viaja con cada pedido.                                                                                            |
| Cuándo se envía       | Al pasar a `PAGADO`, `VENCIDO` o `A_VERIFICAR`.                                                                                                                                                                                                                       |
| Cuerpo                | Liviano: `eventoId`, `tipo`, `ocurridoEn` y `pedido` (`pedidoId`, `referenciaExterna`, `estado`). El detalle se obtiene con `GET /v1/pedidos/{pedidoId}`.                                                                                                             |
| Autenticidad          | Firma HMAC-SHA256 de `<timestamp>.<cuerpo>` con un secreto de avisos por grupo, en el header `X-FAV-Firma`, como el `x-signature` de Mercado Pago. El receptor rechaza firmas de más de 5 minutos **[propuesta]**.                                                    |
| Duplicados y orden    | Entrega al menos una vez; el receptor deduplica por `eventoId`. Los estados solo avanzan, así que un aviso atrasado se reconoce y se ignora.                                                                                                                          |
| Reintentos            | Sin `2xx` en 5 s: a los 1, 5, 15 y 60 min. Con `401` no se reintenta.                                                                                                                                                                                                 |
| Implementación en FAV | En Ventas, con el mismo mecanismo que los mails (ADR-005 §6): el aviso pendiente se guarda en la misma transacción que el cambio de estado y un proceso lo envía después, con reintentos. Entra por el adaptador de la capacidad, que se desactiva por configuración. |
| Mock                  | Prism responde las operaciones y un servidor chico adelante envía el aviso firmado, porque el callback de Prism sale sin los headers de firma.                                                                                                                        |

**Alternativas consideradas** (apunte _Comunicación entre servicios_):

| Alternativa                                                  | Por qué no                                                                                                                                                                                                                             |
| ------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Solo agregar la consulta (el consumidor pregunta cada tanto) | Consultas sin novedades durante 48 horas y el cambio se ve tarde. Se mantiene igual la consulta como respaldo del webhook.                                                                                                             |
| Server-Sent Events                                           | El consumidor es un microservicio, no un navegador: tendría que mantener una conexión abierta y reconectarse con `Last-Event-ID`, y el gateway y el balanceador de la nube tendrían que sostenerla. Sin historial durable garantizado. |
| WebSockets                                                   | Canal bidireccional para un flujo que va en un solo sentido. Más ciclo de vida, autenticación y reconexión que resolver.                                                                                                               |
| gRPC (server streaming)                                      | Obliga al otro grupo a compartir el toolchain de `.proto` y no le sirve a la tienda.                                                                                                                                                   |
| GraphQL (subscriptions)                                      | Un solo consumidor y pocos datos: el esquema, los resolvers y el control de costo no se justifican.                                                                                                                                    |

**Consecuencias.** El consumidor se entera del pago o del vencimiento sin consultar. A cambio, FAV tiene que alcanzar la URL del consumidor (pública en el entorno desplegado), guardar y reintentar avisos, y entregar un secreto más por grupo. El receptor tiene que verificar la firma y deduplicar. Si los avisos fallan, la consulta sigue dando el estado vigente.

## Alternativas consideradas

| Alternativa                                                                 | Por qué se descartó                                                                                                                                                               |
| --------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Opción A de la Entrega 1: reserva de cupos con vencimiento** (Inventario) | Se pensó para un consumidor de otro dominio, sin ropa que comprar. El consumidor asignado sí compra ropa: necesita el catálogo y pedidos sobre stock real.                        |
| **Opción B de la Entrega 1: envío de emails** (Ventas)                      | Mismo motivo. Además, es poco central para un consumidor que compra.                                                                                                              |
| **Reservar y confirmar por separado** (tres o más operaciones)              | Obliga a exponer reservas y su vencimiento a otro sistema. El consumidor no lo pidió y la tienda no lo necesita: la confirmación ya existe como confirmación de la transferencia. |
| **Disponibilidad en el catálogo**                                           | Agrega una dependencia de Catálogo con Inventario (llamada o copia por eventos) sin un beneficio claro hoy. Se puede agregar después sin romper el contrato.                      |
| **Paginación en el servidor**                                               | El catálogo es chico; el consumidor pagina de su lado si quiere.                                                                                                                  |
| **Compra pagada al instante** (venta registrada como pagada)                | El pago no ocurre en el sistema. Dejarla pendiente de pago por transferencia reutiliza un flujo existente y no registra cobros que no ocurrieron.                                 |
| **Consultar el estado del pedido** (1.0.0)                                  | El consumidor no lo pidió. **Se agregó en la 1.1.0** junto con el webhook (punto 5), como respaldo de los avisos.                                                                 |
| **JWT con credenciales de cliente**                                         | Más piezas para los dos lados (emitir, renovar y vencer tokens) sin ganancia clara frente a una clave por grupo.                                                                  |
| **Servicio propio para la capacidad**                                       | Suma un servicio, una base y casos de falla para algo que existe por la materia (ADR-001).                                                                                        |

## Consecuencias

**Positivas**

- Reutiliza el catálogo, la reserva de stock y la compra por transferencia que la tienda necesita igual. Lo único nuevo es el canal _grupo externo_ y la clave por grupo.
- Las garantías de la tienda valen para el consumidor: sin sobreventa, todo o nada, sin pedidos duplicados.
- El consumidor tiene códigos de error estables y sabe cuándo reintentar.
- Se puede desactivar sin tocar la lógica de FAV.

**Negativas aceptadas**

- **Toca tres servicios:** Catálogo para el catálogo, Ventas para el pedido e Inventario para el stock. Para la URL pública hay que desplegar el gateway y los tres servicios con sus bases.
- **El consumidor no sabe si hay stock** hasta que hace el pedido.
- ~~**El consumidor no se entera por la API** de si el pago se acreditó o el pedido venció.~~ Resuelto en la 1.1.0 con el webhook y la consulta del pedido (punto 5). La entrega de un pedido pagado se sigue coordinando por el canal de contacto.
- **Para recibir avisos, el consumidor necesita un endpoint alcanzable desde FAV.** Si no lo tiene, puede consultar el pedido.
- **Un pico del consumidor puede afectar a la tienda,** porque comparte servicios. Se acota con el límite por grupo.

## Pendientes

- **Clave por grupo y secreto de avisos:** cómo se entregan y se rotan. **[?]**
- **Si el grupo de proveedores va a recibir avisos** en su flujo de compra, y su URL en el entorno desplegado. **[?]**
- **Datos para transferir** (alias o CBU y titular): los define el cliente. **[?]**
- **URL pública y proveedor de nube.** **[?]**
- **Respuesta del grupo de proveedores** sobre el contrato. Si piden cambios antes de que exista el servicio, se ajusta la v1.

## Validación prevista

- **Entrega 2:** el contrato pasa la validación de OpenAPI y el mock responde todos los ejemplos. El servicio real responde según el contrato: un test verifica que las respuestas cumplen el esquema, que un pedido repetido con la misma clave no duplica nada y que un pedido sin stock suficiente no aparta nada.
- **Presentación grupal:** el grupo de proveedores usa la URL pública desde su microservicio, en su flujo de compra.
- **Jornada de fallas:** con Ventas o Inventario caídos, el consumidor recibe `503` o un timeout. Al volver, los reintentos con la misma clave no duplican pedidos.
- **Avisos (1.1.0):** un test verifica que el aviso sale firmado y con el mismo `eventoId` en cada reintento, que con el receptor caído se reintenta y que, si nunca llega, la consulta devuelve el estado vigente. En la jornada de fallas se muestra el receptor caído y su recuperación.

## Historia de la decisión

| Fecha      | Estado                 | Qué pasó                                                                                                                                                                                                                                                                |
| ---------- | ---------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 09/10/2026 | Propuesta (Entrega 1)  | Sin consumidor asignado, se propusieron dos capacidades independientes del dominio de FAV: **A**, reserva de cupos con vencimiento (Inventario), y **B**, envío de emails (Ventas), con un contrato y un mock cada una. Se iba a publicar una sola según el consumidor. |
| 14/10/2026 | Aceptada               | La cátedra asignó como consumidor al grupo de proveedores, que compra ropa. Se descartan A y B (ver _Alternativas consideradas_) y se publica **catálogo y pedidos**. Los contratos y mocks de A y B se retiraron del repositorio; quedan en el historial de git.       |
| 15/10/2026 | Aceptada (actualizada) | Contrato **1.1.0**, compatible con la 1.0.0: avisos por webhook cuando el pedido se paga o vence, y consulta `GET /v1/pedidos/{pedidoId}` (punto 5). El mock envía los avisos firmados.                                                                                 |
