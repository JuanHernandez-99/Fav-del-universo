# ADR-014 · Simulador de pagos, mails locales y orden de la compra

| | |
|---|---|
| **Estado** | Aceptada · 04/11/2026 |
| **Fecha** | 04/11/2026 |
| **Decisión de la consigna** | Adicional |
| **Relacionadas** | [ADR-004](ADR-004-consistencia-y-concurrencia.md) (fallas parciales), [ADR-005](ADR-005-comunicacion.md) (pendientes de Mercado Pago y mail), [ADR-010](ADR-010-resiliencia.md) (resiliencia) |

> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar.

## Contexto

Al implementar Ventas quedaban tres puntos abiertos.

- **Mercado Pago.** La consigna no pide pagos: es alcance propio (SPEC RN-18, HU-11, CU-01). El SPEC §3 habla de credenciales de prueba y ADR-005 dejó abierto cómo recibir sus avisos en local. La consigna (2.12) exige levantar el sistema con un procedimiento único, sin configuraciones manuales, y (2.9) demostrar qué pasa cuando una dependencia falla.
- **Mails.** La consigna tampoco los pide (salen de HU-20 y HU-21) y el proveedor estaba pendiente.
- **Orden de la compra.** Los documentos chocaban: CU-01 creaba la venta y después reservaba, mientras que ADR-005 §3 dice que si la reserva falla no se crea la venta y HU-11.9 pide que no quede una venta a medias.

## Criterios

1. Arrancar con un comando, en cualquier computadora y sin cuentas ni túneles.
2. Poder provocar los casos difíciles: pago rechazado, aviso duplicado y caída.
3. No duplicar ni dejar a medias una venta ante una falla parcial.
4. Poder pasar a un proveedor real tocando un solo lugar.

## Decisión

**1. Mercado Pago se reemplaza por un simulador local detrás de un puerto de Ventas.** `mocks/mercadopago-simulador` imita lo que Ventas usa: crear el pago, consultarlo y avisar el resultado con una firma HMAC. El puerto es `ProveedorPagos` y el adaptador, `adaptadores/mercadopago`. Esto reemplaza lo que dice el SPEC §3 sobre credenciales de prueba.

**2. Los mails salen por SMTP hacia Mailpit**, un servidor de desarrollo con bandeja web. Nada sale a internet. Para usar un proveedor real alcanza con cambiar `SMTP_DIRECCION`, siempre que acepte SMTP sin autenticación; si la pide, hay que sumarla en `adaptadores/mail`.

**3. Primero se reserva y después se guarda la venta.** Se corrige CU-01. El id de la venta se deriva de su clave de idempotencia, así un reintento usa el mismo id y la misma referencia en la reserva.

| Falla | Qué pasa |
|---|---|
| Inventario no responde al reservar | `503`. No queda ninguna venta. |
| Ventas se cae después de reservar y antes de guardar | La reserva vence sola. Un reintento con la misma clave la recupera y crea la venta. |
| Ese reintento llega con la reserva ya vencida | `409 CLAVE_IDEMPOTENCIA_REUTILIZADA`: hay que hacer un pedido nuevo. |
| Mercado Pago no responde al crear el pago | La venta queda creada y responde `503`. El reintento retoma solo el pago; si no llega, la reserva vence sola. |

**4. Tiempo total por pedido: 4,5 s** en Ventas, reintentos incluidos, por debajo de los 5 s del gateway. Así Ventas responde su propio error en lugar de un `504` (resuelve el problema señalado en ADR-005 y ADR-010). Si no alcanza el tiempo, no se agotan los reintentos del ADR-005.

## Alternativas consideradas

| Alternativa | Por qué se descartó |
|---|---|
| **Sandbox real de Mercado Pago** | Necesita credenciales y un túnel público, y no se pueden provocar sus caídas a voluntad. |
| **Sacar Mercado Pago de la v1** | También cumple la consigna, pero recorta el alcance acordado con el cliente. |
| **Proveedor de mail real** | Necesita una cuenta y credenciales; no aporta a lo que se evalúa. |
| **Solo registrar los mails** | Deja HU-20 y HU-21 a medias. |
| **Crear la venta y después reservar** | Deja rastro de cada intento, pero obliga a limpiar ventas sin reserva y contradice ADR-005 §3. |

## Consecuencias

**Positivas**

- El sistema arranca sin cuentas, y los casos difíciles se pueden demostrar en la jornada de fallas.
- Una falla de Inventario no deja ventas a medias.

**Negativas aceptadas**

- **El adaptador de Mercado Pago no se probó contra la API real.** Imita su forma, pero pasar a producción puede requerir ajustes. Se declara en las limitaciones (ARCHITECTURE §16).
- **Los pagos del simulador viven en memoria:** se pierden si se reinicia el contenedor.
- **No queda rastro de una compra que falló antes de guardarse**, salvo en los logs.

## Pendientes

- **[?]** Estado de una venta con el pago rechazado: el SPEC pide liberar la reserva (HU-11.4) pero no define el estado. **[propuesta]** *Cancelada*, que es lo que hace hoy.
- **[?]** Cómo cancela un comprador invitado: hoy alcanza con conocer el id de la venta, que no se puede adivinar. **[propuesta]**
- **[?]** Datos para transferir y cómo se resuelve una venta *A verificar*: siguen pendientes con el cliente (SPEC §12).

## Validación

Comprobado con el sistema levantado el 04/11/2026, por el gateway (detalle en [`services/ventas/README.md`](../../services/ventas/README.md)): aviso duplicado, pago rechazado, firma inválida, Mercado Pago caído con reintento, Inventario caído sin ventas a medias, y RabbitMQ caído con el evento entregado al volver.

La caída entre reservar y guardar solo está probada con un test unitario, no provocada en el sistema real.

---

**Si la decisión cambia:** no se borra este archivo. Se marca como *Reemplazada por ADR-YYY* y el nuevo ADR indica a cuál reemplaza y por qué hubo que revisarla.
