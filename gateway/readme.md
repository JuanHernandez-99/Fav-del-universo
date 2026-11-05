# API Gateway

Punto de entrada único del sistema: valida el token, rutea a los servicios y controla el tráfico.

> Estado: **funcionando**: nginx valida el token, rutea, balancea y limita el tráfico ([ADR-016](../docs/adr/ADR-016-validacion-del-token-en-el-gateway.md), [ADR-012](../docs/adr/ADR-012-balanceo.md)).

- Valida la firma y el vencimiento del JWT **sin llamar a Usuarios**, con njs (`njs/jwt.js`), descarta los encabezados de identidad que vengan del cliente y reenvía `X-User-Id`, `X-User-Email` y `X-User-Role` a los servicios ([ADR-001](../docs/adr/ADR-001-limites-de-los-servicios.md)).
- Aplica límite de pedidos (*rate limiting*), también por grupo externo que usa la capacidad compartida.
- Valida la clave `X-API-Key` de los grupos externos **[propuesta]** y rutea `GET /v1/productos` a Catálogo y `POST /v1/pedidos` a Ventas ([ADR-008](../docs/adr/ADR-008-contrato-propio.md)).
- Recibe el aviso de pago de Mercado Pago y lo reenvía a Ventas.
- Deja pasar sin token la lectura de la tienda: `/catalogo/v1/tienda/productos` y `/inventario/v1/disponibilidad`. El resto de `/catalogo/` e `/inventario/` sigue siendo solo de administrador.
- Sirve la tienda en `/`, en el mismo origen que la API ([ADR-017](../docs/adr/ADR-017-frontend.md)).
- **No tiene lógica de negocio ni compone respuestas** de varios servicios.
- Balancea entre las dos réplicas de Inventario y las dos de Catálogo.

**Después de cambiar su configuración, reiniciarlo:** `docker compose restart gateway`. nginx la arma al arrancar.

## Probar la validación del token

```bash
docker compose up -d --build
node gateway/pruebas/token.mjs
```

Fabrica tokens falsos (sin firma, firmados con otra clave, con el rol cambiado, vencidos) y comprueba roles, "Mis compras" y el funcionamiento con Usuarios caído. Último resultado, el 04/11/2026: 42 de 42.

## Probar las rutas de la tienda

```bash
node gateway/pruebas/tienda.mjs
```

Comprueba productos, filtros, paginación, promociones, hay / no hay, que el resto siga protegido (también con `../`) y las caídas de Inventario y de Catálogo. Detiene y vuelve a levantar esos dos servicios, y deja datos de prueba en la base. Último resultado, el 05/11/2026: 44 de 44 en tres corridas seguidas; una corrida anterior falló en un chequeo, sin causa confirmada.

[Arquitectura §6](../docs/ARCHITECTURE.md)
