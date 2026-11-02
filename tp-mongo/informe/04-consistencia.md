# Consistencia y concurrencia

> Consigna 5.6. Se identifica una operación de negocio donde la consistencia importa, se explica qué pasaría si falla a medias y se resuelve con una **operación atómica sobre un solo documento**, con una prueba que lo demuestra.

## Concepto: atomicidad en MongoDB

MongoDB garantiza que **una escritura sobre un solo documento es atómica**: se aplica entera o no se aplica, y ningún otro pedido ve un estado a medias. Eso incluye cambiar varios campos y agregar elementos a un arreglo dentro del mismo documento. Las **transacciones multidocumento** son para cuando hay que modificar *varios* documentos a la vez, y exigen un *replica set* (no funcionan en una instancia suelta, como la del Compose de este TP).

Por eso el diseño importa: como las variantes están **embebidas** en el producto (`01-diseno.md`, sección 5), agregar una variante modifica un solo documento y alcanza con una operación atómica.

## El caso: agregar una variante sin duplicarla

Un producto no puede tener dos variantes con el mismo talle y color (RF-03). Si las tuviera, Inventario recibiría las dos y repartiría el stock entre ambas.

**Qué podría pasar sin protección.** Dos pedidos simultáneos (dos pestañas del panel, o un reintento por falla de red) agregan la variante `S` / `Blanco`:

| Paso | Pedido A | Pedido B |
|---|---|---|
| 1 | Lee el producto: no tiene `S` / `Blanco` | Lee el producto: no tiene `S` / `Blanco` |
| 2 | Agrega la variante | Agrega la variante |
| Resultado | El producto queda con **dos variantes iguales** y dos ids distintos | |

El error está en el patrón "leer, decidir y escribir": entre la lectura y la escritura otro pedido puede cambiar el documento.

**Un índice único no alcanza.** En MongoDB un índice único sobre un arreglo evita repetidos **entre documentos**, no dentro del mismo arreglo. El índice `variantes_id_unico` protege que un id de variante no se repita en todo el catálogo; falta proteger el talle y el color **dentro del producto**.

## La solución

La condición pasa **dentro del filtro** de la escritura, así MongoDB la evalúa y aplica el cambio en un solo paso atómico (`repository/mongo.go`, `AgregarVariante`):

```js
db.productos.updateOne(
  { _id: idProducto,
    variantes: { $not: { $elemMatch: { talle: "S", color: "Blanco" } } },  // no tiene esa variante
    "variantes.id": { $ne: "var-remera-basica-blanco-s" } },               // ni ese id ("Blanco" y "blanco" dan el mismo id)
  { $push: { variantes: { id: "var-remera-basica-blanco-s", talle: "S", color: "Blanco" } },
    $set: { actualizadoEn: new Date() },
    $inc: { version: 1 } }
)
```

Si hay 20 pedidos iguales, solo el primero encuentra el documento sin la variante. Para los otros 19 el filtro ya no coincide y `matchedCount` es 0. Recién ahí el servicio hace una segunda consulta para distinguir **producto inexistente** (`404 PRODUCTO_NO_ENCONTRADO`) de **variante repetida** (`409 VARIANTE_DUPLICADA`).

| Resultado de la operación | Respuesta |
|---|---|
| Se agregó (coincidió el filtro) | `201` |
| No coincidió y el producto no existe | `404` |
| No coincidió y el producto existe | `409` |
| El id ya lo usa otro producto (índice único) | `409` |

Con la misma operación, la `version` sube **una sola vez** y `actualizadoEn` cambia junto con la variante, todo o nada.

**Por qué no una transacción.** La operación crítica toca un solo documento, que MongoDB ya garantiza que es atómico. Una transacción exigiría un replica set y no agregaría nada acá.

## Prueba

`pruebas/concurrencia_variante.py` crea un producto y lanza **20 pedidos simultáneos** con el mismo talle y color. Resultado guardado en `evidencia/concurrencia_variante.txt`:

| Se esperaba | Se obtuvo |
|---|---|
| Exactamente un `201` | `201`: 1 |
| Diecinueve `409` con `VARIANTE_DUPLICADA` | `409`: 19 |
| Una sola variante `S` / `Blanco` en el producto | Sí |
| `version` subida una sola vez (1 a 2) | Sí |

## Caso secundario: edición concurrente

Si dos administradores editan el mismo producto, el segundo podría pisar los cambios del primero. El `PUT` lleva la `version` que se leyó y el filtro la incluye (`{ _id, version }`). Si otro cambio se coló, la versión guardada ya es otra, el filtro no coincide y se responde `409 CONFLICTO_DE_VERSION` sin pisar nada. Es un **control optimista**: no bloquea a nadie, solo detecta el conflicto al escribir. La misma `version` sirve a FAV para que el indexador de búsqueda ignore eventos viejos.

**Se muestra con:** `python pruebas/concurrencia_variante.py` (con la API levantada). Para el caso secundario, en Swagger: leer un producto, hacer un `PUT` con su `version` (200) y repetir el mismo `PUT` (409).
