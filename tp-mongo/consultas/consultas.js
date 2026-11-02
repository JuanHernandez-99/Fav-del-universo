// Las 10 consultas del TP (consigna 5.3 y PROYECTO.md sección 6), más una extra de búsqueda de texto.
// Cada una responde una pregunta del dominio y tiene objetivo, comando y resultado esperado.
//
// Uso (desde tp-mongo/, con los datos cargados por el cargador):
//   docker compose exec mongo-catalogo sh -c 'mongosh --quiet -u "$MONGO_INITDB_ROOT_USERNAME" -p "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin catalogo --file /consultas/consultas.js'
//
// Convenciones:
// - Los montos se guardan en centavos (D13): 10.000 pesos se escribe 1000000. Para leer más fácil,
//   las proyecciones los muestran en pesos con $divide.
// - "Hoy" es una fecha fija, el día de la defensa, para que los resultados sean reproducibles.
//   En la aplicación se usaría new Date().

db = db.getSiblingDB("catalogo");
const HOY = ISODate("2026-11-03T15:00:00Z"); // 03/11/2026, 12:00 de Argentina

function titulo(numero, pregunta) {
  print(`\n=== Consulta ${numero} · ${pregunta}`);
}
function mostrar(documentos, maximo = 5) {
  print(`${documentos.length} resultado(s)`);
  documentos.slice(0, maximo).forEach((d) => print("  " + JSON.stringify(d)));
  if (documentos.length > maximo) print(`  ... y ${documentos.length - maximo} más`);
}

// ---------------------------------------------------------------------------------------------
// Consulta 1 · Filtro con operadores de comparación
// Pregunta: ¿qué productos publicados cuestan entre $10.000 y $30.000?
// Objetivo: armar la vitrina de precio medio de la tienda.
// Resultado esperado: 89 productos, ordenados por precio desde $10.490 (Body manga larga liso).
titulo(1, "Productos publicados entre $10.000 y $30.000");
mostrar(
  db.productos.find(
    { publicado: true, "precioBase.monto": { $gte: 1000000, $lte: 3000000 } },
    { nombre: 1, categoria: 1, precio: { $divide: ["$precioBase.monto", 100] } }
  ).sort({ "precioBase.monto": 1, _id: 1 }).toArray()
);

// ---------------------------------------------------------------------------------------------
// Consulta 2 · Filtro con operadores de comparación (sobre fechas)
// Pregunta: ¿qué promociones están vigentes hoy?
// Objetivo: saber qué descuentos tienen que aplicarse al calcular el precio vigente.
// Resultado esperado: 6 promociones. Quedan afuera las 4 vencidas y las 4 futuras.
titulo(2, "Promociones vigentes hoy (03/11/2026)");
mostrar(
  db.promociones.find(
    { "vigencia.desde": { $lte: HOY }, "vigencia.hasta": { $gte: HOY } },
    { nombre: 1, tipo: 1, porcentaje: 1, medioPago: 1, "alcance.tipo": 1 }
  ).sort({ porcentaje: -1, _id: 1 }).toArray(),
  10
);

// ---------------------------------------------------------------------------------------------
// Consulta 3 · Operadores lógicos
// Pregunta: ¿qué remeras o jeans publicados cuestan menos de $20.000?
// Objetivo: armar una sección de "básicos baratos" que mezcla dos categorías.
// Resultado esperado: 18 productos, todos remeras: el jean más barato cuesta $30.000.
// Nota: el $or sobre un mismo campo equivale a { categoria: { $in: ["remeras", "jeans"] } }.
titulo(3, "Remeras o jeans publicados de menos de $20.000");
mostrar(
  db.productos.find(
    {
      $and: [
        { publicado: true },
        { $or: [{ categoria: "remeras" }, { categoria: "jeans" }] },
        { "precioBase.monto": { $lt: 2000000 } },
      ],
    },
    { nombre: 1, categoria: 1, precio: { $divide: ["$precioBase.monto", 100] } }
  ).sort({ "precioBase.monto": 1, _id: 1 }).toArray()
);

// ---------------------------------------------------------------------------------------------
// Consulta 4 · Atributos anidados
// Pregunta: ¿qué productos son de algodón y se venden en pesos?
// Objetivo: filtrar por un campo de "atributos", la parte flexible del modelo, y por un campo de
// otro subdocumento (precioBase).
// Resultado esperado: 32 productos con la notación de punto. La comparación contra el
// subdocumento entero da 0: exige que "atributos" sea exactamente { material: "algodón" }, sin
// ningún otro campo y en el mismo orden.
titulo(4, "Productos de algodón que se venden en pesos");
mostrar(
  db.productos.find(
    { "atributos.material": "algodón", "precioBase.moneda": "ARS" },
    { nombre: 1, atributos: 1 }
  ).sort({ _id: 1 }).toArray()
);
print(`Comparando el subdocumento entero { atributos: { material: "algodón" } }: ${
  db.productos.countDocuments({ atributos: { material: "algodón" } })
} resultado(s)`);

// ---------------------------------------------------------------------------------------------
// Consulta 5 · Arreglos
// Preguntas: ¿qué productos tienen la variante M en negro? ¿Cuáles tienen talle S, M o L?
// ¿Cuáles tienen una sola variante?
// Objetivo: consultar dentro del arreglo de variantes embebidas.
// Resultado esperado:
//   5a) $elemMatch: 8 productos. Con condiciones sueltas serían 9: un producto tiene una variante M
//       en otro color y otra negra en otro talle. $elemMatch exige que sean la misma variante.
//   5b) $in: 98 productos con alguna variante S, M o L.
//   5c) $size: 28 productos con una sola variante.
titulo("5a", "Productos con la variante M en negro ($elemMatch)");
mostrar(
  db.productos.find(
    { variantes: { $elemMatch: { talle: "M", color: "Negro" } } },
    { nombre: 1 }
  ).sort({ _id: 1 }).toArray()
);
print(`Con condiciones sueltas ("variantes.talle": "M", "variantes.color": "Negro"): ${
  db.productos.countDocuments({ "variantes.talle": "M", "variantes.color": "Negro" })
} resultado(s)`);

titulo("5b", "Productos con talle S, M o L ($in)");
mostrar(
  db.productos.find({ "variantes.talle": { $in: ["S", "M", "L"] } }, { nombre: 1 }).sort({ _id: 1 }).toArray(),
  3
);

titulo("5c", "Productos con una sola variante ($size)");
mostrar(
  db.productos.find({ variantes: { $size: 1 } }, { nombre: 1, variantes: 1 }).sort({ _id: 1 }).toArray(),
  3
);

// ---------------------------------------------------------------------------------------------
// Consulta 6 · Proyección, ordenamiento y límite
// Pregunta: ¿cuáles son los 5 productos publicados más nuevos? Solo nombre y precio.
// Objetivo: la sección "Novedades" de la tienda.
// Resultado esperado: 5 productos, del creado más recientemente hacia atrás, solo con nombre y
// precio (sin _id). Se desempata por _id para que el orden sea siempre el mismo.
titulo(6, "Los 5 productos publicados más nuevos");
mostrar(
  db.productos.find(
    { publicado: true },
    { _id: 0, nombre: 1, precio: { $divide: ["$precioBase.monto", 100] } }
  ).sort({ creadoEn: -1, _id: 1 }).limit(5).toArray()
);

// ---------------------------------------------------------------------------------------------
// Consulta 7 · Pipeline de agregación ($lookup, $addFields, $project)
// Pregunta: ¿cuál es el precio vigente de cada producto, con su promoción?
// Objetivo: el cálculo de PROYECTO.md 5.7 hecho en la base. Por cada producto busca en
// "promociones" las vigentes que lo alcanzan (por id, por categoría o a toda la tienda) y que
// valen para el medio de pago; se queda con la de mayor porcentaje (no se acumulan, D3) y trunca
// a centavos enteros (D5).
// Resultado esperado: de los 205 productos publicados, 79 tienen una promoción vigente pagando por
// transferencia (el medio por defecto, D4). Por ejemplo, el Body de tiras liso: $23.490 → $16.443.
//   7b) El Jean wide leg elastizado está en "Semana del jean" (20%) y en la liquidación (30%): gana la
//       de 30%, $45.490 → $31.843.
//   7c) La remera básica: por transferencia no tiene promoción ($15.000, como en el contrato);
//       en efectivo aplica "10% pagando en efectivo" ($13.500).
function precioVigente(fecha, medioPago) {
  return [
    {
      $lookup: {
        from: "promociones",
        let: { productoId: "$_id", categoria: "$categoria" },
        pipeline: [
          {
            $match: {
              $expr: {
                $and: [
                  { $lte: ["$vigencia.desde", fecha] },
                  { $gte: ["$vigencia.hasta", fecha] },
                  {
                    $or: [
                      { $eq: ["$alcance.tipo", "TIENDA"] },
                      { $and: [{ $eq: ["$alcance.tipo", "CATEGORIA"] }, { $in: ["$$categoria", "$alcance.ids"] }] },
                      { $and: [{ $eq: ["$alcance.tipo", "PRODUCTO"] }, { $in: ["$$productoId", "$alcance.ids"] }] },
                    ],
                  },
                  { $or: [{ $eq: ["$tipo", "PORCENTAJE"] }, { $eq: ["$medioPago", medioPago] }] },
                ],
              },
            },
          },
          { $sort: { porcentaje: -1, _id: 1 } }, // gana la de mayor porcentaje (D3)
          { $limit: 1 },
          { $project: { nombre: 1, porcentaje: 1 } },
        ],
        as: "promocion",
      },
    },
    { $addFields: { promocion: { $first: "$promocion" } } },
    {
      $addFields: {
        precioVigente: {
          $cond: [
            { $ifNull: ["$promocion", false] },
            // En centavos enteros y truncado: monto × (100 − porcentaje) / 100 (D5).
            { $toLong: { $trunc: { $divide: [{ $multiply: ["$precioBase.monto", { $subtract: [100, "$promocion.porcentaje"] }] }, 100] } } },
            "$precioBase.monto",
          ],
        },
      },
    },
    {
      $project: {
        nombre: 1,
        categoria: 1,
        precioBase: { $divide: ["$precioBase.monto", 100] },
        precioVigente: { $divide: ["$precioVigente", 100] },
        promocion: "$promocion.nombre",
        porcentaje: "$promocion.porcentaje",
      },
    },
  ];
}

titulo(7, "Precio vigente de cada producto publicado, pagando por transferencia");
const vigentes = db.productos.aggregate([
  { $match: { publicado: true } },
  ...precioVigente(HOY, "TRANSFERENCIA"),
  { $sort: { _id: 1 } },
]).toArray();
const conPromocion = vigentes.filter((p) => p.promocion);
print(`${vigentes.length} productos publicados, ${conPromocion.length} con promoción vigente`);
mostrar(conPromocion);

titulo("7b", "Dos promociones sobre el mismo jean: gana la mayor (D3)");
const jeanEnLiquidacion = db.promociones.findOne({ _id: "promo-liquidacion" }).alcance.ids
  .find((id) => db.productos.findOne({ _id: id }).categoria === "jeans");
mostrar(db.productos.aggregate([{ $match: { _id: jeanEnLiquidacion } }, ...precioVigente(HOY, "TRANSFERENCIA")]).toArray());

titulo("7c", "La remera básica según el medio de pago");
for (const medio of ["TRANSFERENCIA", "EFECTIVO"]) {
  const [r] = db.productos.aggregate([{ $match: { _id: "prod-remera-basica" } }, ...precioVigente(HOY, medio)]).toArray();
  print(`  ${medio}: base $${r.precioBase} → vigente $${r.precioVigente}${r.promocion ? ` (${r.promocion}, ${r.porcentaje}%)` : " (sin promoción)"}`);
}

// ---------------------------------------------------------------------------------------------
// Consulta 8 · Pipeline de agregación ($unwind, $group, $sort)
// Pregunta: ¿qué colores se ofrecen más?
// Objetivo: ver la variedad de colores del catálogo publicado, para decidir qué reponer.
// Resultado esperado: los 10 colores con más variantes publicadas, encabezados por Azul y Negro
// (64 variantes cada uno). $unwind abre el arreglo (un documento por variante) y $group cuenta
// variantes y productos distintos por color.
titulo(8, "Colores más ofrecidos (variantes publicadas)");
mostrar(
  db.productos.aggregate([
    { $match: { publicado: true } },
    { $unwind: "$variantes" },
    { $group: { _id: "$variantes.color", variantes: { $sum: 1 }, productos: { $addToSet: "$_id" } } },
    { $project: { variantes: 1, productos: { $size: "$productos" } } },
    { $sort: { variantes: -1, _id: 1 } },
    { $limit: 10 },
  ]).toArray(),
  10
);

// ---------------------------------------------------------------------------------------------
// Consulta 9 · Agrupamiento y cálculos estadísticos
// Pregunta: por categoría, ¿cuántos productos hay y cuál es el precio promedio, mínimo y máximo?
// Objetivo: comparar el surtido y el nivel de precios entre categorías. El $lookup resuelve la
// referencia a "categorias" para mostrar el nombre.
// Resultado esperado: las 13 categorías, ordenadas por precio promedio, de camperas ($81.302,22)
// a accesorios ($10.771,67). Incluye despublicados: el mínimo de accesorios ($1.000) es un caso
// límite que no está publicado.
titulo(9, "Cantidad y precios por categoría");
mostrar(
  db.productos.aggregate([
    {
      $group: {
        _id: "$categoria",
        productos: { $sum: 1 },
        publicados: { $sum: { $cond: ["$publicado", 1, 0] } },
        variantes: { $sum: { $size: "$variantes" } },
        promedio: { $avg: "$precioBase.monto" },
        minimo: { $min: "$precioBase.monto" },
        maximo: { $max: "$precioBase.monto" },
      },
    },
    { $lookup: { from: "categorias", localField: "_id", foreignField: "_id", as: "categoria" } },
    {
      $project: {
        categoria: { $first: "$categoria.nombre" },
        productos: 1,
        publicados: 1,
        variantes: 1,
        promedio: { $round: [{ $divide: ["$promedio", 100] }, 2] },
        minimo: { $divide: ["$minimo", 100] },
        maximo: { $divide: ["$maximo", 100] },
      },
    },
    { $sort: { promedio: -1 } },
  ]).toArray(),
  13
);

// ---------------------------------------------------------------------------------------------
// Consulta 10 · Adicional de complejidad elegida por el equipo ($facet)
// Pregunta: ¿qué categorías, talles, colores y rango de precios hay, para armar los filtros de la
// tienda?
// Objetivo: con una sola lectura del catálogo publicado, calcular cuatro resúmenes distintos.
// $facet corre varios pipelines sobre los mismos documentos y devuelve un solo resultado.
// Resultado esperado: un documento con 4 listas: 13 categorías, 19 talles, 18 colores y el
// precio publicado mínimo ($2.990) y máximo ($180.000).
titulo(10, "Opciones de filtro de la tienda ($facet)");
const [filtros] = db.productos.aggregate([
  { $match: { publicado: true } },
  {
    $facet: {
      categorias: [{ $group: { _id: "$categoria", productos: { $sum: 1 } } }, { $sort: { _id: 1 } }],
      talles: [
        { $unwind: "$variantes" },
        { $group: { _id: "$variantes.talle", productos: { $addToSet: "$_id" } } },
        { $project: { productos: { $size: "$productos" } } },
        { $sort: { _id: 1 } },
      ],
      colores: [
        { $unwind: "$variantes" },
        { $group: { _id: "$variantes.color", productos: { $addToSet: "$_id" } } },
        { $project: { productos: { $size: "$productos" } } },
        { $sort: { productos: -1, _id: 1 } },
      ],
      precios: [
        { $group: { _id: null, minimo: { $min: "$precioBase.monto" }, maximo: { $max: "$precioBase.monto" } } },
        { $project: { _id: 0, minimo: { $divide: ["$minimo", 100] }, maximo: { $divide: ["$maximo", 100] } } },
      ],
    },
  },
]).toArray();
for (const [faceta, valores] of Object.entries(filtros)) {
  print(`  ${faceta} (${valores.length}): ${JSON.stringify(valores)}`);
}

// ---------------------------------------------------------------------------------------------
// Extra · Búsqueda de texto (extensión opcional de la consigna)
// Pregunta: ¿qué productos publicados mencionan "lino" en el nombre o la descripción?
// Objetivo: el buscador de la tienda. Usa el índice de texto en español: separa palabras y las
// reduce a su raíz, así "remeras" también encuentra "Remera".
// Resultado esperado: 33 productos publicados, los más relevantes primero (textScore). Y
// "remeras" encuentra 26 productos aunque los nombres dicen "Remera".
titulo("extra", "Búsqueda de texto: \"lino\"");
mostrar(
  db.productos.find(
    { $text: { $search: "lino" }, publicado: true },
    { nombre: 1, relevancia: { $meta: "textScore" } }
  ).sort({ relevancia: { $meta: "textScore" }, _id: 1 }).toArray()
);
print(`"remeras" (plural) encuentra ${db.productos.countDocuments({ $text: { $search: "remeras" } })} productos ` +
  `aunque los nombres dicen "Remera" en singular`);
