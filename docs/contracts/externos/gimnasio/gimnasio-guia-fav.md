# Integración con FAV: stock y reposiciones del gimnasio

> Guía escrita por el grupo del gimnasio, no por FAV. Lo que dice sobre el cliente de FAV (por ejemplo, que trata todo `4xx` como rechazo) quedó superado por el [ADR-018](../../../adr/ADR-018-consumo-del-gimnasio-contrato-real.md). Sus referencias a `ADR-022` y a `EVIDENCIA-TIENDA` apuntan a documentos de su repositorio, que no están acá.

**Contrato:** [OpenAPI 1.0.0](gimnasio-consignaciones-1.0.0.openapi.yaml). **Responsable:** microservicio Tienda del gimnasio.

**Estado:** endpoints implementados y verificados localmente con PostgreSQL real. URL pública, aceptación de FAV y prueba conjunta pendientes. Los ejemplos no acreditan una conexión con el grupo. La consulta del catálogo externo `GET /api/v1/tienda/productos` sigue respondiendo `503 INTEGRACION_PENDIENTE`; el stock local funciona independientemente.

**Enunciado:** §3, página 4 (documentación); §4.1, páginas 5–6 (contrato formal, integración y pruebas); §5 D8/D9, página 7 (capacidad ofrecida y consumo del proveedor). Decisión: [ADR-022](../adr/ADR-022-tienda-y-consignaciones-fav.md).

## Responsabilidades y reglas

### Revisión de la rama externa del 07/11/2026

Se volvió a revisar `development` en `adb9838023ddd5d4340f8bc7a95b38fdbc695845`, sin cambios desde la inspección anterior. Su contrato 1.0.0 publica `GET /v1/productos` con variantes y precios, sin stock. El código también implementa disponibilidad por variante en `GET /inventario/v1/disponibilidad?variantes=...`, pero esa operación todavía debe incorporarse al contrato compartido y confirmarse con FAV. El cliente de reposiciones conserva las rutas y campos compatibles con esta guía y sigue tratando todos los 4xx salvo 429 como rechazos definitivos. La coordinación de errores indicada abajo sigue pendiente. [Contrato de FAV](https://github.com/JuanHernandez-99/Fav-del-universo/blob/adb9838023ddd5d4340f8bc7a95b38fdbc695845/docs/contracts/catalogo-pedidos-v1.openapi.yaml), [cliente de FAV](https://github.com/JuanHernandez-99/Fav-del-universo/blob/adb9838023ddd5d4340f8bc7a95b38fdbc695845/services/inventario/internal/adaptadores/gimnasio/cliente.go).

Antes de conectar el consumo faltan su confirmación de rama/versión, URL accesible y autenticación por canal privado. Las direcciones `localhost` de sus ejemplos corresponden al equipo que ejecuta FAV. La prueba conjunta verificará catálogo, disponibilidad y recepción de entregas con datos ficticios.

FAV conserva su catálogo, variantes y existencias de la distribuidora. Tienda administra el stock físico recibido en el gimnasio: **una única tienda, sin separación por sede**, en su base PostgreSQL `tienda` con usuario propio. Los servicios se comunican por API; ninguno consulta bases de datos ajenas.

- `productoCodigo` identifica exactamente una **variante de FAV** (`varianteId`), incluyendo talle y color; no es el identificador del producto general.
- FAV consulta las existencias del gimnasio y registra cada reposición con una referencia propia de entrega.
- El administrador del gimnasio registrará las unidades vendidas mediante una función interna. Esta guía no publica una operación de ventas para FAV.
- `unidadesDisponibles` es el total recibido menos el total vendido; nunca puede ser negativo.
- `unidadesVendidas` es el acumulado histórico de ventas de esa variante, sin reiniciarse por reposición.
- Devoluciones y ajustes quedan fuera de esta versión. No existe un límite comercial de 20 unidades por reposición.

## Acceso y operaciones

FAV envía una credencial de aplicación en `X-API-Key`, configurada en el gimnasio como `FAV_API_KEY`. El receptor comprueba esa credencial y `proveedorId=fav`. **El JWT de los usuarios del gimnasio no autoriza estas operaciones.** La clave se intercambia por canal privado y no se publica en el repositorio ni en el navegador del gimnasio.

Las respuestas llevan `Cache-Control: no-store`. La URL base debe coordinarse: `{{gimnasioBaseUrl}}` es una variable de Postman, no una dirección desplegada.

Para probar en el equipo que inicia el gimnasio con `npm run iniciar`, usar `http://localhost:8080` como variable base (o el `GATEWAY_PORT` configurado). El inicio incorpora `TIENDA_DB_PASSWORD` y `FAV_API_KEY` ausentes sin cambiar claves existentes, y crea base/usuario faltantes sin borrar volúmenes. `localhost` no es una dirección compartida entre equipos. Esta API no necesita que el catálogo de FAV esté disponible para recibir sus entregas.

| Método y ruta | Resultado |
|---|---|
| `GET /api/v1/consignaciones/stock?proveedorId=fav` | `200`: stock disponible y vendido de las variantes recibidas. |
| `POST /api/v1/consignaciones/reposiciones` | `201`: reposición registrada o repetición del resultado original. |

### Consultar el stock

En Postman: método **GET**, URL `{{gimnasioBaseUrl}}/api/v1/consignaciones/stock?proveedorId=fav`, encabezado `X-API-Key: {{favApiKey}}`, sin cuerpo.

```json
{
  "proveedorId": "fav",
  "items": [
    { "productoCodigo": "var-remera-basica-blanca-m", "unidadesDisponibles": 3, "unidadesVendidas": 2 }
  ]
}
```

La respuesta incluye variantes recibidas aunque su disponibilidad sea cero. `items: []` significa que no se registró ninguna recepción; un fallo del servicio se comunica como error, sin fabricar una lista vacía.

### Registrar una reposición

En Postman: método **POST**, URL `{{gimnasioBaseUrl}}/api/v1/consignaciones/reposiciones`. Encabezados: `Content-Type: application/json`, `X-API-Key: {{favApiKey}}` e `Idempotency-Key: {{reposicionKey}}`. Cuerpo **raw → JSON**:

```json
{
  "proveedorId": "fav",
  "productoCodigo": "var-remera-basica-blanca-m",
  "unidades": 5,
  "referencia": "entrega-fav-0001"
}
```

`unidades` debe ser un entero positivo representable de forma segura en JSON, de 1 a `9007199254740991`. `referencia` identifica la entrega en FAV y es independiente de `Idempotency-Key`.

Respuesta `201` tanto al registrar como al repetir la misma entrega:

```json
{ "reposicionId": "41a759ec-67b5-45f0-9c16-16318663bd17", "estado": "RECIBIDA" }
```

Los valores de los ejemplos son ficticios. El receptor genera `reposicionId`; FAV no debe generarlo ni enviarlo.

## Idempotencia y recuperación

- Misma clave y mismos datos: se devuelve el mismo resultado, sin volver a sumar unidades.
- Otra clave con la misma referencia y datos: se reconoce la entrega existente y se devuelve el mismo ID y resultado.
- Reutilizar una clave o referencia con otros datos devuelve `409`; no modifica el stock.
- Registrar la reposición, sumar el stock y guardar el resultado repetible es una única transacción. La comprobación de claves/referencias está protegida frente a concurrencia y los resultados sobreviven al reinicio.

Ante pérdida de respuesta, timeout, fallo de red o `500`/`503`, conservar **la misma clave, referencia y cuerpo**. No crear otra entrega para resolver una respuesta incierta.

## Errores

Los errores usan JSON con `codigo` estable y `mensaje` explicativo. Las decisiones del cliente deben basarse en `codigo`, sin comparar el texto del mensaje.

| HTTP | `codigo` | Tratamiento |
|---|---|---|
| 400 | `SOLICITUD_INVALIDA` / `IDEMPOTENCIA_INVALIDA` | Corregir parámetros, cuerpo o clave conforme al OpenAPI. |
| 401 | `NO_AUTENTICADO` | Revisar la credencial; no asumir rechazo de mercadería. |
| 403 | `PROVEEDOR_NO_AUTORIZADO` | Revisar la identidad/permisos del proveedor. |
| 409 | `CLAVE_REUTILIZADA` / `REFERENCIA_REUTILIZADA` | Revisar el conflicto; no generar otra clave o referencia automáticamente. |
| 413 | `SOLICITUD_DEMASIADO_GRANDE` | El JSON excede 1 MiB. Es un límite de transporte, sin relación con la cantidad de unidades. |
| 415 | `TIPO_CONTENIDO_NO_ADMITIDO` | Enviar JSON con su tipo de contenido. |
| 422 | `STOCK_NO_REPRESENTABLE` | La suma excedería el rango entero seguro; revisar la entrega. No es un límite de 20 unidades. |
| 500 | `ERROR_INTERNO` | Resultado potencialmente incierto: recuperar con los mismos datos. |
| 503 | `SERVICIO_NO_DISPONIBLE` | Reintentar conservando los mismos datos. |

**Cambio necesario en el cliente de FAV:** su adaptador revisado trata todos los `4xx`, salvo `429`, como rechazo definitivo y compensa el stock. Debe distinguir errores de autenticación/configuración, conflictos e incertidumbre; `401`, `403`, un `404` de ruta o `408` no prueban que la entrega haya sido rechazada. Un `409` requiere revisión y tampoco debe disparar una compensación automática.

El éxito del POST acordado es `201` con el cuerpo `ReposicionRecibida`; aceptar cualquier respuesta menor a `400`, como hace actualmente el cliente revisado, también aceptaría una redirección o una respuesta ajena al contrato. FAV debe comprobar el estado y esquema antes de dar una entrega por recibida.

## Coordinación pendiente

Confirmar con FAV la aceptación de este contrato, la rama que usarán, URL accesible y credenciales; probar juntos alta, consulta, repeticiones con ambas claves, conflictos y caída/recuperación. El backend ya se verifica con `npm run test:tienda`, contra HTTP y PostgreSQL reales; [EVIDENCIA-TIENDA](../EVIDENCIA-TIENDA.md) distingue esos ensayos de una conexión real con FAV. No se publican claves reales en estos ejemplos.

## Operaciones internas del administrador

Estas rutas no se ofrecen a FAV. Exigen `Authorization: Bearer <JWT>` con rol ADMIN; la clave de FAV no sirve para usarlas. Como en Accesos, la autorización local reconoce el JWT hasta su vencimiento, sin revocación inmediata.

| Operación | Datos y respuesta |
|---|---|
| `GET /api/v1/tienda/stock` | Sin parámetros ni cuerpo; `200` con el mismo modelo de stock local descrito arriba. |
| `POST /api/v1/tienda/ventas` | JSON `{"productoCodigo":"var-remera-basica-blanca-m","unidades":2}` e `Idempotency-Key`. `201` con `{"ventaId":"<UUID>","estado":"REGISTRADA","productoCodigo":"var-remera-basica-blanca-m","unidades":2}`. |

La venta descuenta disponibles y suma vendidos, conserva actor/fecha y resultado en una transacción. La clave se diferencia por administrador: repetirla con el mismo cuerpo devuelve el recibo original, aunque el stock haya cambiado después. Otro cuerpo devuelve `409 CLAVE_REUTILIZADA`. No se admiten campos adicionales, precios, proveedor, actor o estado elegidos por el navegador.

La variante sin recepción produce `404 PRODUCTO_NO_ENCONTRADO`; unidades mayores a las disponibles producen `409 STOCK_INSUFICIENTE`, sin efectos. Cantidades inválidas dan `400 SOLICITUD_INVALIDA`; credencial ausente, `401`; rol SOCIO, `403 ACCESO_DENEGADO`. Se aplican también los errores técnicos y de transporte anteriores. Los rechazos no guardan un movimiento exitoso.

En **Tienda y stock**, ADMIN selecciona variante/unidades y confirma la venta realizada. El portal guarda la operación pendiente por administrador antes de enviarla, sin JWT; una respuesta incierta se recupera con la misma clave y cuerpo, incluso tras recarga y nuevo login. No borrar ese registro para iniciar otra venta. Una respuesta de una vista anterior no puede borrar una venta nueva. El panel solo informa unidades: no procesa cobros, devoluciones ni ajustes.

Se revisaron [main `f8680fb`](https://github.com/JuanHernandez-99/Fav-del-universo/tree/f8680fb281ac6072471cc239b4b88c1ab82f6c13) y [development `adb9838`](https://github.com/JuanHernandez-99/Fav-del-universo/tree/adb9838023ddd5d4340f8bc7a95b38fdbc695845); la antigua rama `feature-inventario` ya no existe. Su [ADR de consumo](https://github.com/JuanHernandez-99/Fav-del-universo/blob/adb9838023ddd5d4340f8bc7a95b38fdbc695845/docs/adr/ADR-009-consumo-del-gimnasio.md) usa un contrato supuesto: los límites y ejemplos de ese mock no se convierten en reglas del gimnasio.

El sentido gimnasio → FAV sigue siendo una integración separada de consulta de catálogo y disponibilidad. El contrato publicado de FAV aún no incluye stock; falta acordar su operación externa antes de conectar el cliente de Tienda. Esta guía documenta exclusivamente el sentido FAV → gimnasio.
