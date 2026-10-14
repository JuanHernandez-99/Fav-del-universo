# Inventario

Cuántas unidades hay y dónde: stock por variante y canal (online y físico), reservas con vencimiento, transferencias, ingresos, ajustes y movimientos.

> Estado: **previsto** (Entrega 2). Hoy solo está el diseño. **[propuesta]** marca lo no confirmado.

| | |
|---|---|
| **Base de datos** | MySQL ([ADR-003](../../docs/adr/ADR-003-persistencia.md)) |
| **Estilo interno** | Hexagonal **[propuesta]**: dominio, aplicación, puertos y adaptadores (http, mysql, rabbitmq, proceso de vencimiento) |
| **Operaciones críticas** | Reserva de stock online y transferencia entre stocks: sin sobreventa, sin duplicados, sin stock online negativo |

## Qué no hace

No conoce precios, compradores ni nombres de productos: solo ids de variante, cantidades y canales.

## Se comunica con

| Con | Cómo | Para qué |
|---|---|---|
| Ventas → Inventario | HTTP, con clave de idempotencia | Reservar, liberar, confirmar al pagar y descontar |
| Inventario → Ventas | Evento `ReservaVencida` | Pasar la venta a *Vencida* |
| Catálogo → Inventario | Evento `VarianteCreada` **[propuesta]** | Registrar la variante con stock 0 |
| Inventario → Gimnasio | HTTP, con timeout, reintentos e idempotencia | Consultar su stock de FAV y registrar reposiciones |

## Integración con otros grupos

- **Pedidos del grupo de proveedores:** no entran directo a Inventario. Ventas le pide reservar el stock online, igual que en una compra por transferencia ([ADR-008](../../docs/adr/ADR-008-contrato-propio.md)).
- **Consignación en el gimnasio (capacidad consumida):** Inventario llama a la API del gimnasio para consultar cuánto stock de FAV le queda y registrar reposiciones. Al reponer, descuenta el stock físico y guarda una *reposición pendiente* que un proceso envía con reintentos. Se hace con un adaptador de salida que traduce el modelo del gimnasio al de FAV. **[?]** Contrato del gimnasio, pendiente (ADR-009).

## Más información

[Arquitectura §4 y §7](../../docs/ARCHITECTURE.md) · [ADR-001](../../docs/adr/ADR-001-limites-de-los-servicios.md) · [ADR-005](../../docs/adr/ADR-005-comunicacion.md)
