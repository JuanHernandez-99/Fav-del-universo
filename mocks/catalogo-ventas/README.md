# Mock · Catálogo y Ventas

Atiende la capacidad publicada ([`catalogo-pedidos-v1`](../../docs/contracts/catalogo-pedidos-v1.openapi.yaml)) mientras Catálogo y Ventas no existen. A diferencia del mock con Prism ([`catalogo-pedidos-v1`](../catalogo-pedidos-v1)), **aparta stock de verdad en Inventario**.

- `GET /v1/productos`: catálogo fijo, el del ejemplo del contrato.
- `POST /v1/pedidos`: valida el pedido según el contrato, calcula el total y reserva en Inventario por 48 h. Usa los tiempos y reintentos del ADR-005: timeout de 2 s y 2 reintentos (200 y 400 ms con un componente aleatorio), solo ante fallas transitorias y siempre con la misma clave de idempotencia. Traduce los errores de Inventario a los del contrato.

No guarda ventas: el `pedidoId` es el id de la reserva. Las claves usadas se recuerdan en memoria para responder `CLAVE_IDEMPOTENCIA_REUTILIZADA`; si el mock se reinicia, igual no se duplica nada porque Inventario conserva la reserva.

Se levanta con el `docker-compose.yml` de la raíz, detrás del gateway (`http://localhost:8080`, header `X-API-Key: clave-grupo-proveedores`).
