# ADR-018 · Consumo del gimnasio con su contrato real

| | |
|---|---|
| **Estado** | Aceptada · Reemplaza a [ADR-009](ADR-009-consumo-del-gimnasio.md) |
| **Fecha** | 08/11/2026 |
| **Decisión de la consigna** | D9 · Consumo del proveedor |
| **Relacionadas** | [ADR-004](ADR-004-consistencia-y-concurrencia.md) (reposiciones), [ADR-008](ADR-008-contrato-propio.md) (el gimnasio también nos consume), [ADR-010](ADR-010-resiliencia.md) (resiliencia) |
| **Contrato** | [`gimnasio-consignaciones-1.0.0.openapi.yaml`](../contracts/externos/gimnasio/gimnasio-consignaciones-1.0.0.openapi.yaml) y [guía](../contracts/externos/gimnasio/gimnasio-guia-fav.md) |

> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar.

## Contexto

El ADR-009 se basó en un contrato supuesto. El gimnasio publicó el real. Las rutas y los campos coinciden, pero cambian tres cosas:

- **No hay límite de 20 unidades.** Era una regla inventada por nuestro mock.
- **Un `4xx` no siempre es un rechazo.** Antes, una clave mal cargada devolvía el stock de cada reposición, aunque la mercadería pudiera estar en camino. El contrato aclara que `401`, `403` y `409` no autorizan a compensar.
- **El éxito es solo `201` con su cuerpo.** Antes se aceptaba cualquier respuesta menor a `400`.

Se mantiene del ADR-009: la capa anticorrupción, el descuento y la reposición pendiente en una misma transacción, el envío en segundo plano y el circuit breaker.

## Criterios

1. **Que ninguna reposición se pierda ni se duplique.**
2. **Devolver stock solo con certeza** de que el gimnasio no registró la entrega. Reintentar con la misma clave no duplica; devolver unidades que ya llegaron deja un stock falso.
3. **Que un cambio incompatible del contrato se detecte en un test.**
4. **Que el administrador sepa qué pasa con cada entrega.**

## Decisión

### 1. Qué se hace ante cada respuesta

| Respuesta | Estado | ¿Devuelve stock? | Qué sigue |
|---|---|---|---|
| `201` con `reposicionId` (UUID) y `estado: RECIBIDA` | `CONFIRMADA` | No | Se guarda el id del gimnasio |
| `400` `SOLICITUD_INVALIDA` o `IDEMPOTENCIA_INVALIDA`, `413`, `415`, `422`, con el `codigo` del contrato | `RECHAZADA` | **Sí, una sola vez** | Fin |
| `401`, `403` | `BLOQUEADA_CONFIGURACION` | No | Se reintenta cada 5 min y queda en el log como error. Al corregir la clave se destraba sola |
| `409` | `REQUIERE_REVISION` | No | No se reintenta. Se revisa con el gimnasio |
| `404`, `408`, `429`, `5xx`, timeout, red, circuito abierto | `PENDIENTE_CONFIRMACION` | No | Se reintenta con la misma clave, referencia y cuerpo |
| `2xx` o `3xx` fuera de contrato, o `4xx` sin el `codigo` esperado | `PENDIENTE_CONFIRMACION` | No | Igual que el anterior. Si no es un error, además registra `CONTRATO_INCOMPATIBLE` en el log |

**Se decide por `codigo` y no por el estado solo:** un `400` sin código puede venir de un proxy, y no prueba que el gimnasio lo haya rechazado.

### 2. Traducción

`varianteId` → `productoCodigo`; id de la reposición → `referencia`; clave de la reposición → `Idempotency-Key`; `proveedorId` = `fav`; credencial en `X-API-Key`. FAV nunca envía `reposicionId`: lo genera el gimnasio.

### 3. Protecciones

| Mecanismo | Valor |
|---|---|
| Timeout por intento | 3 s (`GIMNASIO_TIMEOUT_MS`). Sube de 2 s porque ahora la llamada sale por internet |
| Circuit breaker | Abre tras 5 fallas seguidas, 30 s abierto, una llamada de prueba. Cuentan red, timeout, `404`, `408`, `429` y `5xx`. **No** cuentan `401`, `403` ni `409`: el gimnasio está atendiendo |
| Reintentos | Persistidos en la base. De 5 s a 5 min, con hasta 20 % de variación para que no reintenten todas juntas. Sin tope de intentos: la reposición ya descontó stock |
| Redirecciones | No se siguen |

### 4. Qué ve el administrador

Pantalla **Gimnasio** del panel: el stock allá, el formulario de reposición y el estado de cada entrega. Si no se puede consultar el stock, muestra "Stock del gimnasio no disponible en este momento". **Un error nunca se muestra como cero ni como una lista vacía:** `items: []` significa que no se recibió nada; una respuesta sin `items` o con campos faltantes es indisponibilidad.

### 5. Test de contrato

`adaptadores/gimnasio/contrato_test.go` carga el YAML con `kin-openapi`, valida contra él los pedidos del cliente y le pasa al cliente los ejemplos de cada respuesta. Se comprobó alterando el YAML: renombrar `reposicionId`, cambiar un `codigo` o mover la ruta hace fallar el test.

## Alternativas consideradas

| Alternativa | Por qué se descartó |
|---|---|
| **Reservar el rechazo para `409` y `422`,** como proponía el ADR-009 | El contrato dice lo contrario sobre el `409`: puede existir una entrega anterior, y no autoriza a compensar. |
| **Probar solo contra el gimnasio real** | Su URL todavía no está publicada, y no controlamos su sistema. El mock con WireMock responde según la clave (`probar-conflicto-`, `probar-rechazo-`), lo que permite provocar cada respuesta del contrato. No reemplaza la prueba conjunta. |
| **Reintentar el `409` con otra clave** | Es lo que el contrato pide no hacer: podría duplicar la entrega. |
| **Dejar de reintentar la `BLOQUEADA` hasta una acción manual** | Exige una pantalla para destrabarla y no aporta: reintentar con la clave mala no causa daño, y al corregirla se resuelve sola. |
| **Tope de intentos y descartar** | La reposición ya descontó stock: descartarla perdería la mercadería en los registros. |
| **Rechazar respuestas con campos desconocidos** | Una ampliación del gimnasio frenaría las entregas. Lo estricto queda en el test de contrato. |
| **Sugerir reposiciones por umbral de stock** | El umbral sería una regla de negocio inventada: no está definida por el cliente. |

## Consecuencias

**Positivas**

- Una clave mal cargada ya no genera stock falso.
- La caída del gimnasio solo afecta a sus datos y a las reposiciones.
- Un cambio incompatible del contrato se detecta en un test.

**Negativas aceptadas**

- **Una `REQUIERE_REVISION` no se resuelve desde el sistema.** Las unidades quedan fuera del stock hasta resolverlo por fuera. **[?]** Cómo se corrige: no hay endpoint para eso.
- **Una `BLOQUEADA_CONFIGURACION` tarda hasta 5 min en destrabarse** después de corregir la clave.
- **Un `4xx` inesperado se reintenta sin límite.** Queda visible en el panel como pendiente, con su último error.
- **Mientras una reposición no se confirma, sus unidades no figuran en ningún stock.**
- **Las ventas del gimnasio y la comisión siguen fuera del sistema** (SPEC §11).

## Pendientes

Las alertas `ReposicionBloqueada`, `ReposicionARevisar` y `ReposicionPendienteAntigua` ya existen (ADR-011).

**Con el grupo del gimnasio:**
- **URL pública y `X-API-Key`** por canal privado. Hoy su contrato declara solo `localhost`.
- **Aceptación del contrato y prueba conjunta:** alta, consulta, reintentos con la misma clave y con otra, `409`, caída y recuperación.

**Del equipo:**
- **Cómo se revisa una `REQUIERE_REVISION`.**

## Validación

Probado el 08/11/2026 con el mock local y un gimnasio falso por HTTP. **Todavía no se probó contra el sistema real del gimnasio.**

- **Clasificación e idempotencia:** `adaptadores/gimnasio/cliente_test.go` (cada respuesta, misma clave en los reintentos, circuito).
- **Contra MySQL real:** `adaptadores/mysql/reposiciones_test.go`. El stock físico en cada caso, la devolución una sola vez y la migración repetida. Requiere `INVENTARIO_TEST_DSN`.
- **Migración sobre una base existente:** aplicada al volumen local con `estado` en `ENUM`.
- **Por el gateway:** una reposición de 25 unidades quedó `CONFIRMADA`; `probar-rechazo-` devolvió las unidades; `probar-conflicto-` quedó `REQUIERE_REVISION` sin devolverlas; con `GIMNASIO_API_KEY=clave-invalida` quedó `BLOQUEADA_CONFIGURACION` y el panel recibió `503`.
- **Caída provocada:** `services/inventario/pruebas/caida_gimnasio.sh`. Con el mock apagado, la reposición queda pendiente sin devolver unidades, y se confirma sola 7 s después de que vuelve, descontada una sola vez. El ensayo de caídas, incluida la del gimnasio, está en [`POSTMORTEM.md`](../POSTMORTEM.md).
