# Ventas

Ventas online y del local, sus estados, los pagos (Mercado Pago y transferencia) y el envío de mails.

> Estado: **previsto** (Entrega 2). Hoy solo está el diseño. **[propuesta]** marca lo no confirmado.

| | |
|---|---|
| **Base de datos** | MySQL ([ADR-003](../../docs/adr/ADR-003-persistencia.md)) |
| **Estilo interno** | Hexagonal **[propuesta]**: dominio, aplicación, puertos y adaptadores (http, mysql, mercadopago, mail, rabbitmq) |
| **Estados de la venta** | Ver [`SPEC.md` §7](../../SPEC.md) |

## Qué no hace

No modifica el stock: se lo pide a [Inventario](../inventario). No consulta a Usuarios: la identidad del comprador llega en el token.

## Se comunica con

| Con | Cómo | Para qué |
|---|---|---|
| Ventas → Catálogo | HTTP, con timeout | Precio vigente **[propuesta]** |
| Ventas → Inventario | HTTP, con idempotencia | Reservar, liberar, confirmar y descontar |
| Ventas → Mercado Pago | HTTP, con timeout y circuit breaker | Crear el pago |
| Mercado Pago → Ventas | Aviso por el gateway | Resultado del pago |
| Ventas → proveedor de mail | Fuera de la transacción de la venta | Mails al comprador y al administrador |
| Inventario → Ventas | Evento `ReservaVencida` | Pasar la venta a *Vencida* |

## Capacidad compartida (opción B)

Si se elige la **opción B · Envío de emails**, Ventas la expone con el contrato [`emails-v1`](../../docs/contracts/emails-v1.openapi.yaml), por un adaptador de entrada aparte que usa el puerto de mail existente y con una tabla propia ([ADR-008](../../docs/adr/ADR-008-contrato-propio.md)). Mock: [`mocks/emails-v1`](../../mocks/emails-v1).

## Más información

[Arquitectura §4 y §6](../../docs/ARCHITECTURE.md) · [ADR-001](../../docs/adr/ADR-001-limites-de-los-servicios.md) · [ADR-005](../../docs/adr/ADR-005-comunicacion.md)
