# Conclusiones

## Qué se logró

- Un **modelo documental** de tres colecciones, con las variantes, fotos, precio y atributos embebidos en el producto, y la categoría y las promociones separadas, justificado por cómo se leen y se escriben los datos (`01-diseno.md`).
- Una base **reproducible**: 279 documentos con casos límite, scripts que se pueden repetir, verificación automática y esquema de validación dentro de MongoDB (`05-api-y-datos.md`).
- **10 consultas** de tipos distintos, más una de texto, que responden preguntas del negocio, incluido el precio vigente calculado con un `$lookup` (`02-consultas.md`).
- **Índices** pensados para cada consulta y medidos con `explain("executionStats")`, con su beneficio y su costo (`03-indices.md`).
- Un **caso de consistencia** resuelto con una operación atómica sobre un documento y demostrado con 20 pedidos simultáneos: un solo `201` y diecinueve `409` (`04-consistencia.md`).
- Una **API en Go** con capa de repositorio, validaciones, errores estables y documentación en Swagger, y un **análisis** con 4 indicadores y recomendaciones.

## Qué aprendimos sobre MongoDB

| Idea | Cómo se vio en el proyecto |
|---|---|
| El modelo sale de los patrones de acceso, no de las tablas | Variantes embebidas porque se leen siempre con el producto; promociones aparte porque viven y se aplican por separado |
| Una escritura sobre un documento es atómica | La variante repetida se resuelve sin transacciones, poniendo la condición dentro del filtro |
| Un índice único sobre un arreglo no evita repetidos dentro del mismo documento | Hizo falta la actualización condicional además del índice `variantes_id_unico` |
| Sin esquema fijo no significa sin reglas | `$jsonSchema` agrega una segunda barrera, aunque no reemplaza las validaciones del servicio |
| Los índices se pagan | Cada índice ocupa espacio y se actualiza en cada escritura; se aceptan porque hay un solo administrador y mucha lectura |
| No todo conviene en MongoDB | El stock quedó en Inventario (MySQL): se lee poco, no puede venderse de más y una compra toca varias variantes |

## Alcance recortado (queda para la etapa FAV)

Para llegar a la entrega se dejaron afuera tres cosas. El modelo y los datos ya las contemplan; falta el código Go:

- **Cálculo del precio vigente en el servicio.** Hoy lo resuelve la consulta 7 dentro de la base. En Go será una función aislada, con tests unitarios.
- **Consulta por lote de variantes** (`GET /v1/variantes`), que van a usar Ventas y el panel de administración.
- **Gestión de promociones por API.** Hoy se cargan con el cargador y se consultan en MongoDB.

Quedan también para FAV: el adaptador del contrato público (solo publicados, sin stock), el evento `VarianteCreada`, la búsqueda externa, la caché, dos réplicas detrás del gateway y la autenticación. Ninguna cambia lo implementado.

## Limitaciones conocidas

| Limitación | Detalle |
|---|---|
| Pruebas | Hay tests unitarios y de concurrencia, pero no del servicio ni de integración del repositorio contra un MongoDB real |
| Color de texto libre | No hay lista de colores: `Negro` y `negro` son distintos para los filtros y el análisis (aunque no pueden convivir en un mismo producto, porque dan el mismo id) |
| Producto duplicado | Dos productos con el mismo nombre se aceptan: el segundo recibe un sufijo en el id. No hay clave natural que lo impida |
| Listado por precio base | Filtra y ordena por el precio base, no por el vigente con promoción |
| Sin autenticación | Las rutas de administración están abiertas; en FAV las protege el gateway |
| Una sola instancia de MongoDB | Sin réplicas, por lo que no hay transacciones multidocumento. Alcanzan para este caso, no para repartir stock entre varios productos |

## Mejoras posibles

1. Tests de integración del repositorio contra un MongoDB de prueba.
2. Normalizar los colores (lista de referencia o una sola forma de escribirlos).
3. Validación de que el talle sea del sistema del producto también en el esquema de la base.
4. Un replica set de un solo nodo en el Compose, para poder usar transacciones si algún día hacen falta (por ejemplo, si el stock se moviera a MongoDB).
