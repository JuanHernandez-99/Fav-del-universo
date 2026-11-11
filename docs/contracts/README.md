# Capacidad publicada por FAV · Catálogo y pedidos

> **Versión vigente: 1.3.0** · 11/11/2026 · Compatible con la 1.2.0: agrega la consulta de un pedido.
>
> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido por FAV, sin confirmar.

> El contrato del gimnasio, que FAV consume, está en [`externos/gimnasio/`](externos/gimnasio/) ([ADR-018](../adr/ADR-018-consumo-del-gimnasio-contrato-real.md)).

FAV es una tienda de ropa. Esta capacidad le permite a otros grupos **ver el catálogo de FAV, consultar la disponibilidad y hacerle pedidos**, como un comprador mayorista:

1. **Ver el catálogo:** productos publicados, sus variantes (talle y color) y el precio vigente.
2. **Consultar la disponibilidad:** cuántas unidades de cada variante se pueden apartar.
3. **Hacer un pedido:** FAV aparta el stock y registra la venta, que queda **pendiente de pago por transferencia**.
4. **Consultar un pedido:** su estado vigente (pendiente, pagado, vencido o a verificar).

La clave del gimnasio no habilita las operaciones de pedidos (3 y 4).

El pedido lo hace el grupo para sí mismo: no se envían datos de clientes finales.

| | |
|---|---|
| **Contrato** | [`catalogo-pedidos-v1.openapi.yaml`](catalogo-pedidos-v1.openapi.yaml) (OpenAPI 3.0.3). Es la fuente de verdad. Se puede ver en [Swagger Editor](https://editor.swagger.io) pegando el YAML. |
| **Mock** | [`mocks/catalogo-pedidos-v1/`](../../mocks/catalogo-pedidos-v1) |
| **Decisión de diseño** | [ADR-008](../adr/ADR-008-contrato-propio.md) |

---

## 1. Operaciones

| Operación | Ruta | Qué hace |
|---|---|---|
| Ver el catálogo | `GET /v1/productos` | Devuelve todos los productos publicados con sus variantes y el precio vigente. |
| Consultar la disponibilidad | `GET /v1/disponibilidad?variantes=a,b` | Unidades disponibles de cada variante. No aparta nada. |
| Hacer un pedido | `POST /v1/pedidos` | Aparta el stock y registra la venta, pendiente de pago por transferencia. Todo o nada. |
| Consultar un pedido | `GET /v1/pedidos/{pedidoId}` | Devuelve el pedido con su estado vigente. Desde la 1.3.0. |

### Ver el catálogo

- Devuelve solo productos **publicados**, dentro de `items`.
- Por producto: `productoId`, `nombre`, `categoria` y `variantes`. `descripcion` y `fotoUrl` son opcionales: pueden no venir.
- Por variante: `varianteId` (el que se usa en el pedido), `talle`, `color` y `precio` (precio vigente por unidad, en pesos, con la promoción aplicada si hay una).
- **No informa stock:** para eso está la disponibilidad. Si una variante no alcanza, el pedido responde `409 STOCK_INSUFICIENTE`.
- Devuelve el catálogo completo en una sola respuesta (del orden de cientos de productos). Si el consumidor quiere mostrarlo por páginas, lo hace de su lado.

### Consultar la disponibilidad

- `variantes`: entre 1 y 200 `varianteId` separados por coma. Por fuera de ese rango: `400 PEDIDO_INVALIDO`.
- Responde `items` con `varianteId` y `disponible`: unidades del **stock online** que no están reservadas. No incluye el stock físico.
- Una variante que no existe o que no tiene stock figura con `disponible: 0`, sin error.
- Es una foto del momento: no aparta unidades. Un pedido posterior puede responder `409 STOCK_INSUFICIENTE`.

### Hacer un pedido

- Se envían `items` (hasta 20 variantes, sin repetir) con `varianteId` y `cantidad` (de 1 a 10), y opcionalmente una `referenciaExterna` (el número de pedido en su sistema).
- **Todo o nada:** si una variante no alcanza, no se aparta ninguna.
- **El precio lo calcula FAV** al momento del pedido y se devuelve en la respuesta.
- El pedido queda `PENDIENTE_DE_PAGO`. La respuesta incluye el `pedidoId`, el total, los datos para transferir y `pago.venceEn`: el stock queda apartado **48 horas**.
- **Guardar el `pedidoId`:** es el que se usa para consultar el pedido.
- La respuesta del alta siempre dice `PENDIENTE_DE_PAGO`, también si se repite con la misma `Idempotency-Key` después de pagado: la idempotencia devuelve la misma respuesta. El estado vigente sale de la consulta.

### Consultar un pedido

- `GET /v1/pedidos/{pedidoId}` devuelve el mismo esquema que el alta; lo que cambia con el tiempo es `estado`.
- Es una lectura: no lleva `Idempotency-Key` y se puede repetir sin riesgo.
- Solo responde pedidos hechos con esta API. Un id que no existe, o que corresponde a otra venta de FAV, da `404 PEDIDO_INEXISTENTE`.
- Las consultas cuentan para el límite de uso del grupo (§2).

### Estados del pedido

| `estado` | Qué significa | ¿Puede cambiar? |
|---|---|---|
| `PENDIENTE_DE_PAGO` | El stock está apartado hasta `pago.venceEn`, esperando que FAV verifique la transferencia. Así se crea siempre. | Sí: a `PAGADO`, `VENCIDO` o `A_VERIFICAR`. |
| `PAGADO` | FAV verificó la transferencia y el stock se descontó. La entrega se coordina por el contacto (§7). | No. |
| `VENCIDO` | Pasaron las 48 horas sin que FAV verifique el pago; el stock se liberó. Para volver a comprar, hacer un pedido nuevo con otra `Idempotency-Key`. | Solo a `A_VERIFICAR`, si FAV recibe una transferencia tardía. |
| `A_VERIFICAR` | FAV recibió el pago con el pedido ya vencido. Lo revisa a mano y se comunica por el contacto **[?]** (reintegro o nueva reserva). | No por la API. |

- **Pueden agregarse estados en una versión menor.** Un estado que el consumidor no conoce se trata como "en revisión": no tomar ninguna acción automática y consultar por el contacto.
- FAV no envía avisos: el grupo se entera consultando.

### Cómo seguir un pedido (para el grupo consumidor)

```
POST /v1/pedidos ──► guardar pedidoId ──► transferir el total
                                                │
          GET /v1/pedidos/{pedidoId} cada tanto ◄┘   mientras esté PENDIENTE_DE_PAGO
                       │
                       ├─ PAGADO      → marcar la orden como pagada y dejar de consultar
                       ├─ VENCIDO     → la orden no se concretó; si sigue haciendo falta, pedido nuevo
                       └─ A_VERIFICAR u otro → "en revisión": consultar por el contacto
```

Se sugiere consultar no más de una vez por minuto por pedido **[propuesta]**. Pasado `pago.venceEn`, una consulta más alcanza para ver el resultado.

### Qué hace FAV de su lado

| Paso | Quién | Estado que ve el grupo |
|---|---|---|
| Llega el pedido | Ventas aparta el stock online en Inventario y avisa por mail al administrador ("transferencia pendiente"). | `PENDIENTE_DE_PAGO` |
| Llega la transferencia | El administrador la confirma en el panel (*Confirmar transferencia*). Inventario descuenta el stock en segundo plano; mientras tanto el pedido sigue pendiente. | `PAGADO` (unos segundos después) |
| Pasan 48 horas sin confirmar | Inventario vence la reserva y avisa a Ventas por RabbitMQ. | `VENCIDO` |
| Se confirma una transferencia con la reserva vencida | La venta queda *A verificar* y se avisa al administrador. | `A_VERIFICAR` |
| El administrador despacha o entrega | La venta avanza en el panel (*Entregada al correo*, *Entregada*). | Sigue `PAGADO` |

Los datos para transferir son los de FAV: alias `FAV.ALIAS`, titular `Cielo Stellantis`.

---

## 2. Autenticación, idempotencia y límites

**Autenticación.** Cada grupo recibe una clave propia y la envía en el header `X-API-Key`. Sin clave o con una clave inválida: `401 NO_AUTENTICADO`. FAV entrega la clave por mensaje directo a quien la pida; para rotarla, pedirla al contacto (§7).

**Idempotencia.** `POST /v1/pedidos` exige el header `Idempotency-Key`: una clave única por pedido (se recomienda un UUID).
- Ante un timeout, un `500` o un `503`, reintentar **con la misma clave y el mismo cuerpo**: se recibe la misma respuesta, sin crear otro pedido.
- La misma clave con otro cuerpo devuelve `409 CLAVE_IDEMPOTENCIA_REUTILIZADA`.
- La clave se conserva como mínimo 48 horas.

**Límite de uso.** 60 pedidos por minuto por grupo, sumando todas las operaciones, con ráfagas de 20. El exceso recibe `429` con el header `Retry-After`.

**Correlación.** Se puede enviar `X-Correlation-Id` para seguir una operación en los logs de los dos sistemas. FAV lo devuelve siempre; si no se envía, genera uno.

---

## 3. Errores

Todos los errores usan **Problem Details** (RFC 9457) con un campo `codigo` estable. **Decidir siempre por `codigo`**, no por los textos.

```json
{
  "type": "https://fav.example/errores/stock-insuficiente",
  "title": "Stock insuficiente",
  "status": 409,
  "detail": "Al menos una variante no alcanza. No se apartó ninguna unidad.",
  "codigo": "STOCK_INSUFICIENTE",
  "faltantes": [{ "varianteId": "var-remera-basica-blanca-m", "solicitada": 3 }]
}
```

| HTTP | `codigo` | Operación | Significado | ¿Reintentar? |
|---|---|---|---|---|
| 400 | `PEDIDO_INVALIDO` | Pedido, disponibilidad | Falta un header obligatorio, o el cuerpo o `variantes` no cumplen el contrato. `errores` indica los campos. | No. Corregir. |
| 400 | `VARIANTE_INEXISTENTE` | Pedido | Una variante no existe o su producto no está publicado. `variantes` indica cuáles. | No. Corregir. |
| 401 | `NO_AUTENTICADO` | Todas | Falta la clave, no es válida o no habilita la operación (la del gimnasio, en pedidos). | No. |
| 404 | `NO_ENCONTRADO` | Todas | La ruta no existe, o FAV apagó temporalmente la operación (`GET /v1/disponibilidad`, `GET /v1/pedidos/{pedidoId}`). | No. |
| 404 | `PEDIDO_INEXISTENTE` | Consulta de pedido | No hay un pedido con ese `pedidoId`. | No. |
| 409 | `STOCK_INSUFICIENTE` | Pedido | Al menos una variante no alcanza. No se apartó nada. `faltantes` indica cuáles. | No con las mismas cantidades. |
| 409 | `CLAVE_IDEMPOTENCIA_REUTILIZADA` | Pedido | La clave ya se usó con otro cuerpo. | No. Usar otra clave. |
| 429 | `LIMITE_EXCEDIDO` | Todas | Demasiados pedidos del grupo. | Sí, después de `Retry-After`. |
| 500 | `ERROR_INTERNO` | Todas | Error inesperado de FAV. | Sí, con la misma `Idempotency-Key` si es un pedido. |
| 503 | `NO_DISPONIBLE` | Todas | FAV o una dependencia no está disponible. | Sí, con la misma `Idempotency-Key` si es un pedido. |

**Recomendaciones para el consumidor [propuesta]:**
- Timeout de 3 segundos por llamada.
- Reintentar solo ante timeout, `429`, `500` o `503`, hasta 3 veces con espera creciente.
- Ignorar los campos de respuesta que no conozca (ver §5).

---

## 4. Ejemplos

```bash
# 1. Ver el catálogo
curl http://localhost:8090/v1/productos \
  -H "X-API-Key: clave-de-prueba"
# → 200 { "items": [ { "productoId": "prod-remera-basica", "nombre": "Remera básica",
#          "variantes": [ { "varianteId": "var-remera-basica-blanca-m", "talle": "M",
#                           "color": "Blanco", "precio": 15000 }, … ] }, … ] }

# Consultar la disponibilidad (servicios reales; clave local de prueba del gimnasio)
curl "http://localhost:8080/v1/disponibilidad?variantes=var-remera-basica-blanca-m,var-jean-mom-celeste-38" \
  -H "X-API-Key: clave-grupo-gimnasio"
# → 200 { "items": [ { "varianteId": "var-remera-basica-blanca-m", "disponible": 12 },
#                    { "varianteId": "var-jean-mom-celeste-38", "disponible": 0 } ] }

# 2. Hacer un pedido
curl -X POST http://localhost:8090/v1/pedidos \
  -H "X-API-Key: clave-de-prueba" -H "Content-Type: application/json" \
  -H "Idempotency-Key: 6f1c2b8e-3d4a-4e5f-9a7b-1c2d3e4f5a6b" \
  -d '{ "referenciaExterna": "OC-2026-0457",
        "items": [ { "varianteId": "var-remera-basica-blanca-m", "cantidad": 3 },
                   { "varianteId": "var-jean-mom-celeste-38", "cantidad": 1 } ] }'
# → 201 { "pedidoId": "7d9f2c1e-…", "estado": "PENDIENTE_DE_PAGO", "total": 87000, "moneda": "ARS",
#         "pago": { "medio": "TRANSFERENCIA", "venceEn": "2026-10-18T15:00:00Z",
#                   "datosTransferencia": { "alias": "FAV.ALIAS", "titular": "Cielo Stellantis" } }, … }

# 3. Consultar el pedido (servicios reales; el pedidoId es el que devolvió el alta)
curl http://localhost:8080/v1/pedidos/7d9f2c1e-4b3a-4e8f-9c2d-1a0b3c4d5e6f \
  -H "X-API-Key: clave-grupo-proveedores"
# → 200 { "pedidoId": "7d9f2c1e-…", "estado": "PAGADO", "total": 87000, … }
# → 404 { "codigo": "PEDIDO_INEXISTENTE" } si el id no es de un pedido

# En el mock: ver el pedido pagado
curl http://localhost:8090/v1/pedidos/7d9f2c1e-4b3a-4e8f-9c2d-1a0b3c4d5e6f \
  -H "X-API-Key: clave-de-prueba" -H "Prefer: example=pagado"

# 4. Ver un error en el mock (stock insuficiente)
curl -X POST http://localhost:8090/v1/pedidos \
  -H "Prefer: code=409" \
  -H "X-API-Key: clave-de-prueba" -H "Content-Type: application/json" \
  -H "Idempotency-Key: 6f1c2b8e-3d4a-4e5f-9a7b-1c2d3e4f5a6b" \
  -d '{ "items": [ { "varianteId": "var-remera-basica-blanca-m", "cantidad": 3 } ] }'
# → 409 { "codigo": "STOCK_INSUFICIENTE", "faltantes": [ … ] }
```

---

## 5. Versionado y compatibilidad

- La versión mayor va en la ruta (`/v1/...`) y la completa en `info.version` del contrato (versionado semántico).
- **Cambios compatibles** (suben la versión menor, misma ruta): operaciones nuevas, campos opcionales nuevos en los pedidos, campos nuevos en las respuestas, valores nuevos de `codigo` y estados nuevos del pedido. **El consumidor debe ignorar los campos que no conoce y tratar un estado desconocido como "en revisión".**
- **Cambios incompatibles** (nueva versión mayor, `/v2/...`): quitar o renombrar campos u operaciones, volver obligatorio un campo opcional, cambiar el significado de un estado o de un código.
- Si sale una `v2`, la `v1` sigue operativa hasta el fin de la evaluación.
- Posibles agregados compatibles a futuro: paginación opcional (sin parámetros seguiría devolviendo el catálogo completo) y avisos por webhook cuando cambia el estado de un pedido.

## 6. Entornos

| Entorno | URL | Estado |
|---|---|---|
| Mock local | `http://localhost:8090` | Disponible. Ver [`mocks/catalogo-pedidos-v1/`](../../mocks/catalogo-pedidos-v1). Responde ejemplos fijos. |
| Local, con los servicios reales | `http://localhost:8080` | Disponible con `docker compose up --build`. Claves de prueba: `clave-grupo-proveedores` y `clave-grupo-gimnasio`. |
| Desplegado (nube) | `https://fav-ucc.chilecentral.cloudapp.azure.com` | Disponible. Requiere la clave del grupo en `X-API-Key`. |

## 7. Contacto

Para reportar problemas, pedir cambios o resolver un pedido *A verificar*: 2212370@ucc.edu.ar.

## 8. Historial de versiones

| Versión | Fecha | Cambios |
|---|---|---|
| 1.3.0 | 11/11/2026 | Agrega `GET /v1/pedidos/{pedidoId}`, los estados `PAGADO`, `VENCIDO` y `A_VERIFICAR`, y el código `PEDIDO_INEXISTENTE`. Declara en el OpenAPI el `404 NO_ENCONTRADO` de `GET /v1/disponibilidad`, que ya figuraba en este README. Compatible con la 1.2.0. |
| 1.2.0 | 09/11/2026 | Documenta el código `NO_ENCONTRADO` (404), que el gateway ya devolvía. Define la URL pública, el contacto y cómo se entrega la clave. Compatible con la 1.1.0. |
| 1.1.0 | 08/11/2026 | Agrega `GET /v1/disponibilidad`, compatible con la 1.0.0, y una clave para el grupo del gimnasio. |
| 1.0.0 | 14/10/2026 | Primera versión: catálogo y pedidos. Reemplaza las propuestas de la Entrega 1 (reserva de cupos y envío de emails), descartadas al asignarse el grupo consumidor (ADR-008). |

**Aclaración sobre el archivo "1.1.0" del 15/10/2026.** Ese día se entregó un contrato también rotulado 1.1.0 que agregaba la consulta de un pedido, los estados `PAGADO`, `VENCIDO` y `A_VERIFICAR`, y avisos por webhook (`notificacionUrl` y `pedidoActualizado`, firmados con HMAC). Solo existió en ese archivo y en un mock propio: **el servicio real nunca lo implementó**. Se perdió del repositorio en un merge del 23/10, sin una decisión explícita, y el número 1.1.0 se volvió a usar el 08/11 para la disponibilidad. Por eso la 1.2.0 no tenía esas operaciones. La 1.3.0 reincorpora solo la consulta, con los mismos nombres de estados y de código de error que aquel archivo, para que lo ya programado contra el mock siga sirviendo. Los avisos por webhook y su firma quedan fuera por ahora. Las versiones ya publicadas no se renumeran.
