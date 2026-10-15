# Capacidad publicada por FAV · Catálogo y pedidos

> **Versión vigente: 1.1.0** · 15/10/2026 · Compatible con la 1.0.0: un consumidor de la 1.0.0 sigue funcionando sin cambios.
>
> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido por FAV, sin confirmar.

FAV es una tienda de ropa. Esta capacidad le permite a otro grupo **ver el catálogo de FAV y hacerle pedidos**, como un comprador mayorista:

1. **Ver el catálogo:** productos publicados, sus variantes (talle y color) y el precio vigente.
2. **Hacer un pedido:** FAV aparta el stock y registra la venta, que queda **pendiente de pago por transferencia**.
3. **Enterarse de cómo sigue el pedido** *(nuevo en 1.1.0)*: FAV avisa por **webhook** cuando el pedido se paga o vence, y el estado se puede consultar en cualquier momento.

El pedido lo hace el grupo para sí mismo: no se envían datos de clientes finales.

| | |
|---|---|
| **Contrato** | [`catalogo-pedidos-v1.openapi.yaml`](catalogo-pedidos-v1.openapi.yaml) (OpenAPI 3.0.3). Es la fuente de verdad. Se puede ver en [Swagger Editor](https://editor.swagger.io) pegando el YAML; el webhook aparece en la pestaña *Callbacks* de `POST /v1/pedidos`. |
| **Mock** | [`mocks/catalogo-pedidos-v1/`](../../mocks/catalogo-pedidos-v1): responde las operaciones y envía el webhook firmado. |
| **Decisión de diseño** | [ADR-008](../adr/ADR-008-contrato-propio.md) |

## Novedades de la 1.1.0 y por qué un webhook

En la 1.0.0, después de hacer el pedido el grupo no tenía forma de saber por la API si FAV ya había verificado la transferencia o si el pedido había vencido: había que preguntar por el contacto.

Quien se entera primero de esos cambios es FAV: el pago lo verifica el administrador y el vencimiento ocurre solo, a las 48 horas. Por eso **FAV le avisa al grupo** con un webhook en el momento en que pasa, en lugar de que el grupo consulte cada tanto sin saber cuándo va a haber novedades. Para no depender solo del aviso (puede perderse si el receptor está caído), se agrega también la **consulta del pedido**, que siempre devuelve el estado vigente.

| Qué se agrega | Para qué |
|---|---|
| Campo opcional `notificacionUrl` en `POST /v1/pedidos` | Indicar dónde recibir los avisos de ese pedido. |
| Webhook `pedidoActualizado` | FAV avisa cuando el pedido pasa a `PAGADO`, `VENCIDO` o `A_VERIFICAR`. |
| `GET /v1/pedidos/{pedidoId}` | Confirmar el estado o ponerse al día si se perdió un aviso. |
| Estados `PAGADO`, `VENCIDO`, `A_VERIFICAR` y código `PEDIDO_INEXISTENTE` | Describir lo anterior. |

**Todo es opcional.** Si el pedido no trae `notificacionUrl`, no se envían avisos y la capacidad funciona igual que en la 1.0.0.

---

## 1. Operaciones

| Operación | Ruta | Qué hace |
|---|---|---|
| Ver el catálogo | `GET /v1/productos` | Devuelve todos los productos publicados con sus variantes y el precio vigente. |
| Hacer un pedido | `POST /v1/pedidos` | Aparta el stock y registra la venta, pendiente de pago por transferencia. Todo o nada. |
| Consultar un pedido *(1.1.0)* | `GET /v1/pedidos/{pedidoId}` | Devuelve el pedido con su estado vigente. |
| Aviso de cambio de estado *(1.1.0)* | `POST` a la `notificacionUrl` del grupo | **Lo envía FAV.** Ver §2. |

### Ver el catálogo

- Devuelve solo productos **publicados**, dentro de `items`.
- Por producto: `productoId`, `nombre`, `categoria` y `variantes`. `descripcion` y `fotoUrl` son opcionales: pueden no venir.
- Por variante: `varianteId` (el que se usa en el pedido), `talle`, `color` y `precio` (precio vigente por unidad, en pesos, con la promoción aplicada si hay una).
- **No informa stock.** Si una variante no alcanza, el pedido responde `409 STOCK_INSUFICIENTE`.
- Devuelve el catálogo completo en una sola respuesta (del orden de cientos de productos). Si el consumidor quiere mostrarlo por páginas, lo hace de su lado.

### Hacer un pedido

- Se envían `items` (hasta 20 variantes **[propuesta]**, sin repetir) con `varianteId` y `cantidad` (de 1 a 10 **[propuesta]**), y opcionalmente:
  - `referenciaExterna`: el número de pedido en su sistema. Vuelve en la respuesta y en los avisos.
  - `notificacionUrl` *(1.1.0)*: dónde recibir los avisos. Tiene que ser alcanzable desde FAV (en el entorno desplegado, una URL pública con `https` **[propuesta]**).
- **Todo o nada:** si una variante no alcanza, no se aparta ninguna.
- **El precio lo calcula FAV** al momento del pedido y se devuelve en la respuesta.
- El pedido queda `PENDIENTE_DE_PAGO`. La respuesta incluye el total, los datos para transferir y `pago.venceEn`: el stock queda apartado **48 horas**.

### Estados del pedido

```
POST /v1/pedidos ──► PENDIENTE_DE_PAGO ──(FAV verifica la transferencia)──► PAGADO: el stock se descuenta
                            │
                            └──(48 horas sin pago)──► VENCIDO: el stock se libera
                                                         │
                                                         └──(la transferencia llega tarde)──► A_VERIFICAR
```

| Estado | Qué significa | Qué hace el grupo |
|---|---|---|
| `PENDIENTE_DE_PAGO` | El stock está apartado hasta `pago.venceEn`. | Transferir el total a `pago.datosTransferencia`. |
| `PAGADO` | FAV verificó la transferencia y descontó el stock. | Coordinar la entrega por el contacto (§8). |
| `VENCIDO` | Pasaron 48 horas sin pago verificado. El stock se liberó. | Si todavía quiere la ropa, hacer un pedido nuevo (con otra `Idempotency-Key`). |
| `A_VERIFICAR` | La transferencia llegó cuando el pedido ya había vencido. FAV lo revisa a mano. | Esperar el contacto de FAV (reintegro o nueva reserva). |

**Un estado que no se conoce** (pueden agregarse en versiones menores) se trata como "en revisión": no tomar ninguna acción automática y consultar por el contacto.

**[?]** Los datos bancarios reales los define el cliente. Los del ejemplo son ficticios.

---

## 2. Avisos por webhook *(1.1.0)*

Si el pedido trae `notificacionUrl`, FAV envía un `POST` a esa URL **cada vez que el pedido cambia de estado**.

```http
POST /webhooks/fav HTTP/1.1
Content-Type: application/json
X-FAV-Firma: t=1792243201,v1=2fbec75293b36b2159e73e8c8c1a36062411d20762993a8755dd47a211182ab4
X-FAV-Evento-Id: 3b7e9a2c-5d1f-4c8e-a9b0-6f2d1e4c7a8b
X-Correlation-Id: 0c1e6a52-…

{"eventoId":"3b7e9a2c-5d1f-4c8e-a9b0-6f2d1e4c7a8b","tipo":"pedido.actualizado","ocurridoEn":"2026-10-17T13:20:00Z","pedido":{"pedidoId":"7d9f2c1e-4b3a-4e8f-9c2d-1a0b3c4d5e6f","referenciaExterna":"OC-2026-0457","estado":"PAGADO"}}
```

### Qué tiene que hacer el receptor

1. **Verificar la firma** antes de confiar en el cuerpo (abajo). Si no es válida o tiene más de 5 minutos: responder `401` y descartar.
2. **Deduplicar por `eventoId`** (también viene en `X-FAV-Evento-Id`): el mismo aviso puede llegar más de una vez. Si ya se procesó, responder `200` sin repetir el efecto.
3. **Responder `2xx` en menos de 5 segundos** y hacer el trabajo pesado después.
4. **Ante cualquier duda, consultar** `GET /v1/pedidos/{pedidoId}`: el estado vigente es el de la consulta, no el del aviso. El aviso informa que algo cambió; no reemplaza a la fuente.
5. **Ignorar los campos que no conozca.**

### Cómo verificar la firma

`X-FAV-Firma` tiene la forma `t=<timestamp>,v1=<firma>`:

- `t`: segundos desde 1970 (Unix) en que FAV firmó el aviso.
- `v1`: HMAC-SHA256, en hexadecimal, de `<t>.<cuerpo>`, con el **secreto de avisos** del grupo como clave. `<cuerpo>` es el cuerpo **crudo, tal como llegó**: si se parsea el JSON y se vuelve a serializar, la firma no coincide.

El secreto de avisos lo entrega FAV junto con la clave del grupo **[?]**. En el mock es `secreto-de-prueba`.

Ejemplo en Go:

```go
// verificarFirma devuelve true si el aviso lo firmó FAV en los últimos 5 minutos.
func verificarFirma(secreto string, cuerpo []byte, header string, ahora time.Time) bool {
	var t int64
	var v1 string
	for _, parte := range strings.Split(header, ",") {
		clave, valor, _ := strings.Cut(parte, "=")
		switch clave {
		case "t":
			t, _ = strconv.ParseInt(valor, 10, 64)
		case "v1":
			v1 = valor
		}
	}
	if t == 0 || math.Abs(float64(ahora.Unix()-t)) > 300 {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secreto))
	fmt.Fprintf(mac, "%d.", t)
	mac.Write(cuerpo)
	recibida, err := hex.DecodeString(v1)
	return err == nil && hmac.Equal(mac.Sum(nil), recibida)
}
```

**Vector de prueba:** el cuerpo y la firma del ejemplo de arriba son reales. Con el secreto `secreto-de-prueba`, la firma tiene que dar válida si se toma como "ahora" el instante `t=1792243201`. En JavaScript, la misma lógica está en [`mocks/catalogo-pedidos-v1/firma.mjs`](../../mocks/catalogo-pedidos-v1/firma.mjs).

### Entrega y reintentos

- **Al menos una vez.** El mismo aviso (mismo `eventoId`) puede llegar repetido.
- Si FAV no recibe un `2xx` en 5 segundos, reintenta a los **1, 5, 15 y 60 minutos**. Si el receptor responde `401`, no reintenta.
- Agotados los intentos, el aviso se descarta. El estado sigue disponible con `GET /v1/pedidos/{pedidoId}`.
- **Orden:** los estados solo avanzan (ver §1). Si llega un aviso con un estado anterior al que ya se tiene, se puede ignorar.

---

## 3. Autenticación, idempotencia y límites

**Autenticación.** Cada grupo recibe una clave propia y la envía en el header `X-API-Key`. Sin clave o con una clave inválida: `401 NO_AUTENTICADO`. **[?]** Cómo se entrega y se rota la clave. Los avisos no llevan `X-API-Key`: se autentican con la firma (§2).

**Idempotencia.** `POST /v1/pedidos` exige el header `Idempotency-Key`: una clave única por pedido (se recomienda un UUID).
- Ante un timeout, un `500` o un `503`, reintentar **con la misma clave y el mismo cuerpo**: se recibe la misma respuesta, sin crear otro pedido.
- La misma clave con otro cuerpo devuelve `409 CLAVE_IDEMPOTENCIA_REUTILIZADA`. `notificacionUrl` es parte del cuerpo.
- La clave vale 48 horas **[propuesta]**.

**Límite de uso.** 60 pedidos por minuto por grupo, sumando todas las operaciones **[propuesta]**. El exceso recibe `429` con el header `Retry-After`.

**Correlación.** Se puede enviar `X-Correlation-Id` para seguir una operación en los logs de los dos sistemas. FAV lo devuelve siempre (si no se envía, genera uno) y lo repite en los avisos de ese pedido.

---

## 4. Errores

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
| 400 | `PEDIDO_INVALIDO` | Pedido | Falta un header obligatorio o el cuerpo no cumple el contrato (incluida una `notificacionUrl` inválida). `errores` indica los campos. | No. Corregir. |
| 400 | `VARIANTE_INEXISTENTE` | Pedido | Una variante no existe o su producto no está publicado. `variantes` indica cuáles. | No. Corregir. |
| 401 | `NO_AUTENTICADO` | Todas | Falta la clave o no es válida. | No. |
| 404 | `PEDIDO_INEXISTENTE` | Consulta *(1.1.0)* | El pedido no existe o es de otro grupo. | No. |
| 409 | `STOCK_INSUFICIENTE` | Pedido | Al menos una variante no alcanza. No se apartó nada. `faltantes` indica cuáles. | No con las mismas cantidades. |
| 409 | `CLAVE_IDEMPOTENCIA_REUTILIZADA` | Pedido | La clave ya se usó con otro cuerpo. | No. Usar otra clave. |
| 410 | `VERSION_RETIRADA` | Todas | La versión de la ruta pasó su fecha de baja. `detail` indica la ruta nueva (§6). | No. Migrar a la versión nueva. |
| 429 | `LIMITE_EXCEDIDO` | Todas | Demasiados pedidos del grupo. | Sí, después de `Retry-After`. |
| 500 | `ERROR_INTERNO` | Todas | Error inesperado de FAV. | Sí, con la misma `Idempotency-Key`. |
| 503 | `NO_DISPONIBLE` | Todas | FAV o una dependencia no está disponible. | Sí, con la misma `Idempotency-Key`. |

**Recomendaciones para el consumidor [propuesta]:**
- Timeout de 3 segundos por llamada.
- Reintentar solo ante timeout, `429`, `500` o `503`, hasta 3 veces con espera creciente.
- Ignorar los campos de respuesta que no conozca (ver §6).

---

## 5. Ejemplos

```bash
# 1. Ver el catálogo
curl http://localhost:8090/v1/productos \
  -H "X-API-Key: clave-de-prueba"
# → 200 { "items": [ { "productoId": "prod-remera-basica", "nombre": "Remera básica",
#          "variantes": [ { "varianteId": "var-remera-basica-blanca-m", "talle": "M",
#                           "color": "Blanco", "precio": 15000 }, … ] }, … ] }

# 2. Hacer un pedido con avisos
curl -X POST http://localhost:8090/v1/pedidos \
  -H "X-API-Key: clave-de-prueba" -H "Content-Type: application/json" \
  -H "Idempotency-Key: 6f1c2b8e-3d4a-4e5f-9a7b-1c2d3e4f5a6b" \
  -d '{ "referenciaExterna": "OC-2026-0457",
        "notificacionUrl": "http://host.docker.internal:9000/webhooks/fav",
        "items": [ { "varianteId": "var-remera-basica-blanca-m", "cantidad": 3 },
                   { "varianteId": "var-jean-mom-celeste-38", "cantidad": 1 } ] }'
# → 201 { "pedidoId": "7d9f2c1e-…", "estado": "PENDIENTE_DE_PAGO", "total": 87000, "moneda": "ARS",
#         "pago": { "medio": "TRANSFERENCIA", "venceEn": "2026-10-18T15:00:00Z",
#                   "datosTransferencia": { "alias": "FAV.EJEMPLO.ALIAS", "titular": "FAV (ejemplo)" } }, … }
# → a los 2 segundos, el mock envía el aviso PAGADO firmado a la notificacionUrl

# 3. Consultar el pedido
curl http://localhost:8090/v1/pedidos/7d9f2c1e-4b3a-4e8f-9c2d-1a0b3c4d5e6f \
  -H "X-API-Key: clave-de-prueba"
# → 200 { "pedidoId": "7d9f2c1e-…", "estado": "PAGADO", … }

# 4. Ver un error en el mock (stock insuficiente)
curl -X POST http://localhost:8090/v1/pedidos \
  -H "Prefer: code=409" \
  -H "X-API-Key: clave-de-prueba" -H "Content-Type: application/json" \
  -H "Idempotency-Key: 6f1c2b8e-3d4a-4e5f-9a7b-1c2d3e4f5a6b" \
  -d '{ "items": [ { "varianteId": "var-remera-basica-blanca-m", "cantidad": 3 } ] }'
# → 409 { "codigo": "STOCK_INSUFICIENTE", "faltantes": [ … ] }
```

Para probar el receptor de avisos (duplicados, firma inválida, aviso vencido, reintentos), ver el [README del mock](../../mocks/catalogo-pedidos-v1/README.md#probar-el-webhook).

---

## 6. Versionado y compatibilidad

La versión mayor va en la ruta (`/v1/...`) y la completa en `info.version` del contrato (versionado semántico).

### Si cambia un endpoint: nueva versión en la ruta, con fecha de baja de la anterior

Cuando FAV cambia un endpoint o el contrato de una forma que puede romper a un consumidor, **no modifica la versión existente**: publica la nueva en otra ruta y las dos conviven hasta una fecha anunciada.

```
GET /v1/productos   ← sigue igual, habilitada hasta la fecha de baja anunciada
GET /v2/productos   ← versión nueva, con su propio contrato catalogo-pedidos-v2.openapi.yaml y su mock
```

1. **Se publica la `v2`** en su propia ruta, con su contrato, su mock y la lista de cambios respecto de la `v1`.
2. **Se anuncia la fecha de baja de la `v1`** (deadline) en la tabla de abajo, en el historial (§9) y por el contacto (§8). Hasta esa fecha la `v1` funciona igual que siempre.
3. **Desde el anuncio, las respuestas de la `v1` avisan la baja** con los headers `Deprecation: true` y `Sunset: <fecha>` (RFC 8594) **[propuesta]**, para que el consumidor lo vea también en sus logs.
4. **Pasada la fecha**, la `v1` responde `410 Gone` con el código `VERSION_RETIRADA`, que indica la ruta nueva.

**Plazo mínimo [propuesta]:** la fecha de baja se anuncia con al menos **1 semana** de anticipación y nunca es anterior al fin de la evaluación de la materia, porque la capacidad tiene que seguir operativa hasta entonces.

**Qué cambios llevan una versión nueva en la ruta:** quitar o renombrar campos u operaciones, volver obligatorio un campo opcional, cambiar el tipo o el significado de un campo, de un estado o de un código, y cambiar la forma de autenticar o de firmar los avisos.

**Qué cambios no la llevan:** agregar operaciones, campos opcionales en los pedidos, campos en las respuestas y en los avisos, o valores nuevos de `codigo` y de estado. Suben la versión menor (por ejemplo, de 1.0.0 a 1.1.0) en la misma ruta y no tienen fecha de baja, porque no rompen a nadie. **Para eso, el consumidor debe ignorar los campos que no conoce y tratar un estado desconocido como "en revisión".**

### Versiones publicadas

| Versión mayor | Ruta | Contrato | Versión vigente | Estado | Habilitada hasta |
|---|---|---|---|---|---|
| v1 | `/v1/...` | [`catalogo-pedidos-v1.openapi.yaml`](catalogo-pedidos-v1.openapi.yaml) | 1.1.0 | Vigente | Sin fecha de baja |

Posibles agregados compatibles a futuro, sin cambiar de ruta: disponibilidad en el catálogo y paginación opcional (sin parámetros seguiría devolviendo el catálogo completo).

## 7. Entornos

| Entorno | URL | Estado |
|---|---|---|
| Mock local | `http://localhost:8090` | Disponible. Ver [`mocks/catalogo-pedidos-v1/`](../../mocks/catalogo-pedidos-v1). |
| Desplegado (nube) | **[?]** | Antes de la presentación grupal. |

## 8. Contacto

Por mail a **2212370@ucc.edu.ar** (o whatsapp) para:

- reportar problemas o pedir cambios en el contrato;
- pedir la clave del grupo y el secreto de avisos, e informar la URL donde reciben los avisos en el entorno desplegado;
- coordinar la entrega de un pedido pagado o resolver un pedido `A_VERIFICAR`.

Indicar el nombre del grupo y, si es sobre un pedido, el `pedidoId` y el `X-Correlation-Id`.

## 9. Historial de versiones

| Versión | Fecha | Cambios |
|---|---|---|
| 1.1.0 | 15/10/2026 | Avisos por webhook (`notificacionUrl` opcional y callback `pedidoActualizado`, firmado con HMAC), consulta `GET /v1/pedidos/{pedidoId}`, estados `PAGADO`, `VENCIDO` y `A_VERIFICAR`, y códigos `PEDIDO_INEXISTENTE` y `VERSION_RETIRADA`. Política de versionado: un cambio incompatible va a una ruta nueva y la anterior tiene fecha de baja (§6). Compatible con la 1.0.0. |
| 1.0.0 | 14/10/2026 | Primera versión: catálogo y pedidos. Reemplaza las propuestas de la Entrega 1 (reserva de cupos y envío de emails), descartadas al asignarse el grupo consumidor (ADR-008). |
