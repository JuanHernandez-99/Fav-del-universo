# Diseño de la base de datos

> Consigna 5.1. Dominio aprobado: **catálogo y gestión de productos** de FAV, una tienda de ropa. Los detalles de cada decisión están en `PROYECTO.md` (se cita su número: D1, D2…).

## 1. Problema y actores

FAV vende hoy solo en el local y lleva el stock en un Excel. El sistema completo suma la venta online. Este servicio resuelve **qué se vende y cómo se muestra**: productos con variantes (talle y color), fotos, categorías, precios y promociones.

| Actor | Qué hace con el catálogo |
|---|---|
| Administrador | Carga y edita productos y variantes; publica y despublica; gestiona promociones |
| Comprador | Ve productos, filtra y busca |
| Ventas / panel de administración | Piden precio y datos de variantes (etapa FAV) |

## 2. Por qué una base documental

MongoDB guarda **documentos** (JSON/BSON): objetos con campos anidados y arreglos, sin un esquema fijo para todos. Conviene cuando los datos se leen y escriben como una unidad. Acá pasa eso:

- Un producto se muestra y se edita completo: nombre, fotos, variantes y precio viajan juntos. En un modelo relacional serían 4 tablas y varios `JOIN`; acá es **un documento, una lectura**.
- Los atributos cambian según la categoría (una remera tiene `material`; un abanico, otra cosa). El campo `atributos` es libre y no obliga a columnas vacías.
- La carga es sobre todo de lectura. Las escrituras tocan **un solo producto** y las hace un único administrador.

## 3. Requerimientos funcionales

| ID | Requerimiento |
|---|---|
| RF-01 | Alta y edición de productos con una o más variantes y fotos |
| RF-02 | Un solo sistema de talles por producto, y solo se aceptan sus valores |
| RF-03 | Sin dos variantes con el mismo talle y color dentro de un producto |
| RF-04 | Publicar y despublicar; el producto nace no publicado y no se borra |
| RF-05 | Cada producto tiene una categoría de una lista fija (13) |
| RF-06 | Promociones con vigencia obligatoria, separadas del precio base |
| RF-08 | Listado con filtros, orden y paginación |

Quedan para la etapa FAV el cálculo del precio vigente (RF-07) y la consulta por lote (RF-09); el modelo ya los contempla (ver `06-conclusiones.md`).

## 4. Colecciones

Son tres: `productos` (entidad principal con el CRUD), `categorias` y `promociones`.

### `productos`

```json
{
  "_id": "prod-remera-basica",
  "nombre": "Remera básica",
  "descripcion": "Remera de algodón, corte recto.",
  "categoria": "remeras",
  "sistemaTalles": "LETRAS",
  "precioBase": { "monto": 1500000, "moneda": "ARS" },
  "publicado": true,
  "atributos": { "material": "algodón", "genero": "unisex" },
  "fotos": [ { "url": "https://fav.example/fotos/remera-basica.jpg", "color": null } ],
  "variantes": [
    { "id": "var-remera-basica-blanca-m", "talle": "M", "color": "Blanco" },
    { "id": "var-remera-basica-negra-l", "talle": "L", "color": "Negro" }
  ],
  "creadoEn": "2026-10-01T12:00:00Z",
  "actualizadoEn": "2026-10-20T15:30:00Z",
  "version": 3
}
```

| Campo | Tipo | Oblig. | Nota |
|---|---|---|---|
| `_id` | string | Sí | Texto legible generado por el servidor (sección 6) |
| `nombre` | string | Sí | |
| `descripcion` | string | No | |
| `categoria` | string | Sí | **Referencia** a `categorias._id` |
| `sistemaTalles` | enum | Sí | `LETRAS`, `NUMERICO_PAR`, `NUMERICO_1_4` o `UNICO` |
| `precioBase` | objeto | Sí | `monto` en **centavos enteros** (`int64`), mayor a 0; `moneda` `ARS` |
| `publicado` | boolean | Sí | `false` al crear |
| `atributos` | objeto libre | No | La parte flexible del modelo |
| `fotos` | arreglo de objetos | No | `url` obligatoria, `color` opcional (null si es general) |
| `variantes` | arreglo de objetos | Sí (1 o más) | `id`, `talle` y `color` obligatorios |
| `creadoEn`, `actualizadoEn` | fecha | Sí | "Novedad" se ordena por `creadoEn` |
| `version` | entero | Sí | Empieza en 1 y sube con cada cambio |

### `categorias` (13) y `promociones` (14)

```json
{ "_id": "remeras", "nombre": "Remeras" }
```

```json
{
  "_id": "promo-efectivo-tienda",
  "nombre": "10% pagando en efectivo",
  "tipo": "MEDIO_PAGO",
  "porcentaje": 10,
  "medioPago": "EFECTIVO",
  "vigencia": { "desde": "2026-11-01T00:00:00Z", "hasta": "2026-11-30T23:59:59Z" },
  "alcance": { "tipo": "TIENDA", "ids": [] }
}
```

| Campo de la promoción | Nota |
|---|---|
| `tipo` | `PORCENTAJE` o `MEDIO_PAGO` |
| `medioPago` | Solo si el tipo es `MEDIO_PAGO` (`EFECTIVO`, `TRANSFERENCIA`, `MERCADO_PAGO`); si no, `null` |
| `vigencia` | `desde` y `hasta` obligatorias, con `desde < hasta` |
| `alcance` | A qué se aplica: `PRODUCTO`, `CATEGORIA` o `TIENDA`, con los ids correspondientes |

## 5. Embeber o referenciar

**Concepto.** En MongoDB no hay `JOIN` obligatorio: un dato se **embebe** (va dentro del documento) o se **referencia** (se guarda su id y se busca aparte). Embeber da lecturas rápidas y escrituras atómicas sobre todo el documento; referenciar evita copiar datos que cambian o se comparten. La regla es decidir por **cómo se accede**, no por cómo se vería en tablas.

| Dato | Decisión | Por qué |
|---|---|---|
| Variantes | **Embebidas** | Se leen siempre con el producto, nacen y mueren con él y son pocas por producto. Una ficha completa sale de un solo documento |
| Fotos, precio base, atributos | **Embebidos** | Son propiedades del producto |
| Categoría | **Referencia** | Conjunto chico y compartido por muchos productos; se valida contra `categorias`. El id es legible (`remeras`), así que no hace falta copiar el nombre |
| Promociones | **Colección aparte** | Tienen otro ciclo de vida (se gestionan sin tocar el producto) y se aplican a muchos productos o a una categoría entera: embebidas habría que copiarlas en cientos de documentos |

Embeber las variantes también habilita la consistencia del caso principal: agregar una variante modifica **un solo documento**, y MongoDB garantiza que eso es atómico (ver `04-consistencia.md`).

## 6. Decisiones de modelado

| Tema | Decisión | Por qué |
|---|---|---|
| **Ids** (D12) | Textos legibles generados por el servidor: `prod-remera-basica`, `var-remera-basica-blanco-m`. Hasta 100 caracteres y no cambian nunca | MongoDB usa `ObjectId` por defecto, pero el contrato de FAV e Inventario usan estos textos (Inventario los guarda en `VARCHAR(100)`). Si el id ya existe se agrega un sufijo numérico |
| **Precio en centavos** (D13) | `monto` es un entero (`1500000` = $15.000,00), nunca un decimal | La coma flotante no es exacta (`0.29 * 100 = 28.999…`). Con enteros, los cálculos de porcentajes no acumulan errores. La API convierte a pesos al responder |
| **Baja lógica** (D2) | `DELETE` pone `publicado: false` y devuelve `204`; no borra el documento | Un id de variante no se puede reutilizar: si se recreara, heredaría el stock de la anterior en Inventario. Además es idempotente (repetirlo no cambia nada) |
| **Sin stock** (D1) | El catálogo solo conoce el id de la variante; el stock es del servicio Inventario (MySQL) | Catálogo se lee mucho y tolera datos levemente atrasados; el stock **no puede venderse de más** y una compra toca varias variantes, algo que SQL resuelve de forma más directa. En MongoDB sería posible (campo `stock` dentro de cada variante y un `updateOne` condicional, o una transacción si la compra abarca varios productos), pero cada servicio usa la base que le corresponde |
| **Versión** | `version` sube con cada cambio | Permite detectar ediciones concurrentes (`409`) y es lo que el indexador de búsqueda de FAV usará para ignorar eventos viejos |
| **Fechas** | Se guardan en UTC; las vigencias se definen en hora de Argentina (UTC−3) | "Hasta el 30/11" es `2026-12-01T02:59:59Z` |
| **Color** | Texto libre, sin lista cerrada | No hay una regla de negocio que lo limite. El filtro distingue mayúsculas (`Negro`, no `negro`) |
| **Una categoría por producto** | Un string, no un arreglo | Así lo define la regla de negocio de FAV |

## 7. Validaciones

| Regla | Dónde se valida |
|---|---|
| El talle pertenece al `sistemaTalles` del producto | Servicio |
| Al menos una variante | Servicio |
| Sin talle y color repetidos en un producto | **Operación atómica en Mongo** (`04-consistencia.md`) |
| Id de variante único en todo el catálogo | Índice único `variantes_id_unico` (`03-indices.md`) |
| La categoría existe | Servicio, contra `categorias` |
| Producto nuevo con `publicado: false` | Servicio |
| Promoción con vigencia y `desde < hasta`; `MEDIO_PAGO` exige `medioPago` | **Esquema de la base** (`$jsonSchema` + `$expr`) y `03_verificar.js`; la gestión por API queda para FAV |

**Validación de esquema en MongoDB.** MongoDB no impone un esquema por defecto: cada documento puede tener cualquier forma. Con `$jsonSchema` se le puede pedir a la colección que rechace los documentos que no cumplan (`validationAction: "error"`). Acá se usa en `productos` y `promociones` como **segunda barrera**, para que valga aunque la escritura no pase por la API (un import, `mongosh` a mano). Controla tipos y rangos: `precioBase.monto` entero largo mayor a 0, moneda `ARS`, `sistemaTalles` y talles de la lista, al menos una variante, ids de hasta 100 caracteres, `version` mayor o igual a 1, y en promociones la vigencia obligatoria con `desde < hasta` y el medio de pago cuando corresponde.

| Colección | Qué exige el esquema |
|---|---|
| `productos` | Campos obligatorios (`nombre`, `categoria`, `sistemaTalles`, `precioBase`, `publicado`, `variantes`, fechas y `version`) · `_id` de hasta 100 caracteres · `precioBase.monto` entero largo (`long`) mayor a 0 y moneda `ARS` · `sistemaTalles` y talles dentro de la lista permitida · al menos una variante, con `id`, `talle` y `color` · cada foto con `url` · `version` mayor o igual a 1 |
| `promociones` | `nombre`, `tipo`, `porcentaje` (1 a 100), `vigencia` y `alcance` obligatorios · `tipo` y `alcance.tipo` dentro de sus valores · `desde` y `hasta` de tipo fecha, con **`desde < hasta`** · si el tipo es `MEDIO_PAGO`, `medioPago` tiene que ser uno de los tres medios |

El validador no está en el código: **lo guarda MongoDB como metadato de la colección**, dentro del volumen de datos. El script `01` solo lo instala (con `collMod`), y se puede ver en la base con `pruebas/ver_esquema.js`. Con `validationAction: "error"` una escritura inválida falla con el código 121 (*Document failed validation*), venga de la API, de `mongoimport` o de `mongosh`.

**Cómo se mantiene sincronizada con el servicio Go.** Las dos capas aplican las mismas reglas. Los talles están en dos lugares (`modelo.SistemasTalles` en Go y `TALLES` en el script) y un test del servicio (`esquema_test.go`) falla si se separan. Lo que el esquema no puede expresar queda en el servicio: que la categoría exista y que el talle sea el del sistema **de ese producto** (cruza documentos y campos).

**Se muestra con:** `scripts/01_crear_colecciones_e_indices.js` (colecciones, esquema e índices), `docker compose run --rm cargador` (carga y verificación) y `pruebas/validacion_esquema.js` (inserciones inválidas rechazadas, con su salida en `evidencia/validacion_esquema.txt`).
