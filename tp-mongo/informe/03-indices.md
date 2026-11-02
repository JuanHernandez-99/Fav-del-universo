# Índices y análisis de rendimiento

> Consigna 5.5. Los índices se crean en `scripts/01_crear_colecciones_e_indices.js`. El servicio crea además, al arrancar, `variantes_id_unico`, porque de él depende una regla y no solo el rendimiento.
> Las mediciones salen de `consultas/indices_explain.js` y están en `evidencia/indices_explain.txt`. Los datos son los del cargador: 252 productos con 881 variantes, y 14 promociones.

## Qué es un índice y cómo se lee un `explain`

Un índice es una estructura ordenada aparte (un *B-tree*) que va de los valores de uno o más campos a los documentos que los tienen. Con él, Mongo encuentra un valor sin recorrer toda la colección.

`explain("executionStats")` muestra el plan que eligió Mongo y cuánto trabajo hizo:

| Dato | Qué indica |
|---|---|
| `COLLSCAN` | Leyó **todos** los documentos de la colección |
| `IXSCAN` → `FETCH` | Buscó en el índice y trajo solo los documentos que este le indicó |
| `SORT` | Ordenó en memoria, porque el índice no le dio el orden pedido |
| claves examinadas | Entradas del índice que recorrió |
| documentos examinados | Documentos que tuvo que leer |
| devueltos | Documentos del resultado |

**La señal de un buen índice** es que los documentos examinados se acerquen a los devueltos.

**Método:** cada consulta se corre dos veces. La primera con `hint({ $natural: 1 })`, que obliga a leer la colección entera como si no hubiera índice. La segunda con el plan que elige Mongo. Así la comparación es sobre los mismos datos.

## Resumen

| Índice | Consulta | Sin índice: examinados → devueltos | Con índice: examinados → devueltos |
|---|---|---|---|
| `variantes_id_unico` | Una variante por id (Ventas) | 252 → 1 | **1 → 1** |
| `publicado_categoria_precio` | Jeans publicados de $20.000 a $40.000, por precio | 252 → 3 (y `SORT`) | **3 → 3** (sin `SORT`) |
| `texto_nombre_descripcion` | Buscar "lino" | 252 → 42 | **42 → 42** |
| `variantes_talle_color` | Talle M en negro (`$elemMatch`) | 252 → 8 | **8 → 8** |
| `vigencia` | Promociones vigentes hoy | 14 → 6 | 6 → 6 |
| `alcance` | Promociones de los buzos | 14 → 1 | 1 → 1 |

Los tiempos dan entre 0 y 1 ms en todos los casos. Con este volumen no hay diferencias de tiempo para mostrar, y la consigna lo anticipa: lo que se evalúa es la pertinencia de cada índice y la interpretación del plan.

## Cada índice

### 1. `variantes_id_unico`: `{ "variantes.id": 1 }`, único

- **Para qué:** Ventas pregunta "¿qué es `var-jean-mom-celeste-38`?" con un timeout de 1 s, y el panel pide variantes por lote para combinarlas con el stock.
- **Por qué este campo:** es la búsqueda más frecuente y la más sensible al tiempo.
- **Resultado:**
  - **Un id:** pasa de 252 documentos leídos a 1.
  - **Lote de 3 ids (`$in`):** examina 5 claves y devuelve 2 documentos, porque dos de las variantes pedidas pertenecen a la misma Remera básica.
- **Multikey:** como `variantes` es un arreglo, el índice tiene **una entrada por variante**: 881 entradas para 252 documentos.
- **Además es una regla:** al ser **único**, impide que dos productos tengan el mismo id de variante. Si los tuvieran, Inventario mezclaría el stock de las dos. Por eso lo crea también el servicio al arrancar.
- **Límite:** un índice único no evita repetidos *dentro del mismo arreglo*. Eso lo cubre la operación atómica de agregar variante (5.9).

### 2. `publicado_categoria_precio`: `{ publicado: 1, categoria: 1, "precioBase.monto": 1 }`

- **Para qué:** el listado de la tienda: "jeans publicados entre $20.000 y $40.000, ordenados por precio".
- **Por qué este orden de campos: la regla ESR (*Equality, Sort, Range*).** Primero van los campos de **igualdad** (`publicado`, `categoria`) y después el de **orden y rango** (`precio`). Mongo salta directo a "publicado = true, categoría = jeans", y ahí los precios ya están ordenados.
- **Resultado:** de 252 documentos y un `SORT` en memoria se pasa a **3 documentos y ningún `SORT`**. El índice entrega el orden.
- **Desempate por `_id` (caso 2b):** la API ordena por precio y desempata por `_id`, para que la paginación no repita ni saltee productos. Como `_id` no está en el índice, reaparece un `SORT`. Ese ordenamiento es solo sobre los 3 documentos ya filtrados, así que su costo es mínimo.
  - **Mejora posible:** agregar `_id` al final del índice (`{ publicado, categoria, precioBase.monto, _id }`). Elimina ese `SORT`, a cambio de un índice algo más grande.
- **Prefijo (caso 2c):** la consulta 1 filtra `publicado` y precio, pero no `categoria`. El índice se usa igual: los límites muestran `categoria: [MinKey, MaxKey]`, es decir, Mongo recorre cada categoría dentro del rango de precio. Por eso examina 100 claves para 89 resultados. Es un poco menos eficiente que con la categoría fija, pero muy lejos de los 252 documentos.

### 3. `texto_nombre_descripcion`: `{ nombre: "text", descripcion: "text" }`, en español

- **Para qué:** el buscador de la tienda.
- **Por qué texto y no una expresión regular:** el índice de texto separa en palabras y las reduce a su raíz en español.
  - **"remeras" encuentra 26 productos** aunque los nombres dicen "Remera".
  - Con `/remeras/i` se encuentran **0**.
- **Resultado:** la búsqueda de "lino" lee 42 documentos contra los 252 de la expresión regular.
  - `$text` **exige** este índice, así que no se puede forzar un `COLLSCAN`. La comparación es contra la alternativa sin índice, que es la expresión regular.
- **Restricción:** solo puede haber un índice de texto por colección.

### 4. `variantes_talle_color`: `{ "variantes.talle": 1, "variantes.color": 1 }`

- **Para qué:** el filtro de la tienda por talle y color.
- **Por qué se permite un compuesto sobre un arreglo:** los dos campos están dentro del **mismo** arreglo. No se permite un índice compuesto sobre dos arreglos distintos.
- **Resultado:** depende de cómo se escriba la consulta. Este es el hallazgo más interesante del análisis.

| Consulta | Límites del índice | Claves | Documentos examinados | Devueltos |
|---|---|---|---|---|
| `{ variantes: { $elemMatch: { talle: "M", color: "Negro" } } }` | talle `["M","M"]`, color `["Negro","Negro"]` | 8 | 8 | 8 |
| `{ "variantes.talle": "M", "variantes.color": "Negro" }` | talle `["M","M"]`, color `[MinKey, MaxKey]` | 86 | 45 | 9 |

  - **Con `$elemMatch`**, Mongo sabe que talle y color son **de la misma variante** y acota los dos campos en el índice.
  - **Con condiciones sueltas**, la pregunta es otra ("alguna variante M y alguna variante negra"). Mongo solo puede acotar el talle y revisa el color documento por documento. Por eso lee 45 para devolver 9.
  - **Conclusión:** `$elemMatch` es más correcto para la pregunta y además aprovecha mejor el índice. Es lo que usan la API y la operación atómica.

### 5. `vigencia`: `{ "vigencia.desde": 1, "vigencia.hasta": 1 }` (promociones)

- **Para qué:** "¿qué promociones rigen hoy?" (`desde ≤ hoy ≤ hasta`). Se repite en cada cálculo de precio vigente.
- **Resultado:** examina 11 claves (las promociones que empezaron antes de hoy) y lee solo las 6 vigentes, contra 14 sin índice.
- **Evaluación honesta:** con 14 promociones el beneficio es despreciable, y el índice ocupa más que los datos (ver costo). Se justifica por el **patrón de consulta**: el historial de promociones crece todos los meses y nunca se borra (una promoción se cancela editando su vigencia).
- **Límite conocido:** con dos rangos, el índice acota bien el primero (`desde`). El segundo lo filtra dentro del índice, sin leer el documento.

### 6. `alcance`: `{ "alcance.tipo": 1, "alcance.ids": 1 }` (promociones)

- **Para qué:** "¿qué promociones alcanzan a los buzos?".
- **Resultado:** lee 1 documento contra 14. Es multikey sobre `alcance.ids`.
- **Evaluación:** igual que el anterior, el beneficio con 14 promociones es mínimo y se justifica por el crecimiento esperado.

## Costo: espacio y escrituras

| Colección | Datos | Índices |
|---|---|---|
| `productos` | 193 KB | **252 KB** (`_id` 44 · `variantes_id_unico` 60 · `publicado_categoria_precio` 36 · texto 60 · `variantes_talle_color` 52) |
| `promociones` | 5 KB | **108 KB** (36 KB cada uno, que es el mínimo que reserva Mongo por índice) |

- **Espacio:** en `productos`, los índices **ocupan más que los datos**. Los dos multikey de variantes tienen 881 entradas cada uno, y el de texto guarda cada palabra de nombre y descripción.
- **Escrituras:** cada alta o cambio de un producto actualiza **todos** sus índices. Agregar una variante suma una entrada en cada índice multikey, y un producto con 24 variantes aporta 24 entradas a cada uno.
- **Por qué se acepta:** el catálogo se lee mucho y lo escribe un solo administrador, pocas veces por día (ADR-003). Cuesta más escribir y ocupa más espacio, a cambio de lecturas que no dependen del tamaño del catálogo.
- **Lo que no se indexó:** campos sin una consulta frecuente que los use. `atributos` no tiene índice porque sus campos varían por categoría; no se consulta en la API y la consulta 4 es de análisis.

## Para la presentación: el índice en un minuto

1. **Mostrar el caso 1:** `COLLSCAN` lee 252 documentos para encontrar 1; con el índice, `IXSCAN` lee 1.
2. **Explicar ESR con el caso 2:** igualdad, después orden y rango. Por eso el plan **no tiene `SORT`**.
3. **Mostrar el caso 4 contra el 4b:** con `$elemMatch` el índice acota talle y color (8 claves); con condiciones sueltas solo el talle (86 claves). Escribir bien la consulta importa tanto como tener el índice.
4. **Cerrar con el costo:** en `productos` los índices ocupan más que los datos y cada escritura los actualiza. Se acepta porque el catálogo se lee mucho y se escribe poco.

Comando para mostrarlo en vivo (desde `tp-mongo/`):

```bash
docker compose exec mongo-catalogo sh -c 'mongosh --quiet -u "$MONGO_INITDB_ROOT_USERNAME" -p "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin catalogo --file /consultas/indices_explain.js'
```
