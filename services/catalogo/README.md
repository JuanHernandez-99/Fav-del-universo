# Catálogo

Qué se vende y cómo se muestra: productos, variantes (talle y color), fotos, categorías, precios base, promociones y publicación. Calcula el **precio vigente**.

> Estado: **previsto** (Entrega 2). Hoy solo está el diseño. **[propuesta]** marca lo no confirmado.

|                    |                                                                                                          |
| ------------------ | -------------------------------------------------------------------------------------------------------- |
| **Base de datos**  | MongoDB, un documento por producto con sus variantes ([ADR-003](../../docs/adr/ADR-003-persistencia.md)) |
| **Estilo interno** | Capas: Handler → Service → Repository (D2)                                                               |
| **Réplicas**       | Dos, por ser el servicio más leído **[propuesta]** (D12)                                                 |

## Qué no hace

No sabe cuántas unidades hay: la disponibilidad es de [Inventario](../inventario).

## Se comunica con

| Con                           | Cómo                    | Para qué                              |
| ----------------------------- | ----------------------- | ------------------------------------- |
| Ventas → Catálogo             | HTTP                    | Precio vigente al comprar             |
| Catálogo → Inventario         | Evento `VarianteCreada` | Registrar la variante con stock 0     |
| Catálogo → índice de búsqueda | Eventos **[propuesta]** | Mantener la búsqueda actualizada (D6) |
| Catálogo → Redis              | Caché de lecturas       | Ficha y listados (D7)                 |

## Capacidad compartida: catálogo

Catálogo expone al grupo de proveedores los productos publicados con sus variantes y el precio vigente (`GET /v1/productos`, contrato [`catalogo-pedidos-v1`](../../docs/contracts/catalogo-pedidos-v1.openapi.yaml)). Entra por un adaptador aparte, que se puede desactivar ([ADR-008](../../docs/adr/ADR-008-contrato-propio.md)).

## Más información

[Arquitectura §4](../../docs/ARCHITECTURE.md) · [ADR-001](../../docs/adr/ADR-001-limites-de-los-servicios.md) · [ADR-005](../../docs/adr/ADR-005-comunicacion.md)
