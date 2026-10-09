# Inventario

Cuántas unidades hay y dónde: stock por variante y canal (online y físico), reservas con vencimiento, transferencias, ingresos, ajustes y movimientos.

> Estado: **previsto** (Entrega 2). Hoy solo está el diseño. **[propuesta]** marca lo no confirmado.

| | |
|---|---|
| **Base de datos** | MySQL ([ADR-003](../../docs/adr/ADR-003-persistencia.md)) |
| **Estilo interno** | **[?]** a definir en el D2 |
| **Operaciones críticas** | Reserva de stock online y transferencia entre stocks: sin sobreventa, sin duplicados, sin stock online negativo |

## Qué no hace

No conoce precios, compradores ni nombres de productos: solo ids de variante, cantidades y canales.

## Se comunica con

| Con | Cómo | Para qué |
|---|---|---|
| Ventas → Inventario | HTTP, con clave de idempotencia | Reservar, liberar, confirmar al pagar y descontar |
| Inventario → Ventas | Evento `ReservaVencida` | Pasar la venta a *Vencida* |
| Catálogo → Inventario | Evento `VarianteCreada` **[propuesta]** | Registrar la variante con stock 0 |

## Capacidad compartida (opción A)

Si se elige la **opción A · Reserva de cupos**, Inventario la expone con el contrato [`cupos-v1`](../../docs/contracts/cupos-v1.openapi.yaml), por una entrada aparte y con tablas propias, para poder quitarla sin tocar el stock de FAV ([ADR-008](../../docs/adr/ADR-008-contrato-propio.md)). Mock: [`mocks/cupos-v1`](../../mocks/cupos-v1).

## Más información

[Arquitectura §4 y §7](../../docs/ARCHITECTURE.md) · [ADR-001](../../docs/adr/ADR-001-limites-de-los-servicios.md) · [ADR-005](../../docs/adr/ADR-005-comunicacion.md)
