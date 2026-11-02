# Catálogo

Qué se vende y cómo se muestra: productos, variantes (talle y color), fotos, categorías, precios base, promociones y publicación. Calcula el **precio vigente**.

> Estado: **CRUD de productos implementado** (TP de MongoDB). Quedan para la etapa FAV el precio vigente, la consulta por lote, las promociones por API y lo que se comunica con otros servicios (tablas de abajo). **[propuesta]** marca lo no confirmado.

## Qué hay hoy

| | |
|---|---|
| **API** | CRUD de productos bajo `/v1/admin/productos`, publicar, agregar variante (operación atómica) y listado con filtros, orden y paginación. Sondas `/healthz` y `/readyz` |
| **Código** | Go, en capas `handler` → `service` → `repository` (único que habla con MongoDB). Sin estado en memoria |
| **Base** | MongoDB con tres colecciones (`productos`, `categorias`, `promociones`), índices y validación de esquema |
| **Documentación** | Especificación en `tp-mongo/api/openapi.yaml` y decisiones en `tp-mongo/PROYECTO.md` |

Se levanta con el Compose de `tp-mongo/`, que construye la imagen de esta carpeta (ver el `README.md` de esa carpeta). Los tests se corren con `go test ./...`.

| | |
|---|---|
| **Base de datos** | MongoDB, un documento por producto con sus variantes ([ADR-003](../../docs/adr/ADR-003-persistencia.md)) |
| **Estilo interno** | Capas: Handler → Service → Repository (D2) |
| **Réplicas** | Dos, por ser el servicio más leído **[propuesta]** (D12) |

## Qué no hace

No sabe cuántas unidades hay: la disponibilidad es de [Inventario](../inventario).

## Se comunica con

| Con | Cómo | Para qué |
|---|---|---|
| Ventas → Catálogo | HTTP | Precio vigente al comprar **[propuesta]** |
| Catálogo → Inventario | Evento `VarianteCreada` **[propuesta]** | Registrar la variante con stock 0 |
| Catálogo → índice de búsqueda | Eventos **[propuesta]** | Mantener la búsqueda actualizada (D6) |
| Catálogo → Redis | Caché de lecturas | Ficha y listados (D7) |

## Capacidad compartida: catálogo

Catálogo expone al grupo de proveedores los productos publicados con sus variantes y el precio vigente (`GET /v1/productos`, contrato [`catalogo-pedidos-v1`](../../docs/contracts/catalogo-pedidos-v1.openapi.yaml)). Entra por un adaptador aparte, que se puede desactivar ([ADR-008](../../docs/adr/ADR-008-contrato-propio.md)).

## Más información

[Arquitectura §4](../../docs/ARCHITECTURE.md) · [ADR-001](../../docs/adr/ADR-001-limites-de-los-servicios.md) · [ADR-005](../../docs/adr/ADR-005-comunicacion.md)
