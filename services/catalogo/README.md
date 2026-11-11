# Catálogo

Qué se vende y cómo se muestra: productos, variantes (talle y color), fotos, categorías, precios base, promociones y publicación. Calcula el **precio vigente**.

> Estado: **funcionando**. Productos, promociones, tienda, búsqueda, caché, consulta por lote con precio vigente y la capacidad publicada (`GET /v1/productos`). **[propuesta]** marca lo no confirmado.

## Qué hay hoy

| | |
|---|---|
| **API** | CRUD de productos bajo `/v1/admin/productos`, publicar, agregar variante (operación atómica) y listado con filtros, orden y paginación. Sondas `/healthz` y `/readyz` |
| **Promociones** | `GET`, `POST /v1/promociones` y `PUT /v1/promociones/{id}`. Por el gateway, en `/catalogo/`. Se cancela editando la vigencia |
| **Tienda** | `GET /v1/tienda/productos` y `GET /v1/tienda/productos/{id}`: solo publicados, con fotos, precio con y sin promoción, filtros y paginación. Por el gateway, en `/catalogo/v1/tienda/`, sin token. No depende de `CAPACIDAD_PUBLICA_ACTIVA` |
| **Búsqueda** | Con `q`, la tienda busca en Elasticsearch: relevancia, tolerancia a errores de tipeo, filtros, orden y paginación. El índice se actualiza con el evento `ProductoCambiado` y se reconstruye con `POST /v1/admin/busqueda/reconstruir` ([ADR-006](../../docs/adr/ADR-006-busqueda.md)). Se prueba con `pruebas/busqueda.sh` |
| **Caché** | Memcached para las lecturas de la tienda, con `CACHE_ENABLED=false` para medir sin ella ([ADR-007](../../docs/adr/ADR-007-cache.md)). Se prueba con `pruebas/cache.sh`. Métricas en `GET /metrics` |
| **Observabilidad** | Logs JSON con `correlation_id` (el track id), también en `app_logs` de `mysql-logs`. El track id viaja a Elasticsearch y en el evento de indexación: el producto guarda el del pedido que lo cambió ([ADR-020](../../docs/adr/ADR-020-logs-y-track-id-modelo-catedra.md)) |
| **Consulta por lote** | `GET /v1/variantes?variantes=a,b,c` (hasta 200 ids), para Ventas. Detalle en `api/openapi.yaml` |
| **Código** | Go, en capas `handler` → `service` → `repository` (único que habla con MongoDB). Sin estado en memoria |
| **Base** | MongoDB con tres colecciones (`productos`, `categorias`, `promociones`), índices y validación de esquema |
| **Documentación** | Especificación en `api/openapi.yaml` y decisiones en `DISENO.md` |

Se levanta con el `docker-compose.yml` de la raíz: dos réplicas detrás del gateway, más `catalogo-init`, que prepara la base (`DISENO.md`, sección 8). Desde afuera se entra por el gateway, en `/catalogo/`, con el token de administrador; la tienda no lo necesita. Ventas usa la entrada interna `http://gateway:8081/catalogo/`. Tests: `go test ./...`.

Carpetas: `api/` (OpenAPI), `db/` (scripts, datos de ejemplo y consultas de MongoDB) y `pruebas/` (concurrencia, esquema, búsqueda, caché y comparación con Solr).

| | |
|---|---|
| **Base de datos** | MongoDB, un documento por producto con sus variantes ([ADR-003](../../docs/adr/ADR-003-persistencia.md)) |
| **Estilo interno** | Capas: Handler → Service → Repository (D2) |
| **Réplicas** | Dos, por ser el servicio más leído ([ADR-012](../../docs/adr/ADR-012-balanceo.md)) |

## Qué no hace

No sabe cuántas unidades hay: la disponibilidad es de [Inventario](../inventario).

## Se comunica con

| Con | Cómo | Para qué |
|---|---|---|
| Ventas → Catálogo | HTTP (`GET /v1/variantes`) | Precio vigente al comprar |
| Catálogo → Inventario | Evento `VarianteCreada` **[propuesta]** | Registrar la variante con stock 0 |
| Catálogo → índice de búsqueda | Evento `ProductoCambiado` | Mantener la búsqueda actualizada (ADR-006) |
| Catálogo → Memcached | Caché de lecturas | Ficha, listados y promociones para mostrar (ADR-019) |

## Capacidad compartida: catálogo

Catálogo expone al grupo de proveedores los productos publicados con sus variantes y el precio vigente (`GET /v1/productos`, contrato [`catalogo-pedidos-v1`](../../docs/contracts/catalogo-pedidos-v1.openapi.yaml)). Entra por un adaptador aparte, que se apaga con `CAPACIDAD_PUBLICA_ACTIVA=false` ([ADR-008](../../docs/adr/ADR-008-contrato-propio.md)).

```bash
curl localhost:8080/v1/productos -H "X-API-Key: clave-grupo-proveedores"
```

## Más información

[Arquitectura §4](../../docs/ARCHITECTURE.md) · [ADR-001](../../docs/adr/ADR-001-limites-de-los-servicios.md) · [ADR-005](../../docs/adr/ADR-005-comunicacion.md)
