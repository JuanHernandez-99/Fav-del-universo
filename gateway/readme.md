# API Gateway

Punto de entrada único del sistema: valida el token, rutea a los servicios y controla el tráfico.

> Estado: **previsto** (Entrega 2). Hoy solo está el diseño.

- Valida la firma y el vencimiento del JWT **sin llamar a Usuarios**, descarta los encabezados de identidad que vengan del cliente y reenvía `X-User-Id`, `X-User-Email` y `X-User-Role` a los servicios ([ADR-001](../docs/adr/ADR-001-limites-de-los-servicios.md)).
- Aplica límite de pedidos (*rate limiting*), también por grupo consumidor de la capacidad compartida.
- Valida la clave `X-API-Key` de los grupos consumidores **[propuesta]** ([ADR-008](../docs/adr/ADR-008-contrato-propio.md)).
- Recibe el aviso de pago de Mercado Pago y lo reenvía a Ventas.
- **No tiene lógica de negocio ni compone respuestas** de varios servicios.
- Balanceo entre las réplicas de Catálogo: D12 **[?]**.

[Arquitectura §6](../docs/ARCHITECTURE.md)
