# TP MongoDB · Catálogo de FAV · Guía de trabajo

> **Versión 0.4** · 01/11/2026 · Entrega: **martes 3/11/2026**, en un `.zip`. No hay entregas parciales.
>
> Convenciones: **[propuesta]** decisión provisoria nuestra · **[?]** pendiente de definir con alguien (ver sección 14).
>
> **Para qué sirve este archivo:** es la fuente única de lo definido hasta hoy. Se deja en `tp-mongo/PROYECTO.md`. El informe de diseño que se entrega (`informe/01-diseno.md`) se arma a partir de la sección 5. Si algo de acá cambia, se cambia acá primero.
>
> **Para quien implemente (persona o asistente):** leer las secciones 2, 3 y 13 antes de escribir código. Si algo no está definido, no se inventa: se anota en la sección 14 y se usa lo provisorio indicado.

---

## 1. Contexto y reglas de juego

| Tema | Definición |
|---|---|
| Materia | Base de Datos Aplicada. Consigna: *Trabajo Práctico Integrador, Diseño e implementación de una base de datos NoSQL* (MongoDB) |
| Dominio | **Catálogo y gestión de productos**, aprobado por el profesor. Es el servicio Catálogo del proyecto FAV (Arquitectura de Software) |
| Equipo | 2 personas |
| Entrega | Martes 3/11/2026, en `.zip`. Informe en `.md`. Solo el servicio Catálogo; FAV se puede mostrar aparte desde la PC |
| Defensa | El profesor ve el proyecto y hace preguntas. Se muestra desde la PC de uno de los integrantes. Cada integrante debe poder explicar el modelo, ejecutar una consulta, justificar un índice y describir el repositorio |
| Aprobado por el profesor | Dominio con `productos`, `categorias` y `promociones`, **sin stock** · API en **Go** y notebook en **Python** · consistencia con **operación atómica sobre un documento** · **baja lógica** como `DELETE` · pruebas con evidencia manual · informe en `.md` |

### Ramas y carpetas

- Ramas de git: `development` → `services-catalogo` (servicio para FAV) → `services-catalogo-mongo` (TP). **Se trabaja en `services-catalogo-mongo`.**
- Hasta la entrega se toca **solo** `services/catalogo/` y `tp-mongo/`. No se modifican el `docker-compose.yml` raíz, el gateway, `.env.example` ni `go.work` (los usa también el resto del equipo de FAV). La integración al raíz se hace después, en otro PR.
- Después de entregar: tag `tp-mongo-v1` sobre lo entregado, merge a `services-catalogo` y seguir con FAV.

---

## 2. Decisiones tomadas

| # | Decisión | Detalle |
|---|---|---|
| D1 | **El stock no está en Catálogo** | Es de Inventario (MySQL). Catálogo solo conoce el id de la variante. Si el profesor pregunta cómo se haría en Mongo, se contesta de forma oral (sección 12) |
| D2 | **Despublicar en lugar de borrar** | `DELETE /v1/admin/productos/{id}` hace baja lógica (`publicado: false`), devuelve `204` y es idempotente. Los ids no se reutilizan |
| D3 | **Las promociones no se acumulan** | Si aplican varias a un producto, gana la de mayor porcentaje **[propuesta]** |
| D4 | **El precio público asume transferencia** | Si no se indica `medioPago`, vale `TRANSFERENCIA`. No se implementa ninguna pasarela de pagos |
| D5 | **Redondeo hacia abajo a 2 decimales** | Se calcula en **centavos enteros** y se trunca, para evitar errores de coma flotante (`0.29 * 100 = 28.999…`) |
| D6 | **Un producto puede venderse en el local sin estar publicado** | Publicado solo habilita la web. La consulta por lote no filtra por `publicado` |
| D7 | **Un solo código para las dos materias** | El servicio de `services/catalogo/` es el de FAV y corre solo, con su base. Lo exclusivo del TP vive en `tp-mongo/` |
| D8 | **Sin autenticación, caché, búsqueda externa ni eventos** | La consigna no los pide. Quedan para la etapa FAV (sección 15) |
| D9 | **Compose del TP en `tp-mongo/`** | La carpeta del servicio queda solo con lo que es de FAV |
| D10 | **Jupyter** | El notebook se entrega ejecutado (`.ipynb`) y exportado a HTML. Para la demo se puede mostrar el HTML. El servicio de Jupyter en el Compose es opcional y se deja para el final |
| D11 | **Dashboard interactivo** | Opcional. Solo si sobra tiempo, al final |
| D12 | **El servidor genera el id de variante** | El administrador no lo escribe. Regla en 5.5. El cargador de datos usa la misma función. Los 3 ids del contrato ya existen y se cargan tal cual (sección 3, punto 11) |
| D13 | **Montos en centavos enteros (`int64`)** | `precioBase.monto` se guarda en centavos, nunca como double. La API convierte a pesos con 2 decimales hacia afuera, porque el contrato define `precio` como `number` (double) en pesos |
| D14 | **El CRUD completo va bajo `/v1/admin/productos`** | Así calza con la tabla de la sección 5.4 de la consigna (un solo recurso para crear, obtener, actualizar, eliminar y listar). `GET /v1/productos` queda libre para la ruta pública del contrato, en la etapa FAV |
| D15 | **Códigos de error propios, con el estilo de Inventario** | `PEDIDO_INVALIDO` (400), `PRODUCTO_NO_ENCONTRADO` (404), `VARIANTE_DUPLICADA` (409), `CONFLICTO_DE_VERSION` (409), `VARIANTE_INEXISTENTE` (400 en el lote, 404 en la ficha; ver sección 7) |

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

## 4. Checklist de la consigna

El criterio de evaluación suma 100 puntos. Cada fila es lo que hay que poder mostrar.

| Criterio | Pts | Se cumple con | Listo |
|---|---|---|---|
| **Diseño documental y justificación** | 15 | Sección 5: colecciones, JSON, obligatorios y opcionales, anidados y arreglos, relaciones, embeber vs referenciar | ☐ |
| **Implementación y calidad de los datos** | 10 | 100 documentos como mínimo (meta: 200 a 500). Scripts reproducibles, verificación de cantidades, variedad, atributos opcionales y casos límite. Cómo reiniciar la base | ☐ |
| **Consultas y agregaciones** | 15 | 10 consultas diferentes, cada una con objetivo, comando y resultado esperado (sección 6). Variar solo un filtro no cuenta | ☐ |
| **API y repositorios** | 20 | CRUD funcional, capa de repositorio, validación, ids inexistentes, códigos HTTP y mensajes de error. Demostrable por Swagger o Postman | ☐ |
| **Índices y consistencia** | 10 | Al menos 2 índices, qué campos y por qué, `explain("executionStats")`, beneficio vs costo. Caso de consistencia: qué pasa si una parte falla, solución y prueba | ☐ |
| **Análisis de datos** | 10 | Conexión, preparación, **3 indicadores como mínimo**, **2 visualizaciones diferentes**, interpretación escrita y conclusión con recomendación | ☐ |
| **Docker, reproducibilidad y pruebas** | 10 | Mongo con volumen, variables y archivo de ejemplo sin credenciales, API en el mismo Compose conectada por **nombre de servicio** (no `localhost`), cómo iniciar, detener y cargar datos, prueba desde un entorno limpio | ☐ |
| **Documentación y defensa individual** | 10 | README con requisitos, comandos, carga y pruebas. Cada integrante explica modelo, consulta, índice y repositorio | ☐ |

**Entregables (sección 7 de la consigna):** informe técnico · scripts y datos · código de la aplicación · análisis de datos · despliegue · presentación y demostración. No incluir contraseñas, tokens ni secretos.

**Extensiones opcionales que conviene tomar** (son baratas y suman): índice de texto, validación de esquema con `$jsonSchema`, pruebas de integración y prueba de concurrencia.

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

**Sistemas de talles (RN-02):** `LETRAS` (XS, S, M, L, XL, XXL) · `NUMERICO_PAR` (32, 34, 36, 38, 40, 42, 44, 46) · `NUMERICO_1_4` (1, 2, 3, 4) · `UNICO` (valor `U` **[propuesta]**).

**Categorías (RN-17), 13:** tops, bodys, remeras, buzos, camperas, jeans, pantalones, polleras, shorts, vestidos, conjuntos, abanicos, accesorios. Una por producto **[propuesta]**.

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
| `fotos` | arreglo | No | `url` obligatoria, `color` opcional (RN-03). Solo la URL. La **primera** es la principal (`fotoUrl` del contrato) **[?]** |
| `variantes` | arreglo | Sí (1+) | `id`, `talle`, `color` obligatorios. **Sin stock** |
| `creadoEn`, `actualizadoEn` | fecha | Sí | "Novedad" se ordena por `creadoEn` |
| `version` | entero | Sí | Empieza en 1. **Sube con todo cambio**: editar, publicar, despublicar, agregar variante |

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
| `alcance` | A qué productos se aplica: `PRODUCTO`, `CATEGORIA` o `TIENDA`, con los ids correspondientes. Los tres quedan soportados porque el SPEC no fijó cuál usa el cliente **[?]** |

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
- Los genera el **servidor**, en una sola función que también usa el cargador de datos (D12):
  - Producto: `prod-` + nombre normalizado. Si el id ya existe, se agrega un sufijo numérico **[propuesta]**.
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

La respuesta debería indicar el precio base, el vigente y la promoción aplicada (si la hay).

### 5.8 Índices

Se crean al arrancar (idempotente, porque dos réplicas pueden arrancar a la vez) y también en `scripts/01_crear_colecciones_e_indices.js`, que es lo que se muestra en la defensa. **Las dos listas tienen que ser iguales** (sección 14).

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
    variantes: { $not: { $elemMatch: { talle: "S", color: "Blanco" } } } },
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

## 6. Consultas (10)

Cada una se documenta con **objetivo, comando y resultado esperado** en `consultas/consultas.js` (comentado) y su salida en `evidencia/`.

| # | Pregunta del dominio | Tipo que pide la consigna | Operadores |
|---|---|---|---|
| 1 | ¿Qué productos publicados cuestan entre 10.000 y 30.000? | Filtro con comparación | `$gte`, `$lte` |
| 2 | ¿Qué promociones están vigentes hoy? | Filtro con comparación (fechas) | `$lte`, `$gte` |
| 3 | ¿Qué remeras o jeans publicados cuestan menos de 20.000? | Operadores lógicos | `$and`, `$or` |
| 4 | ¿Qué productos son de algodón y se venden en pesos? | Atributos anidados | `atributos.material`, `precioBase.moneda` |
| 5 | ¿Qué productos tienen la variante M en negro? ¿Cuáles tienen talle S, M o L? ¿Cuáles tienen una sola variante? | Arreglos | `$elemMatch`, `$in`, `$size` |
| 6 | ¿Cuáles son los 5 productos publicados más nuevos? (solo nombre y precio) | Proyección, orden y límite | `find` con `sort` y `limit` |
| 7 | ¿Cuál es el precio vigente de cada producto con su promoción? | `aggregate` | `$lookup`, `$addFields`, `$project` |
| 8 | ¿Qué colores se ofrecen más? | `aggregate` | `$unwind`, `$group`, `$sort` |
| 9 | Por categoría: cantidad, precio promedio, mínimo y máximo | Agrupamiento y estadísticas | `$group` con `$sum`, `$avg`, `$min`, `$max` |
| 10 | ¿Qué categorías, talles y colores hay para armar los filtros de la tienda? | Adicional de complejidad (elegida por el equipo) | `$facet` |

Los montos de las preguntas están en pesos, pero en los comandos se escriben en centavos (D13). Por ejemplo, 10.000 pesos es `1000000`.

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
| `POST /v1/promociones`, `PUT /v1/promociones/{id}`, `GET /v1/promociones` | Gestionar promociones |
| `GET /healthz`, `GET /readyz` | Sondas. La segunda verifica la conexión a MongoDB |

- El listado filtra y ordena por **precio base**; el vigente se calcula al consultar la ficha o el lote **[?]**.
- `GET /v1/productos` queda reservada para la ruta pública del contrato (con `items`, solo publicados), que se agrega en la etapa FAV como adaptador aparte (D14). Por eso todo el CRUD de administración está bajo `/v1/admin/productos`.
- Estructura en capas: `handler` (HTTP y validación de entrada) → `service` (reglas y precio vigente) → `repository` (único lugar que habla con MongoDB). Sin estado en memoria.

---

## 8. Datos de prueba

| Conjunto | Contenido | Para qué |
|---|---|---|
| **Volumen (TP)** | 13 categorías, ~250 productos (~900 variantes), 12 a 15 promociones | Superar el mínimo de la consigna y alimentar el análisis |
| **Mínimo (FAV)** | Los 2 productos y 3 variantes del contrato, con los ids de la sección 3, punto 11 | Que el flujo completo siga funcionando al integrar |

- Las variantes del conjunto de volumen **no existen en Inventario**: una compra sobre ellas fallaría allí. Por eso FAV carga solo el mínimo.
- **El TP carga los dos conjuntos.** `docker compose run --rm cargador` hace la carga completa (mínimo, volumen y promociones). `docker compose run --rm -e CARGA=minimo cargador` carga solo las categorías y los 2 productos del contrato, que es lo que usa FAV.
- Archivos en `datos/`: `categorias.json` (13), `productos_minimo.json` (2, con los ids heredados exactos), `productos.json` (250 productos, 881 variantes) y `promociones.json` (14). En total son 279 documentos.
- **Generador determinista** en Python (`datos/generar.py`, semilla fija), cuyos JSON ya generados se entregan. Para regenerarlos: `python datos/generar.py`. La carga corre dentro de Docker (`mongoimport` y `mongosh`), sin instalar nada.
- `02_cargar` vacía las colecciones con `deleteMany` y vuelve a importar, así se puede repetir. No usa `mongoimport --drop`, porque borraría los índices de `01`. El índice único de `variantes.id` hace que la carga falle si hubiera un id repetido.
- **Promociones respecto de la defensa (03/11/2026):** 6 vigentes, 4 vencidas y 4 futuras. Un jean está en "Semana del jean" (20%) y en la liquidación (30%), para mostrar que gana la mayor (D3).
- **Casos límite:** producto despublicado; sin fotos ni atributos; talle único; con una sola variante y con muchas; precios mínimo y máximo; nombres con tildes y ñ; promoción vencida, futura, de medio de pago y de alcance `TIENDA`; fotos asociadas y no asociadas a un color.
- **`03_verificar.js`:** comprueba cantidades (al menos 100 documentos), que toda categoría referenciada exista, que los ids de variante sean únicos, que cada talle pertenezca al sistema del producto y que ninguna promoción tenga la vigencia invertida.

---

## 9. Análisis de datos

Notebook de Jupyter con PyMongo, Pandas y Matplotlib, que lee la base del proyecto.

| # | Indicador | Pregunta | Gráfico |
|---|---|---|---|
| 1 | Precio promedio, mínimo y máximo por categoría | ¿Dónde está concentrado el precio? | Barras |
| 2 | Cantidad de productos y variantes por categoría | ¿Dónde es más amplia la oferta? | Barras agrupadas |
| 3 | Porcentaje de productos con promoción vigente, por categoría | ¿Qué se promociona y qué no? | Barras |
| 4 | Talles y colores más ofrecidos | ¿Falta variedad en algún talle o color? | **Mapa de calor talle × color** |

- La consigna pide **al menos dos visualizaciones diferentes**. Para que sean claramente distintas, mezclar barras y un mapa de calor.
- Estructura del notebook: conexión y recuperación → preparación → indicadores y gráficos → **interpretación escrita de cada uno** → conclusión con una recomendación (por ejemplo, qué categorías ampliar o en cuáles lanzar promociones).
- No se hacen indicadores de stock ni rotación: el stock es de Inventario.
- Se entrega ejecutado (`.ipynb`) y exportado a HTML. Las credenciales salen de variables de entorno, no del notebook.

---

## 10. Estructura del repositorio y del zip

El zip es la copia de estas dos carpetas más un `README.md` en la raíz. Se arma con un script.

```
services/catalogo/                # el servicio, el mismo que usará FAV
├── cmd/catalogo/main.go
├── internal/{handler,service,repository}/    # con sus tests
├── Dockerfile · go.mod · go.sum
└── README.md
tp-mongo/
├── PROYECTO.md                   # este archivo
├── docker-compose.yml            # mongo-catalogo + catalogo + swagger-ui + cargador (+ analisis opcional)
├── .env.example                  # sin credenciales reales
├── informe/                      # diseño, consultas e índices, consistencia, conclusiones
├── datos/                        # generador y JSON
├── scripts/                      # 01 crear colecciones e índices · 02 cargar · 03 verificar
├── consultas/consultas.js        # las 10, comentadas
├── api/openapi.yaml              # para Swagger
├── pruebas/                      # requests de ejemplo y script de concurrencia
├── analisis/                     # notebook, versión HTML, requirements.txt
├── evidencia/                    # salidas, explain, concurrencia, capturas
├── guion-demo.md
└── empaquetar.sh
```

### Docker Compose

- El servicio de Mongo se llama **`mongo-catalogo`**, igual que en el Compose de FAV, así la misma configuración sirve en los dos. La API se conecta por ese nombre, **nunca por `localhost`**.
- URI: `mongodb://USUARIO:CLAVE@mongo-catalogo:27017/BASE?authSource=admin`. **El `authSource=admin` es obligatorio**: con las variables `MONGO_INITDB_ROOT_*` el usuario vive en la base `admin` y sin eso la conexión falla.
- Volumen para persistir los datos, `healthcheck` en Mongo y `depends_on` con `service_healthy`.
- Variables: las mismas del `.env.example` raíz de FAV (`MONGO_INITDB_ROOT_USERNAME`, `MONGO_INITDB_ROOT_PASSWORD`, `MONGO_DB`).
- Puertos distintos de los de FAV (que usa 8080, 8095, 9090, 15672 y 16686), por ejemplo API en `8085` y Swagger en `8086`. El `27017` de Mongo se publica **solo en este Compose** (el notebook lo necesita); en el Compose raíz de FAV nunca, porque ningún servicio accede a la base de otro.
- `cargador`: imagen de Mongo que corre los scripts y se invoca con `docker compose run --rm cargador`.

---

## 11. Cómo probar y cómo mostrar

### Prueba desde un entorno limpio

Con otra carpeta (o en la PC del compañero) y solo Docker:

```bash
unzip catalogo-mongo.zip && cd tp-mongo
cp .env.example .env
docker compose up -d --build            # mongo, api y swagger-ui
docker compose run --rm cargador        # crea colecciones e índices, carga datos y verifica
curl localhost:8085/healthz
docker compose exec mongo-catalogo mongosh --file /consultas/consultas.js
docker compose down -v                  # reinicia todo (borra el volumen)
```

Para el notebook: `pip install -r analisis/requirements.txt` y abrirlo con Jupyter, o el HTML en el navegador.

### Recorrido de la demo (unos 10 minutos)

1. Entorno limpio: `down -v`, `up --build`, carga de datos.
2. Modelo: producto con variantes y fotos embebidas; promoción que apunta a una categoría. Por qué embeber y por qué referenciar.
3. Consultas: 3 o 4, siempre la del precio vigente con `$lookup` y una de agrupamiento.
4. Índices: `explain("executionStats")` antes y después, y qué se observa.
5. API en Swagger: crear (nace no publicado), agregar variante, repetirla (`409`), `DELETE` que despublica.
6. Consistencia: script con 20 pedidos simultáneos, un solo `201`.
7. Análisis: el notebook (HTML) con gráficos y conclusión.
8. Si preguntan por FAV: se muestra desde la PC con el Compose raíz.

**Respaldo:** capturas de cada paso, por si Docker falla. Ensayar al menos una vez en la máquina donde se va a presentar, con las imágenes ya descargadas.

---

## 12. Si el profesor pregunta por el stock (solo respuesta oral)

1. **Dónde:** un campo `stock` dentro de cada variante embebida; descontar modifica un solo documento.
2. **Sin vender de más:** actualización atómica y condicional: `updateOne({ _id, variantes: { $elemMatch: { id, stock: { $gte: cantidad } } } }, { $inc: { "variantes.$.stock": -cantidad } })`. Si no coincide nada, no había stock.
3. **Varios productos en una compra:** son documentos distintos, así que hace falta una **transacción multidocumento** (requiere replica set) o descontar uno por uno con compensación si falla alguno.
4. **Reservas y trazabilidad:** una colección `reservas` con vencimiento y otra `movimientos_stock` (quién, cuándo, cuánto). Un índice TTL borra las reservas vencidas, pero devolver las unidades requiere un proceso aparte.
5. **Por qué FAV no lo hace así:** el catálogo se lee mucho y tolera atraso; el stock no puede venderse de más y la reserva toca varias variantes con restricciones que SQL resuelve de forma más directa. Por eso está en Inventario (MySQL), y Catálogo solo conoce el id de la variante.

*Frase de cierre:* "En MongoDB se puede, pero acá no corresponde: catálogo y stock son dos servicios con dos bases, cada una elegida para lo suyo."

---

## 13. Convenciones de código

- **Go 1.26.** Módulo como el de Inventario: `github.com/JuanHernandez-99/Fav-del-universo/services/catalogo`. Nombres del dominio en español, como en Inventario.
- **Driver oficial** de MongoDB para Go (`go.mongodb.org/mongo-driver/v2`).
- **Capas:** `handler` no conoce a Mongo, `service` no conoce a HTTP y `repository` es el único que importa el driver. El repositorio se define con una **interfaz**, así la búsqueda y la caché se pueden enchufar después sin tocar el servicio.
- **Sin estado en memoria** y con **cierre ordenado** (`SIGTERM`), para correr en varias réplicas.
- **Logs** en JSON con `slog`, con `servicio` e `instancia` en cada línea. Sondas sin loguear.
- **Errores** en `application/problem+json` con `codigo` estable.
- **Operaciones críticas dentro de la base:** la condición va dentro del filtro de la actualización, nunca "leer, decidir y escribir".
- **Tests:** unitarios del precio vigente y de las validaciones; de integración del repositorio contra un Mongo real; de concurrencia sobre la variante repetida.
- **Secretos:** nunca en el repositorio. `.env` va en `.gitignore`; solo se entrega `.env.example`.
- **Commits** chicos y frecuentes, en `services-catalogo-mongo`.

---

## 14. Pendientes y supuestos

| # | Tema | Estado | Provisorio |
|---|---|---|---|
| 1 | Alcance de las promociones (producto, categoría o tienda) | **[?]** Cliente | Los tres soportados |
| 2 | Si hay dos promociones aplicables, cuál gana | **[propuesta]** | La de mayor porcentaje |
| 3 | Valor del talle en el sistema "talle único" | **[propuesta]** | `U` |
| 4 | Una sola categoría por producto | **[propuesta]** (RN-17) | Un string |
| 5 | Cuál es la foto principal | **[?]** | La primera del arreglo |
| 6 | Dónde se guardan las fotos | **[?]** ADR-003 | Solo URL |
| 7 | El listado filtra y ordena por precio base o vigente | **[?]** | Base |
| 8 | Contenido del evento `VarianteCreada` | **[?]** Equipo (Inventario) | Fuera de este trabajo |
| 9 | Ids de variante y rutas de Inventario | Hay que **revisarlos antes de entregar**: el código leído es el de `development` y puede cambiar | — |
| 10 | Los índices del servicio y los del script `01_…` deben coincidir | Riesgo de duplicación | Revisar al final |
| 11 | Cómo se normalizan los espacios y la ñ en los ids (5.5) | Confirmado | Espacio → `-` (como `remera-basica`), ñ → `n`. Además, cualquier otro símbolo pasa a `-` y se quitan los `-` repetidos o de los extremos (`normalizar` en `datos/generar.py`; la función de Go tiene que dar lo mismo) |
| 12 | Dos colores distintos que se normalizan igual (por ejemplo "Azul marino" y "azul-marino") generarían el mismo id | **[propuesta]** | El índice único lo rechaza y se responde `409 VARIANTE_DUPLICADA` |
| 13 | Variante inexistente: `400` en el lote y `404` en la ficha | Decidido, a propósito (sección 7) | — |
| 14 | Zona horaria de las vigencias de promociones | Confirmado | Se definen en hora de Argentina (UTC−3) y se guardan en UTC: "hasta el 30/11" es `2026-12-01T02:59:59Z` |

---

## 15. Qué queda para la etapa FAV

Sin cambiar lo anterior:

- **Adaptador del contrato público** `catalogo-pedidos-v1` (solo publicados, sin stock, `fotoUrl`), aparte del API de administración, y la separación de rutas en el gateway (hoy `/v1/productos` y `/v1/pedidos` van al mismo mock).
- **Evento `VarianteCreada`** con outbox (ni se publica ni se consume hoy). Si se guarda junto con el producto en una transacción, requiere replica set.
- **Eventos de cambio de producto** para el indexador, con la `version` ya presente, y el **motor de búsqueda** (ADR-006) en lugar del índice de texto de Mongo.
- **Caché** con Redis (ADR-007), como capa delante del repositorio.
- **Observabilidad**: correlation id, trazas y métricas (ADR-011).
- **Dos réplicas** detrás del gateway, e identidad con los encabezados `X-User-*`.
- **Tests de contrato** contra el YAML publicado.
- El **stock sigue siendo de Inventario**.

---

## 16. Plan hasta el martes

| Día | Persona A (API) | Persona B (datos, consultas, análisis) |
|---|---|---|
| **Sáb 31** | Esqueleto Go en capas, conexión a Mongo, Compose con `mongo-catalogo` y `catalogo`, `/healthz` | Generador de datos, scripts 01 a 03, y las primeras consultas |
| **Dom 1** | CRUD, validaciones, errores, variante atómica y prueba de concurrencia, consulta por lote | Las 10 consultas, índices con `explain`, e informe de consultas e índices |
| **Lun 2** | `openapi.yaml`, README, tests del repositorio, prueba desde un entorno limpio | Notebook con 4 indicadores y 2 tipos de gráfico, HTML, informe técnico y guion de la demo |
| **Mar 3** | Empaquetar y entregar el zip | |

**Antes de entregar, el lunes:** cada uno explica al otro sus partes (la defensa es individual), se hace una prueba completa desde cero y se revisa el punto 9 de la sección 14.

**Definición de terminado:** el checklist de la sección 4 está tildado, `docker compose up` más la carga funcionan desde cero en una máquina limpia, y el zip no contiene secretos.
