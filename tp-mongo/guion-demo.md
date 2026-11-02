# Guion de la demo (unos 10 minutos)

Todos los comandos se corren desde `tp-mongo/`, en PowerShell o Git Bash.

## Antes de empezar (5 minutos antes)

- Docker Desktop abierto y las imágenes ya descargadas.
- Stack limpio: `docker compose down -v`, `docker compose up -d --build` y `docker compose run --rm cargador` (tiene que terminar con `verificación OK`).
- Abiertos y listos: **Swagger** (`localhost:8086`), **Compass** conectado a `catalogo` → `productos`, `analisis/catalogo.html` y una terminal.
- **Respaldo:** una captura de cada paso, por si Docker falla.

## Recorrido

### 1. El problema y el modelo (1:30)
**Compass** → `productos` → abrir `prod-remera-basica`.

> "FAV es una tienda de ropa. El catálogo resuelve qué se vende y cómo se muestra. Elegimos MongoDB porque un producto se lee y se edita completo: las **variantes** y las **fotos** van **embebidas** en el mismo documento, y `atributos` es libre porque cambia según la categoría. La **categoría** es una **referencia**, porque la comparten muchos productos. Las **promociones** son otra colección: tienen otro ciclo de vida y se aplican a muchos productos."

Si preguntan por el stock: no está acá, es del servicio de Inventario (MySQL). Respuesta oral en `PROYECTO.md`, sección 12.

### 2. Consultas (2:00)
**Compass** → Documents → filtro: `{ variantes: { $elemMatch: { talle: "M", color: "Negro" } } }` → 8 resultados.

> "`$elemMatch` exige que talle y color sean de la misma variante. Con condiciones sueltas salen 9: uno tiene una M de otro color y una negra de otro talle."

**Terminal** (precio vigente con `$lookup`, consulta 7, y agrupamiento, consulta 9):

```bash
docker compose exec mongo-catalogo sh -c 'mongosh --quiet -u "$MONGO_INITDB_ROOT_USERNAME" -p "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin catalogo --file /consultas/consultas.js'
```

Mostrar la **7b**: el jean con dos promociones (20 % y 30 %) gana la mayor, de $45.490 a $31.843 (Jean wide leg elastizado). Y la **7c**: la remera básica cuesta $15.000 por transferencia y $13.500 en efectivo.

### 3. Índices (1:30)
**Compass** → pestaña **Indexes** (5 índices) y **Explain Plan** con el filtro anterior.

> "Sin índice, Mongo lee los 252 documentos (`COLLSCAN`). Con `variantes_talle_color` busca en el índice (`IXSCAN`) y lee solo los que cumplen. Cuesta espacio y trabajo en cada escritura, que es aceptable porque hay un solo administrador y mucha lectura."

Detalle de las mediciones: `informe/03-indices.md`.

### 4. Validación de esquema (0:30)
**Compass** → pestaña **Validation**.

> "La base rechaza lo inválido aunque no pase por la API: precio entero y mayor a 0, talle de la lista, al menos una variante."

Prueba: `evidencia/validacion_esquema.txt` (siete inserciones inválidas rechazadas).

### 5. La API en Swagger (2:30)
`http://localhost:8086`

1. `GET /v1/admin/productos`: 20 de 252; probar `categoria=remeras`.
2. `POST /v1/admin/productos` con el ejemplo `valido`: **201**, nace con `publicado: false`. Anotar el `productoId`.
3. `POST /v1/admin/productos/{id}/variantes` con `{"talle":"S","color":"Blanco"}`: **201**.
4. Repetir el mismo pedido: **409 `VARIANTE_DUPLICADA`**.
5. `POST` con el ejemplo `invalido`: **400** con la lista de campos.
6. `DELETE`: **204**. Repetirlo: **204** (baja lógica e idempotente; el producto sigue en la base).
7. `GET` de un id inexistente: **404**.

### 6. Consistencia (1:00)
```bash
python pruebas/concurrencia_variante.py
```

> "Veinte pedidos simultáneos intentan agregar la misma variante. Si leyéramos y después escribiéramos, entrarían varias. Como la condición va **dentro** del `updateOne`, que MongoDB aplica de forma atómica sobre un documento, hay exactamente un `201` y diecinueve `409`."

Resultado esperado: `RESULTADO: OK`. Detalle: `informe/04-consistencia.md`.

### 7. Análisis (1:00)
Abrir `analisis/catalogo.html`: pasar por las pestañas **Precio**, **Talles y colores** y **Conclusión**.

> "Cuatro indicadores sacados de la base. La conclusión recomienda sumar una promoción de verano en tops, shorts y polleras, y ampliar la variedad de camperas."

### 8. Cierre (0:30)
> "Dejamos para la etapa de FAV el precio vigente en el servicio, la consulta por lote y las promociones por API. El modelo y los datos ya lo contemplan."

Si preguntan por FAV: se muestra desde la PC con el Compose raíz.

## Si algo falla

| Problema | Qué hacer |
|---|---|
| Docker no levanta | Usar las capturas y mostrar los archivos de `evidencia/` |
| Compass no conecta | Correr `consultas.js` por terminal (paso 2) |
| Swagger da `Failed to fetch` | Abrirlo en `http://localhost:8086`, no en otra dirección |
| Los números no coinciden | Se crearon productos de prueba: `docker compose run --rm cargador` vuelve a los 252 |

## Preguntas que pueden hacer

| Pregunta | Dónde está la respuesta |
|---|---|
| ¿Por qué embeber las variantes? | `informe/01-diseno.md`, sección 5 |
| ¿Por qué no una transacción? | `informe/04-consistencia.md` |
| ¿Qué hace el repositorio? | `services/catalogo/internal/repository/mongo.go`: único que importa el driver |
| ¿Por qué un índice único no alcanza para las variantes? | Evita repetidos entre documentos, no dentro del mismo arreglo (`04-consistencia.md`) |
| ¿Qué es la `version`? | Control optimista: un `PUT` con una versión vieja da `409` |
| ¿Por qué baja lógica? | Un id de variante no se puede reutilizar: heredaría el stock de la anterior en Inventario |
