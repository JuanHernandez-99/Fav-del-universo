# Consultas y agregaciones

> Consigna 5.3. Las consultas están en `consultas/consultas.js`, cada una con **objetivo, comando y resultado esperado** en comentarios. Su salida real, sobre los 279 documentos del cargador, está en `evidencia/consultas_salida.txt`. Acá se resume qué responde cada una y qué concepto de MongoDB muestra.

## Las 10 consultas

| # | Pregunta del dominio | Tipo que pide la consigna | Operadores | Resultado |
|---|---|---|---|---|
| 1 | ¿Qué productos publicados cuestan entre $10.000 y $30.000? | Comparación | `$gte`, `$lte` | 89 productos |
| 2 | ¿Qué promociones están vigentes hoy? | Comparación sobre fechas | `$lte`, `$gte` | 6 (quedan afuera 4 vencidas y 4 futuras) |
| 3 | ¿Qué remeras o jeans publicados cuestan menos de $20.000? | Operadores lógicos | `$and`, `$or`, `$lt` | 18 (todas remeras: el jean más barato cuesta $30.000) |
| 4 | ¿Qué productos son de algodón y se venden en pesos? | Atributos anidados | `atributos.material`, `precioBase.moneda` | 32 |
| 5 | ¿Qué productos tienen la variante M en negro? ¿Cuáles tienen talle S, M o L? ¿Cuáles tienen una sola variante? | Arreglos | `$elemMatch`, `$in`, `$size` | 8 · 98 · 28 |
| 6 | ¿Cuáles son los 5 productos publicados más nuevos? (solo nombre y precio) | Proyección, orden y límite | `find` + `sort` + `limit` | 5 |
| 7 | ¿Cuál es el precio vigente de cada producto con su promoción? | `aggregate` | `$lookup`, `$addFields`, `$project` | 73 de 205 publicados tienen promoción vigente |
| 8 | ¿Qué colores se ofrecen más? | `aggregate` | `$unwind`, `$group`, `$sort` | Azul y Negro, 64 variantes cada uno |
| 9 | Por categoría: cantidad, precio promedio, mínimo y máximo | Agrupamiento y estadísticas | `$group` con `$sum`, `$avg`, `$min`, `$max` | 13 categorías, de camperas ($81.302) a accesorios ($10.771) |
| 10 | ¿Qué categorías, talles, colores y rango de precios hay para armar los filtros de la tienda? | Complejidad elegida por el equipo | `$facet` | 13 categorías, 19 talles, 18 colores, $2.990 a $180.000 |
| Extra | ¿Qué productos mencionan "lino"? | Búsqueda de texto (extensión) | `$text` | 33 productos |

Los montos de las preguntas están en pesos, pero los comandos usan **centavos** (10.000 pesos es `1000000`), porque así se guardan (D13).

## Lo que muestra cada grupo

**Filtros simples (1 a 4).** `find` recibe un filtro, y los campos anidados se alcanzan con la notación de punto (`"atributos.material"`). La consulta 4 incluye una trampa útil para la defensa: comparar contra el subdocumento entero (`{ atributos: { material: "algodón" } }`) da **0** resultados, porque exige que `atributos` sea exactamente ese objeto, sin otros campos y en el mismo orden.

**Arreglos (5).** Como las variantes están embebidas, se consultan dentro del documento. La diferencia clave es entre `$elemMatch` y condiciones sueltas: con `"variantes.talle": "M"` y `"variantes.color": "Negro"` por separado salen **9** productos, porque cada condición puede cumplirla una variante distinta; con `$elemMatch` salen **8**, porque exige que sea *la misma* variante (M **y** negra). Es el mismo motivo por el que el listado de la API usa `$elemMatch` cuando se pasan `talle` y `color` juntos.

**Proyección, orden y límite (6).** La proyección devuelve solo los campos pedidos; el orden se desempata por `_id` para que el resultado sea siempre el mismo.

**Agregación (7 a 10).** `aggregate` procesa los documentos por etapas (*pipeline*): cada etapa recibe la salida de la anterior.

| Etapa | Qué hace | Dónde se usa |
|---|---|---|
| `$match` | Filtra, como `find` | 7, 8, 10 |
| `$unwind` | Abre un arreglo: un documento por elemento | 8, 10 (una fila por variante) |
| `$group` | Agrupa y calcula (`$sum`, `$avg`, `$min`, `$max`) | 8, 9, 10 |
| `$lookup` | Une con otra colección (el equivalente a un `JOIN`) | 7 (promociones), 9 (nombre de la categoría) |
| `$facet` | Corre varios pipelines sobre los mismos documentos y devuelve un solo resultado | 10 |

La **consulta 7** es la más importante: calcula el precio vigente **dentro de la base**, sin código de la aplicación. Por cada producto busca con `$lookup` las promociones vigentes que lo alcanzan (por id, por categoría o a toda la tienda), se queda con la de mayor porcentaje (no se acumulan, D3) y trunca a centavos enteros (D5). Se prueba con dos casos de la demo: el *Jean mom tiro alto* está en dos promociones (20% y 30%) y gana la de 30%; la *remera básica* cuesta $15.000 por transferencia y $13.500 en efectivo, porque la promoción depende del medio de pago. El mismo cálculo en el servicio Go queda para la etapa FAV (ver `06-conclusiones.md`).

El `$lookup` de la 7 y la 9 es la contrapartida de haber dejado las promociones y las categorías en colecciones aparte (ver `01-diseno.md`, sección 5): lo que se ganó en flexibilidad se paga con una unión al consultar. Se acepta porque las uniones son poco frecuentes y las colecciones, chicas.

## Fecha de referencia

Las consultas que dependen de "hoy" (2 y 7) usan una fecha fija, el día de la defensa (03/11/2026), para que los resultados sean reproducibles. La aplicación usaría la fecha actual.

**Se muestra con:**

```bash
docker compose exec mongo-catalogo sh -c 'mongosh --quiet -u "$MONGO_INITDB_ROOT_USERNAME" -p "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin catalogo --file /consultas/consultas.js'
```
