# Catálogo · Diseño

Decisiones, modelo de datos, API y supuestos del servicio. Viene del diseño hecho para el TP de MongoDB, sin lo que era solo de esa entrega.

- Las decisiones de este archivo (D1, D2…) son del diseño de Catálogo, **no** las D1…D13 de la consigna de FAV que tratan los ADR.
- Las secciones conservan su número original porque el código las cita (por ejemplo, "sección 14, punto 16").

---

## 2. Decisiones tomadas

| # | Decisión | Detalle |
|---|---|---|
| D1 | **El stock no está en Catálogo** | Es de Inventario (MySQL). Catálogo solo conoce el id de la variante. |
| D2 | **Despublicar en lugar de borrar** | `DELETE /v1/admin/productos/{id}` hace baja lógica (`publicado: false`), devuelve `204` y es idempotente. Los ids no se reutilizan |
| D3 | **Las promociones no se acumulan** | Si aplican varias a un producto, gana la de mayor porcentaje |
| D4 | **El precio público asume transferencia** | Si no se indica `medioPago`, vale `TRANSFERENCIA`. No se implementa ninguna pasarela de pagos |
| D5 | **Redondeo hacia abajo a 2 decimales** | Se calcula en **centavos enteros** y se trunca, para evitar errores de coma flotante (`0.29 * 100 = 28.999…`) |
| D6 | **Un producto puede venderse en el local sin estar publicado** | Publicado solo habilita la web. La consulta por lote no filtra por `publicado` |
| D8 | **Sin autenticación ni caché, por ahora** | La autenticación la hace el gateway. La caché está en la sección 15. La búsqueda y sus eventos están en ADR-006 |
| D12 | **El servidor genera el id de variante** | El administrador no lo escribe. Regla en 5.5. El generador de datos (`db/datos/generar.py`) aplica la misma regla. Los 3 ids del contrato ya existen y se cargan tal cual (sección 3, punto 11) |
| D13 | **Montos en centavos enteros (`int64`)** | `precioBase.monto` se guarda en centavos, nunca como double. La API convierte a pesos con 2 decimales hacia afuera, porque el contrato define `precio` como `number` (double) en pesos |
| D14 | **El CRUD completo va bajo `/v1/admin/productos`** | Un solo recurso para crear, obtener, actualizar, eliminar y listar. `GET /v1/productos` queda libre para la ruta pública del contrato, en la etapa FAV |
| D15 | **Códigos de error propios, con el estilo de Inventario** | `PEDIDO_INVALIDO` (400), `PRODUCTO_NO_ENCONTRADO` (404), `VARIANTE_DUPLICADA` (409), `CONFLICTO_DE_VERSION` (409), `VARIANTE_INEXISTENTE` (400 en el lote, 404 en la ficha; ver sección 7) |

| D16 | **CORS opcional, apagado por defecto** | Lo activa la variable `CORS_ORIGENES` (orígenes separados por coma). En FAV queda apagado: CORS lo maneja el gateway, y si los dos mandaran las cabeceras el navegador rechazaría la respuesta |

---

## 3. Lo que el servicio debe respetar de FAV

Está tomado del código de Inventario (rama `development`), de los ADR y del contrato. **No inventar reglas que después no vayan a ser así.**

| # | Requisito ya definido | Fuente |
|---|---|---|
| 1 | `varianteId` es un **texto de hasta 100 caracteres**. Inventario lo usa como clave del stock (`VARCHAR(100)`) y se lo manda al gimnasio como código de producto. **No cambia nunca** | Código de Inventario |
| 2 | Ventas pide a Catálogo el **precio vigente y los datos de la variante** de forma síncrona, con timeout de **1 s** y **1 reintento** | ADR-005 |
| 3 | El panel pide variantes **por lote** para combinarlas con el stock | ADR-001, decisión 3 |
| 4 | La mercadería nueva suele estar activa primero en el local | SPEC §3, RN-04 |
| 5 | Invariantes de Catálogo: un solo sistema de talles por producto, sin variantes repetidas, promociones con vigencia | ADR-001, RN-02, RN-05 |
| 6 | Cada producto lleva una **`version`** que sube con cada cambio (el indexador de búsqueda solo aplica eventos con versión mayor) | ADR-006 |
| 7 | `VarianteCreada` **hoy no existe**: Inventario da de alta una variante con un ingreso de stock. No se implementa en este trabajo; la creación de variantes queda en **un solo punto** del servicio para sumar el evento después | ADR-005, `002_datos_de_ejemplo.sql` |
| 8 | Forma del catálogo público: JSON camelCase, solo publicados, **sin stock**, precio vigente por variante, `fotoUrl` opcional | Contrato `catalogo-pedidos-v1` |
| 9 | Errores en `application/problem+json` con `codigo` estable | Contrato, mock, Inventario |
| 10 | Go 1.26, sondas `/healthz` y `/readyz`, **sin estado en memoria** (se prevén dos réplicas), variables `MONGO_*` del `.env.example` raíz | Inventario, ADR-011, ARCHITECTURE §12 |
| 11 | Los ids de las 3 variantes del contrato ya tienen stock en Inventario: `var-remera-basica-blanca-m`, `var-remera-basica-negra-l`, `var-jean-mom-celeste-38`. **Se respetan tal cual**, aunque no sigan la regla de 5.5 ("blanca" en lugar de "blanco"): un id no cambia nunca y es opaco (el talle y el color se leen de sus campos, no del id). El conjunto mínimo los carga escritos, no generados | `002_datos_de_ejemplo.sql` |

---

## 5. Diseño de la base de datos

### 5.1 Problema, actores y por qué una base documental

FAV es una tienda de ropa que hoy vende solo en el local y lleva el stock en un Excel. El sistema completo agrega la venta online. Este servicio resuelve **qué se vende y cómo se muestra**: productos, variantes (talle y color), fotos, categorías, precios y promociones, más el cálculo del **precio vigente**.

**Por qué MongoDB** (ADR-003): un producto se lee y se edita como una unidad (nombre, fotos, variantes y precio viajan juntos), los atributos pueden variar por categoría, la carga es mayormente de lectura y las escrituras tocan un solo producto, hechas por un solo administrador.

| Actor | Qué hace |
|---|---|
| Administrador | Carga y edita productos, variantes y fotos; publica y despublica; gestiona promociones |
| Comprador | Ve productos, filtra y busca |
| Servicio de Ventas | Pide precio vigente y datos de variantes |
| Panel de administración | Pide variantes por lote para combinarlas con el stock |

### 5.2 Requerimientos funcionales

| ID | Requerimiento | Origen |
|---|---|---|
| RF-01 | Alta y edición de productos con una o más variantes y fotos | HU-03, RN-01, RN-03 |
| RF-02 | Un solo sistema de talles por producto, y solo se aceptan sus valores | RN-02 |
| RF-03 | Sin dos variantes con el mismo talle y color dentro de un producto | HU-03.4 |
| RF-04 | Publicar y despublicar. Nace no publicado. No se borra | RN-04, HU-03.1 |
| RF-05 | Cada producto tiene una categoría de la lista fija | RN-17 |
| RF-06 | Promociones con vigencia obligatoria, separadas del precio base | RN-05, HU-05 |
| RF-07 | Cálculo del precio vigente | RN-05, RN-22 |
| RF-08 | Listado con filtros, orden y paginación | HU-02 |
| RF-09 | Consulta por lote de variantes | ADR-005, ADR-001 |

**Sistemas de talles (RN-02):** `LETRAS` (XS, S, M, L, XL, XXL) · `NUMERICO_PAR` (32, 34, 36, 38, 40, 42, 44, 46) · `NUMERICO_1_4` (1, 2, 3, 4) · `UNICO` (valor `U`).

**Categorías (RN-17), 13:** tops, bodys, remeras, buzos, camperas, jeans, pantalones, polleras, shorts, vestidos, conjuntos, abanicos, accesorios. Una por producto.

### 5.3 Colecciones

Son tres: `productos` (entidad principal), `categorias` y `promociones`.

#### `productos`

```json
{
  "_id": "prod-remera-basica",
  "nombre": "Remera básica",
  "descripcion": "Remera de algodón, corte recto.",
  "categoria": "remeras",
  "sistemaTalles": "LETRAS",
  "precioBase": { "monto": { "$numberLong": "1500000" }, "moneda": "ARS" },
  "publicado": true,
  "atributos": { "material": "algodón", "genero": "unisex" },
  "fotos": [
    { "url": "https://fav.example/fotos/remera-basica.jpg", "color": null },
    { "url": "https://fav.example/fotos/remera-basica-blanca-1.jpg", "color": "Blanco" }
  ],
  "variantes": [
    { "id": "var-remera-basica-blanca-m", "talle": "M", "color": "Blanco" },
    { "id": "var-remera-basica-negra-l", "talle": "L", "color": "Negro" }
  ],
  "creadoEn": { "$date": "2026-10-01T12:00:00Z" },
  "actualizadoEn": { "$date": "2026-10-20T15:30:00Z" },
  "version": 3
}
```

| Campo | Tipo | Oblig. | Nota |
|---|---|---|---|
| `_id` | string | Sí | Ver 5.5 |
| `nombre` | string | Sí | |
| `descripcion` | string | No | Opcional también en el contrato |
| `categoria` | string | Sí | **Referencia** a `categorias._id` |
| `sistemaTalles` | enum | Sí | Uno de los cuatro de RN-02 |
| `precioBase` | objeto | Sí | `monto`: entero `int64` en **centavos**, mayor a 0 (1500000 = $15.000,00, D13). El precio es **del producto**: las variantes no tienen precio propio. `moneda`: `ARS` |
| `publicado` | boolean | Sí | `false` por defecto (HU-03.1) |
| `atributos` | objeto libre | No | Puede variar por categoría. Es la parte flexible del modelo |
| `fotos` | arreglo | No | `url` obligatoria, `color` opcional (RN-03). Solo la URL. La **primera** es la principal (`fotoUrl` del contrato) |
| `variantes` | arreglo | Sí (1+) | `id`, `talle`, `color` obligatorios. **Sin stock** |
| `creadoEn`, `actualizadoEn` | fecha | Sí | "Novedad" se ordena por `creadoEn` |
| `version` | entero | Sí | Empieza en 1. **Sube con todo cambio**: editar, publicar, despublicar, agregar variante |
| `publicadoVersion` | entero | No | Última versión avisada por evento. Sin el campo cuenta como 0, así que un producto cargado por script también se avisa (ADR-006) |

#### `categorias`

```json
{ "_id": "remeras", "nombre": "Remeras" }
```

El `_id` coincide con el valor `categoria` del contrato (`remeras`, `jeans`, `buzos`).

#### `promociones`

```json
{
  "_id": "promo-verano-remeras",
  "nombre": "Verano en remeras",
  "tipo": "PORCENTAJE",
  "porcentaje": 20,
  "medioPago": null,
  "vigencia": { "desde": { "$date": "2026-12-01T00:00:00Z" }, "hasta": { "$date": "2026-12-31T23:59:59Z" } },
  "alcance": { "tipo": "CATEGORIA", "ids": ["remeras"] }
}
```

```json
{
  "_id": "promo-efectivo-tienda",
  "nombre": "10% pagando en efectivo",
  "tipo": "MEDIO_PAGO",
  "porcentaje": 10,
  "medioPago": "EFECTIVO",
  "vigencia": { "desde": { "$date": "2026-11-01T00:00:00Z" }, "hasta": { "$date": "2026-11-30T23:59:59Z" } },
  "alcance": { "tipo": "TIENDA", "ids": [] }
}
```

| Campo | Nota |
|---|---|
| `tipo` | `PORCENTAJE` o `MEDIO_PAGO` (RN-05). 2x1 y promociones bancarias quedan fuera de v1 |
| `medioPago` | Solo si `tipo` es `MEDIO_PAGO`: `EFECTIVO`, `TRANSFERENCIA` o `MERCADO_PAGO` (RN-18) |
| `vigencia` | `desde` y `hasta` **obligatorias**, con `desde < hasta` (HU-05.2) |
| `alcance` | A qué productos se aplica: `PRODUCTO`, `CATEGORIA` o `TIENDA`, con los ids correspondientes. Los tres quedan soportados porque el SPEC no fijó cuál usa el cliente |

Una promoción se cancela o se acorta editando su vigencia.

### 5.4 Embeber o referenciar

| Dato | Decisión | Por qué |
|---|---|---|
| Variantes | **Embebidas** | Se leen siempre con el producto, nacen y mueren con él y son pocas. Un documento devuelve la ficha completa, sin joins |
| Fotos, precio base, atributos | **Embebidos** | Son propiedades del producto |
| Categoría | **Referencia** | Conjunto chico y compartido por muchos productos; se valida contra `categorias`. El id es legible, así que no se duplica el nombre |
| Promociones | **Colección aparte** | Otro ciclo de vida (se manejan sin tocar el producto), se aplican a muchos productos o a una categoría entera, y embebidas habría que copiarlas en cientos de documentos |

La decisión responde a los patrones de acceso (lectura frecuente de la ficha completa y escrituras poco frecuentes sobre un solo producto), no a copiar un modelo relacional.

### 5.5 Identificadores

- `_id` y `variantes[].id` son **textos legibles**, no `ObjectId`, porque así los usan el contrato e Inventario.
- Hasta 100 caracteres, porque Inventario lo exige (`VARCHAR(100)`). Si el id generado pasa el límite, se rechaza con `400`. **No cambian nunca**, ni al renombrar el producto ni al corregir el color de una variante.
- Los genera el **servidor**, en una sola función, con la misma regla que el generador de datos (D12):
  - Producto: `prod-` + nombre normalizado. Si el id ya existe, se agrega un sufijo numérico.
  - Variante: `var-` + producto (sin el `prod-`) + color + talle, todo normalizado. Por ejemplo, `prod-remera-basica` en Blanco, talle M, da `var-remera-basica-blanco-m`.
  - Normalizar: minúsculas, sin acentos, sin espacios. Cómo se reemplazan los espacios y la ñ está en la sección 14.
- Los 3 ids del contrato son **heredados** y no siguen esta regla. Se cargan tal cual (sección 3, punto 11).
- Como no se borra, un id **no se reutiliza**: una variante recreada con el mismo id heredaría el stock de la anterior en Inventario.

### 5.6 Validaciones

| Regla | Fuente | Dónde |
|---|---|---|
| Talle perteneciente al `sistemaTalles` del producto | RN-02, HU-03.2 | Servicio (y esquema como extensión) |
| Al menos una variante | RN-01 | Servicio |
| Sin talle y color repetidos (en talle único, es la misma regla) | HU-03.3 y 03.4 | **Operación atómica** (5.9) |
| Categoría existente | RN-17 | Servicio |
| Producto nuevo con `publicado: false` | HU-03.1 | Servicio |
| Promoción con `desde` y `hasta`, y `desde < hasta` | HU-05.2 | Servicio (y esquema) |
| `MEDIO_PAGO` exige `medioPago` | RN-05 | Servicio |

### 5.7 Precio vigente

Lo definido (SPEC, RN-05 y HU-05): una promoción fuera de su vigencia no se aplica; una de medio de pago solo se aplica a ventas con ese medio; valen online y en el local; el precio lo calcula el servidor.

Algoritmo, en una **sola función aislada** para poder cambiar la política sin tocar el modelo ni la API:

1. `medioPago` = el recibido, o `TRANSFERENCIA` si no vino (D4).
2. Candidatas: promociones con fecha actual dentro de la vigencia, cuyo `alcance` incluya al producto (por id, por categoría o `TIENDA`), y que sean `PORCENTAJE` o `MEDIO_PAGO` con ese medio.
3. Si hay más de una, **gana la de mayor porcentaje** (no se acumulan, D3).
4. `precioFinalCentavos = trunc(precioBaseCentavos × (100 − porcentaje) / 100)`, en enteros, es decir, hacia abajo y sin coma flotante (D5). Recién al responder se pasa a pesos con 2 decimales (D13).
5. Sin candidatas, el precio vigente es el precio base.

La respuesta indica el precio base, el vigente y la promoción aplicada (si la hay). Si empatan dos promociones, gana la de menor id. Implementado en `internal/service/precio.go` (`PrecioVigente`), con sus casos en `precio_test.go`.

### 5.8 Índices

Todos se crean en `db/scripts/01_crear_colecciones_e_indices.js`. Además, el servicio crea al arrancar **solo `variantes_id_unico`**, porque de él depende una regla (que un id de variante no se repita) y no solo el rendimiento: así la regla vale aunque nadie haya corrido el cargador. Es idempotente, así que dos réplicas pueden arrancar a la vez. Tiene que tener el mismo nombre y la misma definición en los dos lugares.

| Consulta | Índice |
|---|---|
| **Consulta por lote de variantes** (Ventas, timeout 1 s; panel) | `{ "variantes.id": 1 }`, **único** (multikey). También garantiza que un id de variante sea único en el catálogo |
| Listar por categoría y rango de precio, con orden | `{ publicado: 1, categoria: 1, "precioBase.monto": 1 }` |
| Buscar por texto en nombre y descripción | Índice de texto `{ nombre: "text", descripcion: "text" }`, idioma español |
| Filtrar por talle y color | `{ "variantes.talle": 1, "variantes.color": 1 }` (multikey) |
| Promociones vigentes | `{ "vigencia.desde": 1, "vigencia.hasta": 1 }` |
| Promociones por alcance | `{ "alcance.tipo": 1, "alcance.ids": 1 }` |

**Para el informe:** cada índice se acompaña de un `explain("executionStats")` antes y después, con lo que se observa (`COLLSCAN` vs `IXSCAN`, documentos examinados vs devueltos). Con pocos datos no se exige mostrar mejoras de tiempo: se evalúa la pertinencia y la interpretación. Se explica también el **costo**: espacio y trabajo extra en cada escritura (aceptable con un solo administrador).

### 5.9 Consistencia y concurrencia

**Caso principal: agregar una variante sin duplicarla.**

- *Qué podría pasar sin protección:* dos pedidos simultáneos (dos pestañas del panel, o un reintento por red) agregan la misma variante `S` / `Blanco`. Cada uno lee el producto, no ve duplicados y agrega la suya: el producto queda con dos variantes iguales, con ids distintos. Inventario recibiría las dos y repartiría el stock entre ambas.
- *Un índice único no alcanza:* en MongoDB, un índice único sobre un arreglo evita repetidos **entre documentos**, no dentro del mismo arreglo. El índice protege la unicidad del id en todo el catálogo; la actualización condicional protege talle y color dentro del producto.
- *Solución, una operación atómica sobre un solo documento:*

```js
db.productos.updateOne(
  { _id: idProducto,
    variantes: { $not: { $elemMatch: { talle: "S", color: "Blanco" } } },
    "variantes.id": { $ne: "var-remera-basica-blanco-s" } },   // "Blanco" y "blanco" dan el mismo id
  { $push: { variantes: { id: "var-remera-basica-blanco-s", talle: "S", color: "Blanco" } },
    $set: { actualizadoEn: new Date() },
    $inc: { version: 1 } }
)
```

  Si `matchedCount` es 0, el servicio distingue si el producto no existe (`404`) o si la variante ya estaba (`409`).
- *Prueba:* 20 pedidos simultáneos con el mismo talle y color. Debe haber **exactamente un `201`** y diecinueve `409`, y el producto debe quedar con una sola variante. Se guarda como evidencia.

**Caso secundario (opcional): edición concurrente.** El pedido de edición lleva la `version` que leyó y el filtro la incluye (`{ _id, version }`). Si otro cambio se coló, no hay coincidencia y se responde `409`, evitando pisar cambios.

**Por qué no una transacción multidocumento:** cada operación crítica modifica un solo documento y MongoDB garantiza que es atómica (ADR-003 llega a la misma conclusión). Las transacciones requieren replica set y no hacen falta acá.

---

## 7. API

Errores en `application/problem+json` con `codigo` estable, como el contrato de FAV e Inventario. Código HTTP adecuado en cada caso: `400` validación, `404` inexistente, `409` conflicto, `204` baja.

| `codigo` | HTTP | Cuándo |
|---|---|---|
| `PEDIDO_INVALIDO` | 400 | El cuerpo o los parámetros no cumplen las reglas. `errores` indica los campos |
| `PRODUCTO_NO_ENCONTRADO` | 404 | El producto no existe |
| `VARIANTE_INEXISTENTE` | **400** en el lote · **404** en la ficha | Ver la nota debajo. `variantes` indica cuáles |
| `VARIANTE_DUPLICADA` | 409 | El producto ya tiene una variante con ese talle y color (5.9) |
| `CONFLICTO_DE_VERSION` | 409 | La `version` enviada no es la actual (5.9, caso secundario) |
| `PROMOCION_NO_ENCONTRADA` | 404 | La promoción no existe |
| `ERROR_INTERNO` | 500 | Error inesperado. El detalle va al log, no al cliente |

**Nota (diferencia a propósito):** en la consulta por lote, una variante inexistente da `400`, como pide el contrato `catalogo-pedidos-v1`, porque eso es lo que va a consumir Ventas. En la ficha individual da `404`, como Inventario.

Los precios se devuelven en **pesos con 2 decimales** (`number`), como el contrato. Internamente se manejan en centavos (D13).

| Método y ruta | Función |
|---|---|
| `POST /v1/admin/productos` | Crear (nace no publicado, con al menos una variante) |
| `GET /v1/admin/productos/{id}` | Obtener por id (`404` si no existe) |
| `PUT /v1/admin/productos/{id}` | Actualizar datos del producto (lleva `version`) |
| `DELETE /v1/admin/productos/{id}` | **Baja lógica**: despublica. `204` e idempotente |
| `POST /v1/admin/productos/{id}/publicar` | Publicar |
| `GET /v1/admin/productos` | Listar con filtros: `categoria`, `talle`, `color`, `precioMin`, `precioMax`, `q`, `publicado`, `orden`, `pagina`, `tamano`. Orden siempre desempatado por `_id` para que la paginación no repita ni saltee resultados |
| `POST /v1/admin/productos/{id}/variantes` | Agregar una variante (operación atómica de 5.9) |
| `GET /v1/variantes?variantes=a,b,c` | **Consulta por lote** (Ventas y panel): de 1 a 200 ids separados por coma, y la respuesta va en `{"items": [...]}`, igual que `GET /v1/stock?variantes=…` de Inventario. Por variante devuelve `productoId`, `nombre`, `categoria`, `talle`, `color`, `publicado` y precio vigente (`medioPago` opcional, por defecto `TRANSFERENCIA`). No filtra por `publicado` |
| `POST /v1/promociones`, `PUT /v1/promociones/{id}`, `GET /v1/promociones` | Gestionar promociones. El servidor genera el id (`promo-` + nombre normalizado). No hay `DELETE`: se cancela editando la vigencia (5.3) |
| `GET /v1/tienda/productos`, `GET /v1/tienda/productos/{id}` | Lectura pública del comprador (HU-01, HU-02), en `handler/tienda.go`. Solo publicados, con los filtros del listado salvo `publicado`, que da `400`. El no publicado da `404`. Trae todas las fotos, `precio` (vigente para transferencia, D4), `precioBase` y `promocion`. Sin stock (D1) |
| `GET /healthz`, `GET /readyz` | Sondas. La segunda verifica la conexión a MongoDB |

- El listado filtra y ordena por **precio base**; el vigente se calcula al consultar la ficha o el lote (etapa FAV).
- `GET /v1/productos` es la ruta pública del contrato (con `items`, solo publicados y sin paginar), en un adaptador aparte (`handler/publico.go`, D14). El precio es el vigente para transferencia (D4). Se apaga con `CAPACIDAD_PUBLICA_ACTIVA=false` (ADR-008). Por eso todo el CRUD de administración está bajo `/v1/admin/productos`.
- Estructura en capas: `handler` (HTTP y validación de entrada) → `service` (reglas y precio vigente) → `repository` (único lugar que habla con MongoDB). Sin estado en memoria.

---

## 8. Datos de ejemplo

| Conjunto | Contenido | Para qué |
|---|---|---|
| **Mínimo** (por defecto) | 13 categorías y los 2 productos del contrato, con los ids de la sección 3, punto 11 | Lo que usa FAV: esas 3 variantes tienen stock en Inventario |
| **Completo** | Además, 250 productos (881 variantes en total) y 14 promociones | Pruebas de carga y de consultas. Sus variantes **no existen en Inventario**: una compra sobre ellas falla |

- Los archivos están en `db/datos/`. `generar.py` los regenera de forma determinista (semilla fija).
- **Carga:** el servicio `catalogo-init` del Compose corre al levantar. Aplica siempre `db/scripts/01_…` (colecciones, esquema e índices; idempotente) y carga los datos **solo si `productos` está vacía**, así nunca borra lo que cargó el administrador. Variables: `CATALOGO_CARGA` (`minimo` o `completa`) y `CATALOGO_RECARGAR=true`, que vacía las colecciones y vuelve a cargar.
- `db/scripts/03_verificar.js` comprueba que toda categoría referenciada exista, que los ids de variante sean únicos, que cada talle pertenezca al sistema del producto y que ninguna promoción tenga la vigencia invertida.
- Las promociones del conjunto completo tienen fechas fijas de noviembre y diciembre de 2026: con el tiempo quedan vencidas.

---

## 13. Convenciones de código

- **Go 1.26.** Módulo como el de Inventario: `github.com/JuanHernandez-99/Fav-del-universo/services/catalogo`. Nombres del dominio en español, como en Inventario.
- **Driver oficial** de MongoDB para Go (`go.mongodb.org/mongo-driver/v2`).
- **Capas:** `handler` no conoce a Mongo, `service` no conoce a HTTP y `repository` es el único que importa el driver. El repositorio se define con una **interfaz**, así la búsqueda y la caché se pueden enchufar después sin tocar el servicio.
- **Sin estado en memoria** y con **cierre ordenado** (`SIGTERM`), para correr en varias réplicas.
- **Logs** en JSON con `slog`, con `servicio` e `instancia` en cada línea. Sondas sin loguear.
- **Errores** en `application/problem+json` con `codigo` estable.
- **Operaciones críticas dentro de la base:** la condición va dentro del filtro de la actualización, nunca "leer, decidir y escribir".
- **Tests:** unitarios de ids, montos, filtros, CORS y talles del esquema; de concurrencia sobre la variante repetida. Quedan para la etapa FAV los unitarios del precio vigente y los de integración del repositorio contra un Mongo real.
- **Secretos:** nunca en el repositorio. `.env` va en `.gitignore`; solo se entrega `.env.example`.
- **Commits** chicos y frecuentes, en `services-catalogo`.

---

## 14. Decisiones menores y supuestos

| # | Tema | Decisión |
|---|---|---|
| 1 | Alcance de las promociones | Los tres soportados: `PRODUCTO`, `CATEGORIA` y `TIENDA` |
| 2 | Si hay dos promociones aplicables, cuál gana | La de mayor porcentaje (D3) |
| 3 | Valor del talle en el sistema "talle único" | `U` |
| 4 | Categorías por producto (RN-17) | Una sola, guardada como string |
| 5 | Cuál es la foto principal | La primera del arreglo (`fotoUrl` del contrato) |
| 6 | Dónde se guardan las fotos | Solo la URL |
| 7 | El listado filtra y ordena por precio base o vigente | Precio base. El vigente se calcula en la etapa FAV (sección 15) |
| 8 | Contenido del evento `VarianteCreada` | Fuera de este trabajo (sección 15) |
| 9 | Ids de variante y rutas de Inventario | Verificados el 02/11/2026 contra `services/inventario`: los tres ids del contrato existen en `002_datos_de_ejemplo.sql` y la consulta por lote de Inventario es `GET /v1/stock?variantes=…` |
| 10 | Índices del servicio y del script `01_…` | El servicio crea solo `variantes_id_unico`, igual que el script (5.8). Con la base vacía lo crea la API y después `01` no choca |
| 11 | Cómo se normalizan los espacios y la ñ en los ids (5.5) | Espacio → `-` (como `remera-basica`), ñ → `n`. Cualquier otro símbolo pasa a `-` y se quitan los `-` repetidos o de los extremos (`normalizar` en `datos/generar.py`; la función de Go da lo mismo y un test lo comprueba) |
| 12 | Dos colores que se normalizan igual ("Azul marino" y "azul-marino") | Generarían el mismo id: el índice único lo rechaza y se responde `409 VARIANTE_DUPLICADA` |
| 13 | Variante inexistente | `400` en el lote y `404` en la ficha, a propósito (sección 7) |
| 14 | Zona horaria de las vigencias | Se definen en hora de Argentina (UTC−3) y se guardan en UTC: "hasta el 30/11" es `2026-12-01T02:59:59Z` |
| 15 | Unidad del monto al **recibir** (`POST`/`PUT`) | En pesos, con hasta 2 decimales (`15999.90`). Más decimales da `400`. Se convierte a centavos redondeando |
| 16 | Talles escritos en minúscula (`"m"`) | Se pasan a mayúscula antes de validar. El color se guarda como lo escribió el administrador, sin espacios en los extremos y sin lista cerrada de colores |
| 17 | Qué cambia el `PUT` | Reemplaza los datos editables: nombre, descripción, categoría, precio, atributos y fotos. Un opcional que no viene se quita. **No** cambia `sistemaTalles` (dejaría inválidos los talles de las variantes), ni las variantes, ni `publicado`, ni el id. Lleva `version` obligatoria |
| 18 | Publicar o despublicar algo que ya estaba así | No es error y no sube la `version`: `/publicar` responde `200` con el producto y `DELETE` responde `204`. Sobre un id inexistente, los dos dan `404` |
| 19 | Cómo se filtra el listado | `categoria`, `talle` y `color` son coincidencia exacta: el color distingue mayúsculas (`Celeste`, no `celeste`) y el talle se pasa a mayúscula. Con `talle` y `color` juntos se exige que sean **la misma variante** (`$elemMatch`). `q` busca con el índice de texto en español. `precioMin` y `precioMax` van en pesos y se comparan con el precio base (punto 7). Un parámetro desconocido da `400`, para que un error de tipeo no devuelva todo el catálogo sin avisar. Respuesta: `items`, `pagina`, `tamano`, `total` y `paginas`; `tamano` por defecto 20 y máximo 100. Órdenes: `nuevos` (por defecto), `precio_asc`, `precio_desc` y `nombre`, siempre desempatados por `_id` |
| 20 | Producto con un nombre que ya existe | Se acepta: el id lleva un sufijo numérico (`-2`, `-3`…), según 5.5 |
| 21 | Ids del alcance de una promoción | Tienen que existir: con un error de tipeo la promoción no se aplicaría a nada, sin avisar. **[propuesta]** |
| 22 | Edición concurrente de una promoción | Gana la última: no lleva `version`, porque hay un solo administrador (RN-23). **[propuesta]** |

Los puntos 1, 5, 6 y 8 dependen de lo que defina el equipo de FAV. Se resolvieron así y se confirman con el equipo.

---

## 15. Pendientes

Sin cambiar lo anterior:

- **Evento `VarianteCreada`** con outbox (ni se publica ni se consume hoy). Si se guarda junto con el producto en una transacción, requiere replica set.
- **Caché** con Redis (ADR-007), como capa delante del repositorio.
- **Observabilidad**: correlation id, trazas y métricas (ADR-011).
- **Identidad** con los encabezados `X-User-*`.
- **Tests de contrato** contra el YAML publicado.
- El **stock sigue siendo de Inventario**.

---
