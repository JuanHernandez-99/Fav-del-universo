# Frontend web

Tienda para el comprador y panel para el administrador. React con Vite ([ADR-017](../docs/adr/ADR-017-frontend.md)).

> Estado: **en curso**. Están el catálogo, la búsqueda, los filtros, la ficha, el carrito, el registro, el inicio de sesión la compra con los dos medios de pago, "Mis compras" y en el panel, las ventas (confirmar transferencias y avanzar pedidos) la venta del local y los productos (alta, edición, variantes, fotos y publicación), las promociones, el stock (todos los productos, con filtros, orden por cantidad y sus variantes; ingresos, transferencias, ajustes y stock a corregir) y el gimnasio (su stock, reposiciones y el estado de cada una). La política de cambios está publicada, sin el texto ni los contactos del cliente: se completan en `src/paginas/politica.js`.

- **El carrito vive en el navegador** y se envía completo a Ventas al comprar ([ADR-001](../docs/adr/ADR-001-limites-de-los-servicios.md)).
- **Habla solo con el gateway**, en el mismo origen. Todas las llamadas están en `src/api.js`.
- **La vista de stock del panel** la compone el frontend con datos de Catálogo e Inventario.

## Probarlo

Con el sistema levantado (`docker compose up --build`), la tienda está en http://localhost:8080.

```bash
node frontend/pruebas/datos-demo.mjs   # opcional: prendas inventadas, con stock y promociones
```

Para desarrollar, con recarga automática y el gateway levantado:

```bash
cd frontend
npm install
npm run dev     # http://localhost:5173
npm test        # lógica sin React: carrito, sesión, compra, panel e ilustraciones
```

## Carpetas

| | |
|---|---|
| `src/api.js` | Llamadas al gateway |
| `src/dominio.js` | Categorías, talles y formato de precios |
| `src/carrito/` | Lógica del carrito, con sus tests, y su estado en React |
| `src/sesion/` | Token en `sessionStorage`, con sus tests, y su estado en React |
| `src/compra/` | Clave de idempotencia, venta guardada en la pestaña y textos de estados y errores, con sus tests |
| `src/componentes/`, `src/paginas/` | Pantallas |
| `src/estilos.css` | Colores, tipografías y diseño |
| `pruebas/datos-demo.mjs` | Carga datos de ejemplo. No se usa en producción |

[Arquitectura §1 y §6](../docs/ARCHITECTURE.md) · Casos de uso en [`SPEC.md`](../SPEC.md)
