# Catálogo de FAV: diseño de la base de datos (MongoDB)

> Trabajo Práctico Integrador · Base de Datos Aplicada · **versión 0.1** · 29/10/2026
>
> Convenciones: **[propuesta]** decisión provisoria del equipo, a validar · **[?]** pendiente de definir.
>
> Equipo: _completar_. Fuentes: `SPEC.md`, `docs/ARCHITECTURE.md`, ADR-001, ADR-003 y el contrato `catalogo-pedidos-v1` del repositorio de FAV.

## 1. Problema y actores

FAV es una tienda de ropa que hoy vende solo en el local y lleva el stock en un Excel. El sistema completo (cuatro microservicios) agrega la venta online. Este trabajo diseña e implementa el servicio **Catálogo**: qué se vende y cómo se muestra (productos, variantes, fotos, categorías, precios y promociones) y el cálculo del **precio vigente**.

**Por qué una base documental.** Un producto se lee y se edita como una unidad: nombre, fotos, variantes (talle y color) y precio viajan juntos. Los atributos cambian según la prenda (una campera tiene capucha, algunos vestidos no tienen talle) y no conviene una tabla con columnas casi vacías. La carga es mayormente de lectura (ficha y listados) y las escrituras tocan un solo producto, hechas por un solo administrador.

| Actor                              | Qué hace con el Catálogo                                                               |
| ---------------------------------- | -------------------------------------------------------------------------------------- |
| Administrador                      | Carga y edita productos, variantes y fotos; publica y despublica; gestiona promociones |
| Comprador                          | Ve productos, filtra y busca (a través de la tienda web)                               |
| Servicio de Ventas                 | Pide el precio vigente de cada variante al comprar                                     |
| Grupo de proveedores (otro equipo) | Consulta el catálogo publicado (se integra en la etapa FAV)                            |

**Fuera de este servicio:** el stock (lo conoce Inventario), las ventas, los pagos y los usuarios. Catálogo **no sabe cuántas unidades hay** (ADR-001).

## 2. Requerimientos funcionales

| ID    | Requerimiento                                                                               | Origen              |
| ----- | ------------------------------------------------------------------------------------------- | ------------------- |
| RF-01 | Alta y edición de productos con variantes (talle y color) y fotos                           | HU-03, RN-01, RN-03 |
| RF-02 | Cada producto usa **un solo sistema de talles** y solo acepta sus valores                   | RN-02               |
| RF-03 | No puede haber dos variantes con el mismo talle y color en un producto                      | HU-03.4             |
| RF-04 | Publicar y despublicar. Un producto nuevo nace **no publicado**. No se borra: se despublica | RN-04, HU-04        |
| RF-05 | Cada producto pertenece a una categoría de la lista fija                                    | RN-17               |
| RF-06 | Promociones con vigencia (inicio y fin obligatorios), gestionadas aparte del precio base    | RN-05, HU-05        |
| RF-07 | El servicio calcula el **precio vigente** (precio base con la promoción aplicable)          | RN-05, RN-22        |
| RF-08 | Listado con filtros (categoría, talle, color, precio, texto), orden y paginación            | HU-02               |

**Sistemas de talles (RN-02):** `LETRAS` (XS, S, M, L, XL, XXL), `NUMERICO_PAR` (32 a 46, de a 2), `NUMERICO_1_4` (1, 2, 3, 4) y `UNICO` (un solo valor: `U`).

**Categorías (RN-17):** tops, bodys, remeras, buzos, camperas, jeans, pantalones, polleras, shorts, vestidos, conjuntos, abanicos y accesorios.

## 3. Colecciones

Son tres: `productos` (entidad principal, con CRUD), `categorias` y `promociones`.

### 3.1 `productos`

```json
{
  "_id": "prod-remera-basica",
  "nombre": "Remera básica",
  "descripcion": "Remera de algodón, corte recto.",
  "categoria": "remeras",
  "sistemaTalles": "LETRAS",
  "precioBase": { "monto": 15000, "moneda": "ARS" },
  "publicado": true,
  "etiquetas": ["basicos", "verano"],
  "atributos": { "material": "algodón", "genero": "unisex" },
  "fotos": [
    {
      "url": "https://fav.example/fotos/remera-basica-blanca-1.jpg",
      "color": "Blanco",
      "orden": 1
    },
    {
      "url": "https://fav.example/fotos/remera-basica.jpg",
      "color": null,
      "orden": 2
    }
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

| Campo                       | Tipo               | Obligatorio  | Nota                                                                                                                 |
| --------------------------- | ------------------ | ------------ | -------------------------------------------------------------------------------------------------------------------- |
| `_id`                       | string             | Sí           | Id legible (ver sección 4.2)                                                                                         |
| `nombre`                    | string             | Sí           | 1 a 120 caracteres                                                                                                   |
| `descripcion`               | string             | No           |                                                                                                                      |
| `categoria`                 | string             | Sí           | **Referencia** a `categorias._id`                                                                                    |
| `sistemaTalles`             | enum               | Sí           | Uno de los cuatro de RN-02                                                                                           |
| `precioBase`                | objeto             | Sí           | `monto` entero en pesos, mayor a 0; `moneda` fija `ARS` **[propuesta]**                                              |
| `publicado`                 | boolean            | Sí           | `false` por defecto (HU-03.1)                                                                                        |
| `etiquetas`                 | arreglo de string  | No           | Para filtrar y destacar (`oferta`, `verano`) **[propuesta]**                                                         |
| `atributos`                 | objeto libre       | No           | **Varía por categoría** (material, género, cuello, forro). Es la parte flexible del modelo                           |
| `fotos`                     | arreglo de objetos | No           | `url` y `orden` obligatorios; `color` opcional (RN-03). Se guarda la referencia, no el archivo (ADR-003)             |
| `variantes`                 | arreglo de objetos | Sí (1 o más) | `id`, `talle` y `color` obligatorios. **Sin stock** (RN-01 del lado de Inventario)                                   |
| `creadoEn`, `actualizadoEn` | fecha              | Sí           |                                                                                                                      |
| `version`                   | entero             | Sí           | Empieza en 1 y sube con cada cambio (control de concurrencia y, más adelante, sincronización del índice de búsqueda) |

### 3.2 `categorias`

```json
{ "_id": "remeras", "nombre": "Remeras", "orden": 3 }
```

Lista fija de 13 elementos (RN-17). Sirve para validar la categoría de un producto, mostrar el nombre y ordenar el menú.

### 3.3 `promociones`

```json
{
  "_id": "promo-verano-remeras",
  "nombre": "Verano en remeras",
  "tipo": "PORCENTAJE",
  "porcentaje": 20,
  "medioPago": null,
  "vigencia": {
    "desde": { "$date": "2026-12-01T00:00:00Z" },
    "hasta": { "$date": "2026-12-31T23:59:59Z" }
  },
  "alcance": { "tipo": "CATEGORIA", "ids": ["remeras"] },
  "activa": true
}
```

```json
{
  "_id": "promo-efectivo-tienda",
  "nombre": "10% pagando en efectivo",
  "tipo": "MEDIO_PAGO",
  "porcentaje": 10,
  "medioPago": "EFECTIVO",
  "vigencia": {
    "desde": { "$date": "2026-11-01T00:00:00Z" },
    "hasta": { "$date": "2026-11-30T23:59:59Z" }
  },
  "alcance": { "tipo": "TIENDA", "ids": [] },
  "activa": true
}
```

| Campo       | Nota                                                                                                                                                    |
| ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `tipo`      | `PORCENTAJE` o `MEDIO_PAGO` (RN-05). 2x1 y promociones bancarias quedan fuera de v1                                                                     |
| `medioPago` | Solo si `tipo` es `MEDIO_PAGO`: `EFECTIVO`, `TRANSFERENCIA` o `MERCADO_PAGO`                                                                            |
| `vigencia`  | `desde` y `hasta` **obligatorias**; sin ellas se rechaza (HU-05.2)                                                                                      |
| `alcance`   | `PRODUCTO`, `CATEGORIA` o `TIENDA`, con los ids correspondientes. **[?]** El SPEC deja pendiente con el cliente cuál se usa; el modelo soporta los tres |

## 4. Relaciones y decisiones de modelado

### 4.1 Embeber o referenciar

| Dato                    | Decisión                     | Por qué                                                                                                                                                                                                               |
| ----------------------- | ---------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Variantes               | **Embebidas**                | Se leen siempre con el producto, nacen y mueren con él y su cantidad es acotada (decenas). Un solo documento devuelve la ficha completa, sin joins                                                                    |
| Fotos                   | **Embebidas**                | Misma razón. Solo se guarda la URL                                                                                                                                                                                    |
| Precio base y atributos | **Embebidos**                | Son propiedades del producto, no entidades                                                                                                                                                                            |
| Categoría               | **Referencia** (`categoria`) | Es un conjunto pequeño y compartido por muchos productos; se valida contra `categorias`. El id es un texto legible (`remeras`), así que **no se duplica el nombre** en el producto y no hay que actualizar en cascada |
| Promociones             | **Colección aparte**         | Tienen otro ciclo de vida (se crean y vencen sin tocar el producto, HU-05), se aplican a muchos productos o a una categoría entera, y embebidas habría que copiar la misma promoción en cientos de documentos         |

El diseño responde a los patrones de acceso: lectura frecuente de la ficha completa y de listados filtrados, escrituras poco frecuentes sobre un solo producto. No se copió un modelo relacional (no hay tablas `variantes` ni `fotos` separadas).

### 4.2 Identificadores

`_id` es un **string legible**, no un ObjectId: el contrato publicado y Inventario ya identifican productos y variantes con textos (`prod-remera-basica`, `var-remera-basica-blanca-m`; en Inventario es `VARCHAR(100)`).

- Producto: `prod-` + nombre normalizado. Si ya existe, se agrega un sufijo numérico (`-2`).
- Variante: `var-` + id del producto sin el prefijo + color + talle. Es única en todo el catálogo porque el producto es único y dentro del producto no se repite talle y color.
- **Como los productos no se borran (solo se despublican), un id nunca se reutiliza.** Si se borraran, una variante recreada con el mismo id heredaría el stock de la anterior en Inventario.

## 5. Reglas de validación

| Regla                                                                | Dónde se valida                             |
| -------------------------------------------------------------------- | ------------------------------------------- |
| El talle pertenece al `sistemaTalles` del producto (RN-02, HU-03.2)  | Servicio, y esquema de Mongo como extensión |
| Categoría existente                                                  | Servicio                                    |
| `precioBase.monto` entero mayor a 0                                  | Servicio y esquema                          |
| Producto con al menos una variante (RN-01)                           | Servicio y esquema                          |
| Sin talle y color repetidos en un producto (HU-03.4)                 | **Operación atómica** (sección 7)           |
| Promoción con `vigencia.desde` y `hasta` y `desde < hasta` (HU-05.2) | Servicio y esquema                          |
| `MEDIO_PAGO` exige `medioPago`; `PORCENTAJE` no                      | Servicio                                    |
| Producto nuevo con `publicado: false` (HU-03.1)                      | Servicio                                    |

**Extensión opcional:** validación de esquema con `$jsonSchema` en la colección. ADR-003 ya contempla esta opción porque MongoDB no impone la forma de los documentos.

### Precio vigente **[propuesta]**

1. Se toman las promociones `PORCENTAJE` activas, con la fecha actual dentro de la vigencia y con alcance que incluya al producto (por id, por categoría o de toda la tienda).
2. Si hay varias, **se aplica la de mayor porcentaje** (no se acumulan). **[?]** El cliente debe confirmarlo.
3. Las promociones `MEDIO_PAGO` solo se aplican cuando quien pregunta indica el medio de pago.
4. El resultado se redondea al peso.

## 6. Patrones de acceso e índices (borrador)

| Consulta frecuente                                                      | Índice                                                                    |
| ----------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| Listar publicados por categoría y rango de precio, ordenados por precio | `{ publicado: 1, categoria: 1, "precioBase.monto": 1 }` (compuesto)       |
| Buscar por texto en nombre y descripción                                | Índice de texto `{ nombre: "text", descripcion: "text" }`, idioma español |
| Filtrar por talle y color                                               | `{ "variantes.talle": 1, "variantes.color": 1 }` (multikey)               |
| Garantizar que un id de variante sea único en el catálogo               | `{ "variantes.id": 1 }` único (multikey)                                  |
| Promociones vigentes hoy                                                | `{ "vigencia.desde": 1, "vigencia.hasta": 1 }`                            |
| Promociones por alcance                                                 | `{ "alcance.tipo": 1, "alcance.ids": 1 }`                                 |

Cada índice acelera lecturas y **cuesta** espacio y trabajo en cada escritura. Con pocas escrituras (un solo administrador) el costo es aceptable. Cada uno se justifica con `explain("executionStats")` en el segundo documento del informe.

## 7. Consistencia y concurrencia

### Caso principal: agregar una variante sin duplicarla

**Qué podría pasar sin protección.** Dos pedidos simultáneos (dos pestañas del panel, o un reintento por red) agregan la misma variante `M` / `Blanco`. Cada uno lee el producto, no ve duplicados y agrega la suya: el producto queda con dos variantes iguales, con ids distintos. Más adelante Inventario recibiría las dos y repartiría el stock entre ambas.

**Un índice único no alcanza.** En MongoDB, un índice único sobre un arreglo (`variantes.id`) evita repetidos **entre documentos**, no dentro del mismo arreglo. Por eso se usan las dos cosas: el índice protege la unicidad del id en todo el catálogo y la actualización condicional protege talle y color dentro del producto.

**Solución: una operación atómica sobre un solo documento.** La condición y la modificación ocurren en la misma instrucción:

```js
db.productos.updateOne(
  {
    _id: idProducto,
    variantes: { $not: { $elemMatch: { talle: "M", color: "Blanco" } } },
  },
  {
    $push: {
      variantes: {
        id: "var-remera-basica-blanca-m",
        talle: "M",
        color: "Blanco",
      },
    },
    $set: { actualizadoEn: new Date() },
    $inc: { version: 1 },
  },
);
```

Si `matchedCount` es 0, el servicio distingue si el producto no existe (`404`) o si la variante ya estaba (`409 VARIANTE_DUPLICADA`).

**Prueba del comportamiento.** Se lanzan 20 pedidos simultáneos con el mismo talle y color: debe haber **exactamente un `201`** y diecinueve `409`, y el producto debe quedar con una sola variante. Se guarda como evidencia.

### Caso secundario (opcional): edición concurrente

Al editar un producto, el pedido lleva la `version` que leyó y el filtro la incluye (`{ _id, version }`). Si otro cambio se coló en el medio, no se encuentra el documento y se responde `409 VERSION_DESACTUALIZADA`, evitando pisar cambios.

### Por qué no se usa una transacción multidocumento

Cada operación crítica del Catálogo modifica **un solo documento**, y MongoDB garantiza que esa operación es atómica. Las transacciones multidocumento requieren replica set y agregan complejidad sin necesidad. **Quedan como extensión:** registrar el producto y un evento `VarianteCreada` en un outbox en la misma transacción (etapa FAV).

## 8. Consultas previstas (10)

| #   | Pregunta del dominio                                                               | Tipo que pide la consigna         | Operadores                                  |
| --- | ---------------------------------------------------------------------------------- | --------------------------------- | ------------------------------------------- |
| 1   | ¿Qué productos publicados cuestan entre 10.000 y 30.000 pesos?                     | Filtro con comparación            | `$gte`, `$lte`                              |
| 2   | ¿Qué promociones están vigentes hoy?                                               | Filtro con comparación (fechas)   | `$lte`, `$gte`                              |
| 3   | ¿Qué remeras o jeans publicados cuestan menos de 20.000?                           | Operadores lógicos                | `$and`, `$or`                               |
| 4   | ¿Qué productos son de algodón y se venden en pesos?                                | Atributos anidados                | `atributos.material`, `precioBase.moneda`   |
| 5   | ¿Qué productos tienen la variante M en negro? ¿Cuáles tienen la etiqueta "oferta"? | Arreglos                          | `$elemMatch`, `$in`, `$size`                |
| 6   | ¿Cuáles son los 5 productos publicados más nuevos? (solo nombre y precio)          | Proyección, orden y límite        | `find` con `sort` y `limit`                 |
| 7   | ¿Cuál es el precio vigente de cada producto con su promoción?                      | `aggregate`                       | `$lookup`, `$addFields`, `$project`         |
| 8   | ¿Qué colores se ofrecen más?                                                       | `aggregate`                       | `$unwind`, `$group`, `$sort`                |
| 9   | Por categoría: cantidad, precio promedio, mínimo y máximo                          | Agrupamiento y estadísticas       | `$group` con `$sum`, `$avg`, `$min`, `$max` |
| 10  | ¿Qué categorías, talles y colores hay para armar los filtros de la tienda?         | Adicional (elegida por el equipo) | `$facet`                                    |

Cada consulta se documenta con su objetivo, comando y resultado esperado en `tp-mongo/consultas/`.

## 9. Datos de prueba

| Conjunto         | Contenido                                                                                                    | Para qué                                                 |
| ---------------- | ------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------- |
| **Volumen (TP)** | 13 categorías, unos 250 productos (unas 900 variantes) y 12 a 15 promociones                                 | Cumplir el mínimo de la consigna y alimentar el análisis |
| **Mínimo (FAV)** | Los 2 productos y 3 variantes del contrato, con **los mismos ids** que usa Inventario en su carga de ejemplo | Que el flujo completo siga funcionando cuando se integre |

**Casos límite incluidos:** producto despublicado; producto sin fotos ni atributos; talle único; producto con una sola variante y con muchas; precios mínimo y máximo; nombres con tildes y ñ; promoción vencida, futura, de medio de pago y de alcance `TIENDA`; fotos asociadas y no asociadas a un color.

## 10. API del TP

La API usa el recurso `productos`. Los errores siguen el formato `application/problem+json` con un campo `codigo` estable, como el contrato de FAV.

| Método y ruta                                  | Función                                                                                                                    |
| ---------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `POST /v1/productos`                           | Crear (nace no publicado, con al menos una variante)                                                                       |
| `GET /v1/productos/{id}`                       | Obtener por id (`404 PRODUCTO_INEXISTENTE`)                                                                                |
| `PUT /v1/productos/{id}`                       | Actualizar los datos del producto (lleva `version`)                                                                        |
| `DELETE /v1/productos/{id}`                    | **Baja lógica**: despublica. `204` e idempotente                                                                           |
| `POST /v1/productos/{id}/publicar`             | Publicar                                                                                                                   |
| `GET /v1/productos`                            | Listar con filtros: `categoria`, `talle`, `color`, `precioMin`, `precioMax`, `q`, `publicado`, `orden`, `pagina`, `tamano` |
| `POST /v1/productos/{id}/variantes`            | Agregar una variante (operación atómica)                                                                                   |
| `GET /v1/productos/{id}/precio`                | Precio vigente de cada variante (`medioPago` opcional)                                                                     |
| `POST /v1/promociones` y `GET /v1/promociones` | Crear y listar promociones                                                                                                 |
| `GET /healthz`                                 | Estado del servicio                                                                                                        |

Estructura interna **en capas**: `handler` (HTTP y validación de entrada) → `service` (reglas y precio vigente) → `repository` (único lugar que habla con MongoDB). Es el estilo ya definido para Catálogo en ADR-002.

## 11. Análisis de datos

Notebook de Jupyter con PyMongo, Pandas y Matplotlib, sobre la base del proyecto.

| Indicador                                                    | Pregunta                                | Gráfico             |
| ------------------------------------------------------------ | --------------------------------------- | ------------------- |
| Precio promedio, mínimo y máximo por categoría               | ¿Dónde está concentrado el precio?      | Barras              |
| Cantidad de productos y variantes por categoría              | ¿Dónde es más amplia la oferta?         | Barras              |
| Porcentaje de productos con promoción vigente, por categoría | ¿Qué se está promocionando y qué no?    | Barras              |
| Talles y colores más ofrecidos                               | ¿Falta variedad en algún talle o color? | Barras horizontales |

**Conclusión esperada:** una recomendación para el cliente, por ejemplo qué categorías conviene ampliar y en cuáles lanzar promociones.

## 12. Relación con el sistema FAV

Este servicio es el mismo código del Catálogo de FAV. Para la materia funciona solo, con su base. Queda para la etapa de integración, sin cambiar lo anterior:

- El adaptador del contrato `catalogo-pedidos-v1` (camelCase, solo publicados, precio vigente por variante, sin stock).
- El evento `VarianteCreada` con outbox, la caché (Redis), el motor de búsqueda y la observabilidad.
- Dos réplicas detrás del gateway.

El **stock nunca está en este servicio**: es de Inventario (MySQL), según ADR-001 y ADR-003.

## 13. Supuestos y pendientes

| Tema                                                                  | Estado                                                         |
| --------------------------------------------------------------------- | -------------------------------------------------------------- |
| Alcance de las promociones y si se acumulan                           | **[?]** Cliente. Provisorio: tres alcances, se aplica la mayor |
| Si una promoción por medio de pago aplica a pedidos por transferencia | **[?]**                                                        |
| Precios en pesos enteros y moneda única                               | **[propuesta]**                                                |
| Campo `etiquetas`                                                     | **[propuesta]** (no figura en el SPEC)                         |
| Dónde se guardan las fotos                                            | **[?]** ADR-003. Aquí solo se guarda la URL                    |
| Variantes: no se eliminan en v1                                       | **[propuesta]** (por la misma razón que los productos)         |
