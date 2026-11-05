# ADR-017 · Frontend: tecnología y cómo se sirve

| | |
|---|---|
| **Estado** | Aceptada |
| **Fecha** | 05/11/2026 |
| **Decisión de la consigna** | Adicional. Completa D2 y D12 |
| **Relacionadas** | ADR-001 (carrito en el navegador), ADR-006 (disponibilidad), ADR-012 (gateway), ADR-016 (token) |

## Contexto

El sistema necesita una tienda y un panel web. La tecnología es libre (ARCHITECTURE §1). El carrito vive en el navegador y la tienda habla solo con el gateway (ADR-001). Compose tiene que levantar todo con un solo comando.

## Criterios

1. Que se pueda explicar y mantener con el tiempo que queda hasta la presentación.
2. Un solo punto de entrada para el navegador, sin configuración de CORS.
3. Que la tienda se siga viendo si cae un servicio.

## Decisión

| Tema | Decisión |
|---|---|
| **Tecnología** | React con Vite, en JavaScript. Rutas con React Router |
| **Cómo se sirve** | Se compila a archivos estáticos y los entrega un contenedor nginx, sin puerto publicado |
| **Por dónde se entra** | Por el gateway: `/` va al frontend y las rutas de la API van a los servicios. Mismo origen, sin CORS |
| **Rutas de API inexistentes** | El gateway responde `404` en JSON bajo `/v1/`, `/ventas/` y `/usuarios/`. Si no, recibirían la página de la tienda con un `200` |
| **Tipografías** | Incluidas en el paquete (`@fontsource`). La tienda no depende de un servicio externo |
| **Composición** | Productos y unidades disponibles se piden por separado y el frontend junta las respuestas (ADR-006) |
| **Stock visible** | El comprador ve las unidades online disponibles, sin físico ni reservadas (HU-01.5). La ficha y el carrito no dejan pedir más. Es un dato del momento: la reserva la decide Inventario al comprar (`409 STOCK_INSUFICIENTE`) |

## Alternativas consideradas

| Alternativa | Por qué se descartó |
|---|---|
| **Páginas generadas por el servidor** (Next.js) | Suma un servidor Node más para operar, sin una necesidad concreta |
| **HTML y JavaScript sin framework** | Sin dependencias, pero el carrito, los filtros y el panel requieren estado que habría que resolver a mano |
| **Frontend en otro origen, con CORS** | Más configuración y un segundo punto de entrada público |

## Consecuencias

**Positivas**

- El navegador ve un solo origen, y el gateway registra y limita todo el tráfico.
- Sin Inventario, la tienda sigue mostrando las prendas y avisa que no puede consultar el stock.

**Negativas aceptadas**

- **El stock online queda a la vista de cualquiera,** también de la competencia, porque la ruta es pública. Se decidió así para que el comprador sepa cuánto hay y HU-10.2 avise antes de comprar. A confirmar con el cliente.
- **Las opciones del filtro de color** salen de los primeros 100 productos, porque el color es texto libre y no hay una ruta de colores.
- **Los talles y las categorías** están repetidos en el frontend (RN-02, RN-17), porque no hay una ruta pública que los liste.

## Pendientes

- Las fotos reales: sin foto, o si no carga, se muestra una silueta de la prenda en el color elegido.
- Dónde se guarda el token: se decide con el inicio de sesión.

## Validación

El 05/11/2026, en un navegador real (Edge) y por el gateway: catálogo, búsqueda, filtros, orden, promociones, ficha, carrito, avisos de sin stock y de prenda despublicada, y vista de celular. Con Inventario caído se ven las prendas con un aviso, sin marcarlas "sin stock", y no se puede agregar al carrito. Con Catálogo caído aparece un mensaje claro, y "Reintentar" recupera la tienda cuando vuelve.
