# Capacidad publicada por FAV · Catálogo y pedidos

> **Versión vigente: 1.1.0** · 08/11/2026
>
> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido por FAV, sin confirmar.

> El contrato del gimnasio, que FAV consume, está en [`externos/gimnasio/`](externos/gimnasio/) ([ADR-018](../adr/ADR-018-consumo-del-gimnasio-contrato-real.md)).

FAV es una tienda de ropa. Esta capacidad le permite a otros grupos **ver el catálogo de FAV, consultar la disponibilidad y hacerle pedidos**, como un comprador mayorista:

1. **Ver el catálogo:** productos publicados, sus variantes (talle y color) y el precio vigente.
2. **Consultar la disponibilidad:** cuántas unidades de cada variante se pueden apartar.
3. **Hacer un pedido:** FAV aparta el stock y registra la venta, que queda **pendiente de pago por transferencia**. La clave del gimnasio no habilita esta operación.

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

### Ver el catálogo

- Devuelve solo productos **publicados**, dentro de `items`.
- Por producto: `productoId`, `nombre`, `categoria` y `variantes`. `descripcion` y `fotoUrl` son opcionales: pueden no venir.
- Por variante: `varianteId` (el que se usa en el pedido), `talle`, `color` y `precio` (precio vigente por unidad, en pesos, con la promoción aplicada si hay una).
- **No informa stock:** para eso está la disponibilidad. Si una variante no alcanza, el pedido responde `409 STOCK_INSUFICIENTE`.
- Devuelve el catálogo completo en una sola respuesta (del orden de cientos de productos). Si el consumidor quiere mostrarlo por páginas, lo hace de su lado.

### Consultar la disponibilidad

- `variantes`: entre 1 y 200 `varianteId` separados por coma. Por fuera de ese rango: `400 PEDIDO_INVALIDO`.
- Responde `items` con `varianteId` y `disponible`: unidades del **stock online [propuesta]** que no están reservadas. No incluye el stock físico.
- Una variante que no existe o que no tiene stock figura con `disponible: 0`, sin error.
- Es una foto del momento: no aparta unidades. Un pedido posterior puede responder `409 STOCK_INSUFICIENTE`.

### Hacer un pedido

- Se envían `items` (hasta 20 variantes **[propuesta]**, sin repetir) con `varianteId` y `cantidad` (de 1 a 10 **[propuesta]**), y opcionalmente una `referenciaExterna` (el número de pedido en su sistema).
- **Todo o nada:** si una variante no alcanza, no se aparta ninguna.
- **El precio lo calcula FAV** al momento del pedido y se devuelve en la respuesta.
- El pedido queda `PENDIENTE_DE_PAGO`. La respuesta incluye el total, los datos para transferir y `pago.venceEn`: el stock queda apartado **48 horas**.

### Qué pasa después del pedido (fuera de la API)

```
POST /v1/pedidos ──► PENDIENTE_DE_PAGO ──(FAV verifica la transferencia)──► pagado: el stock se descuenta
                            │
                            └──(pasan 48 horas sin pago)──► vencido: el stock se libera
```

1. El grupo transfiere el total a los datos de `pago.datosTransferencia`.
2. FAV verifica la transferencia: la venta pasa a pagada y el stock se descuenta definitivamente.
3. Si no llega en 48 horas, el pedido vence y el stock se libera.

Consultar el estado de un pedido no está en la v1. Para saber si se acreditó un pago, usar el contacto (§7).

**[?]** Los datos bancarios reales los define el cliente. Los del ejemplo son ficticios.

---

## 2. Autenticación, idempotencia y límites

**Autenticación.** Cada grupo recibe una clave propia y la envía en el header `X-API-Key`. Sin clave o con una clave inválida: `401 NO_AUTENTICADO`. **[?]** Cómo se entrega y se rota la clave.

**Idempotencia.** `POST /v1/pedidos` exige el header `Idempotency-Key`: una clave única por pedido (se recomienda un UUID).
- Ante un timeout, un `500` o un `503`, reintentar **con la misma clave y el mismo cuerpo**: se recibe la misma respuesta, sin crear otro pedido.
- La misma clave con otro cuerpo devuelve `409 CLAVE_IDEMPOTENCIA_REUTILIZADA`.
- La clave vale 48 horas **[propuesta]**.

**Límite de uso.** 60 pedidos por minuto por grupo, sumando todas las operaciones **[propuesta]**. El exceso recibe `429` con el header `Retry-After`.

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
| 401 | `NO_AUTENTICADO` | Todas | Falta la clave, no es válida o no habilita la operación (la del gimnasio, en un pedido). | No. |
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
#                   "datosTransferencia": { "alias": "FAV.EJEMPLO.ALIAS", "titular": "FAV (ejemplo)" } }, … }

# 3. Ver un error en el mock (stock insuficiente)
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
- **Cambios compatibles** (suben la versión menor, misma ruta): operaciones nuevas, campos opcionales nuevos en los pedidos, campos nuevos en las respuestas, valores nuevos de `codigo`. **El consumidor debe ignorar los campos que no conoce.**
- **Cambios incompatibles** (nueva versión mayor, `/v2/...`): quitar o renombrar campos u operaciones, volver obligatorio un campo opcional, cambiar el significado de un estado o de un código.
- Si sale una `v2`, la `v1` sigue operativa hasta el fin de la evaluación.
- Posibles agregados compatibles a futuro: paginación opcional (sin parámetros seguiría devolviendo el catálogo completo) y consulta del estado de un pedido.

## 6. Entornos

| Entorno | URL | Estado |
|---|---|---|
| Mock local | `http://localhost:8090` | Disponible. Ver [`mocks/catalogo-pedidos-v1/`](../../mocks/catalogo-pedidos-v1). Responde ejemplos fijos. |
| Local, con los servicios reales | `http://localhost:8080` | Disponible con `docker compose up --build`. Claves de prueba: `clave-grupo-proveedores` y `clave-grupo-gimnasio`. |
| Desplegado (nube) | **[?]** | Antes de la presentación grupal. |

## 7. Contacto

**[?]** Canal para reportar problemas, pedir cambios o consultar el estado de un pago.

## 8. Historial de versiones

| Versión | Fecha | Cambios |
|---|---|---|
| 1.1.0 | 08/11/2026 | Agrega `GET /v1/disponibilidad`, compatible con la 1.0.0, y una clave para el grupo del gimnasio. |
| 1.0.0 | 14/10/2026 | Primera versión: catálogo y pedidos. Reemplaza las propuestas de la Entrega 1 (reserva de cupos y envío de emails), descartadas al asignarse el grupo consumidor (ADR-008). |
