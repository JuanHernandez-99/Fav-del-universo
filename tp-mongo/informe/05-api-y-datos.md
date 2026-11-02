# API, datos, análisis y despliegue

> Consignas 5.2, 5.4, 5.7 y 5.8. Un resumen de lo que se implementó y dónde verlo; el detalle está en el código, en `README.md` y en `analisis/`.

## 1. API y capa de repositorio

Servicio en **Go** con el driver oficial de MongoDB, en tres capas. Cada una conoce solo a la siguiente:

| Capa | Archivo | Responsabilidad | Qué no hace |
|---|---|---|---|
| `handler` | `internal/handler/http.go` | Rutas, JSON de entrada y salida, traducción de errores a códigos HTTP | No conoce MongoDB |
| `service` | `internal/service/servicio.go` | Reglas de negocio y validaciones (talles, categoría, variantes repetidas), generación de ids | No conoce HTTP |
| `repository` | `internal/repository/mongo.go` | **Único lugar que habla con MongoDB**: filtros, operaciones atómicas, índices | No decide reglas |

El servicio no guarda estado en memoria (puede correr en varias réplicas), cierra ordenadamente al recibir `SIGTERM` y escribe logs en JSON.

### Rutas

| Método y ruta | Función | Respuestas |
|---|---|---|
| `POST /v1/admin/productos` | Crear (nace no publicado, con al menos una variante) | `201`, `400` |
| `GET /v1/admin/productos/{id}` | Obtener por id | `200`, `404` |
| `PUT /v1/admin/productos/{id}` | Actualizar datos (lleva `version`) | `200`, `400`, `404`, `409` |
| `DELETE /v1/admin/productos/{id}` | **Baja lógica**: despublica | `204` (idempotente), `404` |
| `POST /v1/admin/productos/{id}/publicar` | Publicar | `200`, `404` |
| `POST /v1/admin/productos/{id}/variantes` | Agregar variante (operación atómica, `04-consistencia.md`) | `201`, `400`, `404`, `409` |
| `GET /v1/admin/productos` | Listar con filtros, orden y paginación | `200`, `400` |
| `GET /healthz`, `GET /readyz` | Sondas; la segunda verifica la conexión a MongoDB | `200`, `503` |

**Filtros del listado:** `categoria`, `talle`, `color`, `precioMin`, `precioMax`, `q` (texto), `publicado`, `orden` y `pagina`/`tamano`. Un parámetro desconocido da `400`: así un error de tipeo no devuelve todo el catálogo sin avisar. El orden siempre se desempata por `_id`, para que la paginación no repita ni saltee resultados.

### Errores

Respuestas `application/problem+json` con un `codigo` estable. Una validación fallida lista **todos** los campos inválidos, no solo el primero.

| `codigo` | HTTP | Cuándo |
|---|---|---|
| `PEDIDO_INVALIDO` | 400 | Cuerpo o parámetros inválidos; `errores` indica los campos |
| `PRODUCTO_NO_ENCONTRADO` | 404 | El id no existe |
| `VARIANTE_DUPLICADA` | 409 | Ya hay una variante con ese talle y color |
| `CONFLICTO_DE_VERSION` | 409 | La `version` enviada no es la actual |
| `NO_DISPONIBLE` | 503 | Se agotó el tiempo (4 s) o se canceló el pedido |
| `ERROR_INTERNO` | 500 | Falla inesperada; el detalle va al log, no al cliente |

### Pruebas

| Tipo | Qué cubre |
|---|---|
| Tests unitarios en Go (`go test ./...`) | Generación y normalización de ids, conversión de montos a centavos, lectura de filtros, CORS, y que los talles de Go coincidan con los del esquema de MongoDB |
| Concurrencia | 20 pedidos simultáneos con la misma variante (`04-consistencia.md`) |
| Esquema de la base | Inserciones inválidas rechazadas (`pruebas/validacion_esquema.js`) |
| Manual, en Swagger | Cada ruta con ejemplos válidos e inválidos (`pruebas/*.json`) |

**Swagger UI** documenta la API y permite probarla desde el navegador: `http://localhost:8086`, a partir de `api/openapi.yaml`.

## 2. Datos de prueba

| Colección | Documentos | Contenido |
|---|---|---|
| `categorias` | 13 | Las 13 categorías de la tienda |
| `productos` | 252 | 2 del contrato de FAV con sus ids exactos + 250 generados; 881 variantes en total |
| `promociones` | 14 | 6 vigentes el día de la defensa, 4 vencidas y 4 futuras |
| **Total** | **279** | Por encima del mínimo de la consigna (100) |

**Variedad y casos límite:** productos despublicados; sin fotos ni atributos; con talle único, con una sola variante y con muchas; precios mínimo y máximo; nombres con tildes y ñ; un jean en dos promociones a la vez (gana la mayor); promociones de medio de pago y de alcance `TIENDA`; fotos asociadas y no asociadas a un color.

**Reproducibilidad.** Los datos los genera un script determinista (`datos/generar.py`, semilla fija) y los JSON ya generados se entregan. La carga corre dentro de Docker (`mongoimport` y `mongosh`), sin instalar nada:

| Script | Qué hace |
|---|---|
| `01_crear_colecciones_e_indices.js` | Colecciones, esquema de validación e índices (idempotente) |
| `02_cargar.sh` | Vacía las colecciones con `deleteMany` y vuelve a importar, así se repite sin duplicar. No usa `--drop`, que borraría los índices |
| `03_verificar.js` | Comprueba cantidades, categorías existentes, ids de variante únicos, talles del sistema correcto y vigencias válidas. Si algo falla, termina con error |

Dos modos de carga: `completa` (el volumen del TP) y `CARGA=minimo` (solo los 2 productos del contrato, que es lo que usa FAV).

## 3. Análisis de datos

Notebook de Jupyter con PyMongo, Pandas y Matplotlib que lee la base (`analisis/catalogo.ipynb`, ejecutado, y `analisis/catalogo.html`). Las credenciales salen de variables de entorno, no del notebook.

| # | Indicador | Pregunta | Gráfico |
|---|---|---|---|
| 1 | Precio promedio, mínimo y máximo por categoría | ¿Dónde está concentrado el precio? | Barras con rango |
| 2 | Productos y variantes por categoría | ¿Dónde es más amplia la oferta? | Barras agrupadas |
| 3 | % de productos con promoción vigente por categoría | ¿Qué se promociona y qué no? | Barras |
| 4 | Variantes por talle y color | ¿Falta variedad en algún talle o color? | **Mapa de calor** |

Cada indicador tiene su interpretación escrita. **Conclusión del análisis:** precio y variedad van en sentidos opuestos (las categorías más baratas tienen más variantes y la más cara, camperas, tiene pocas); las promociones se concentran en abrigo, jeans y accesorios, y dejan sin descuento a las prendas de primavera-verano; y la oferta de talles se concentra en los del medio y deja cortos los extremos. Recomendaciones: lanzar una promoción por categoría en tops, shorts y polleras (basta una promoción nueva, sin tocar productos, gracias a que viven en su propia colección) y ampliar talles y variedad en camperas y en los extremos de talle.

No se calculan indicadores de stock ni de rotación, porque el stock no está en esta base (D1).

## 4. Despliegue con Docker Compose

| Servicio | Qué es | Puerto |
|---|---|---|
| `mongo-catalogo` | MongoDB 7 con volumen persistente y `healthcheck` | 27017 |
| `catalogo` | La API. Se conecta por el **nombre del servicio** (`mongo-catalogo:27017`), nunca por `localhost` | 8085 |
| `swagger-ui` | Documentación interactiva | 8086 |
| `cargador` | Corre los scripts 01 a 03 (`docker compose run --rm cargador`) | — |

Las credenciales salen de `.env`, que no se entrega; solo se entrega `.env.example`, sin datos reales. La conexión usa `authSource=admin`, porque el usuario root de las variables `MONGO_INITDB_ROOT_*` vive en la base `admin`.

**Se muestra con:** `docker compose up -d --build`, `docker compose run --rm cargador` y luego `http://localhost:8086` (API) y `analisis/catalogo.html` (análisis). Los comandos completos están en el `README.md`.
