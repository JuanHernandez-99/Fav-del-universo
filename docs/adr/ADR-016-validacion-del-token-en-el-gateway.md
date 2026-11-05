# ADR-016 · Validación del token en el gateway

| | |
|---|---|
| **Estado** | Aceptada · 04/11/2026 |
| **Fecha** | 04/11/2026 |
| **Decisión de la consigna** | Adicional |
| **Relacionadas** | [ADR-001](ADR-001-limites-de-los-servicios.md) (identidad), [ADR-015](ADR-015-claves-del-token-y-cuenta-de-administrador.md) (claves del token), [ADR-012](ADR-012-balanceo.md) (gateway) |

> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar.

## Contexto

La consigna pide un punto de entrada único con autenticación. ADR-001 decidió que el gateway valide el token **sin llamar a Usuarios**, para que comprar no dependa de ese servicio. El gateway es nginx gratuito, que no valida JWT por sí solo. Hasta ahora el panel usaba una clave provisoria, `X-Admin-Key`.

## Criterios

1. Cumplir ADR-001: sin llamar a Usuarios.
2. No sumar piezas que puedan fallar en cada pedido.
3. Cambiar lo menos posible a pocos días de la presentación.
4. Que un token falso no pase, aunque el atacante conozca la clave pública.

## Decisión

**El gateway valida el token con njs**, el módulo de JavaScript que ya trae la imagen de nginx. Con `auth_request`, nginx consulta un script (`gateway/njs/jwt.js`) antes de pasar el pedido.

**Qué se exige:** algoritmo RS256 (fijado en el script, no se toma del token), firma válida con la clave pública, `exp` futuro, `sub` y `email` presentes, y `role` conocido.

| Rutas | Sin token | Token inválido o vencido | Comprador | Administrador |
|---|---|---|---|---|
| Panel: `/inventario/`, `/catalogo/`, `/ventas/v1/admin/` | `401` | `401` | `403` | Pasa |
| Tienda: `/ventas/v1/compras…`, `/ventas/v1/mis-compras…` | Pasa como invitado; "Mis compras" da `401` | `401` | Pasa | Pasa |
| Capacidad publicada: `/v1/productos`, `/v1/pedidos` | Usa `X-API-Key` (ADR-008) | — | — | — |
| `/ventas/v1/pagos/aviso` | Lo valida Ventas con la firma | — | — | — |
| `/usuarios/v1/registro`, `/usuarios/v1/sesiones` | Públicas | — | — | — |

- **La identidad sale solo del token:** nginx pisa los `X-User-Id`, `X-User-Email` y `X-User-Role` que mande el cliente.
- **Un token vencido en la tienda da `401`,** no se trata como invitado: la compra quedaría sin asociar a la cuenta.
- **El token no llega a los servicios:** nginx borra `Authorization`, porque no lo necesitan.
- **`X-Admin-Key` deja de existir.**

## Alternativas consideradas

| Alternativa | Por qué se descartó |
|---|---|
| **Servicio verificador aparte** | Más fácil de testear en Go, pero suma un contenedor y un salto en cada pedido. Si cae, cae todo el panel y la compra con cuenta. |
| **OpenResty u otro gateway con JWT incluido** | Obliga a rehacer y volver a probar el gateway a días de la presentación. |
| **nginx Plus** | Trae validación de JWT, pero es pago. |
| **Que cada servicio valide el token** | Repite la lógica en cuatro servicios, y ADR-001 la ubica en el gateway. |
| **Llamar a Usuarios para validar** | Contradice ADR-001: con Usuarios caído no se podría comprar con sesión iniciada. |

## Consecuencias

**Positivas**

- Con Usuarios caído, las sesiones iniciadas siguen funcionando (comprobado).
- No hay contenedores nuevos ni llamadas de red para validar.
- Los servicios siguen recibiendo los mismos encabezados.
- El gateway lee la clave pública en cada validación, así que cambiar las claves no debería requerir reiniciarlo (sin probar).

**Negativas aceptadas**

- **Hay lógica de seguridad en JavaScript, fuera del código Go.** Se prueba desde afuera, con `gateway/pruebas/token.mjs`.
- **Se lee y se importa la clave pública en cada pedido autenticado.** En una corrida de validación, con unos 100 pedidos por segundo y el token en todos, el p95 fue de 21 ms. No se comparó contra no validar: se revisa con las pruebas de carga (ADR-013).
- **Los servicios confían en los encabezados `X-User-*`:** quien llegue a ellos sin pasar por el gateway podría falsificarlos. Hoy no publican puertos en Docker. Se revisa en el despliegue.
- **El token no se puede revocar** antes de su vencimiento (ADR-001).

## Pendientes

- **Dónde guarda el token el frontend:** se decide con el frontend.
- **Comparar el costo de validar** en las pruebas de carga (ADR-013).
- **Mantener los puertos de los servicios cerrados en la nube.** **[?]** Se resuelve con el despliegue.

## Validación

`gateway/pruebas/token.mjs`, contra el sistema levantado (04/11/2026): 42 de 42. Ver [`gateway/readme.md`](../../gateway/readme.md).

**Sin probar:** que `Authorization` no llegue a los servicios (lo indica la configuración, no lo comprueba un test) y un pico de carga real.

---

**Si la decisión cambia:** no se borra este archivo. Se marca como *Reemplazada por ADR-YYY* y el nuevo ADR indica a cuál reemplaza y por qué hubo que revisarla.
