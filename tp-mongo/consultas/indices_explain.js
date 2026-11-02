// Análisis de los índices con explain("executionStats") (consigna 5.5 y PROYECTO.md 5.8).
// Para cada índice corre una consulta real del dominio dos veces: sin índice (hint $natural obliga
// a leer toda la colección, COLLSCAN) y con el plan que elige Mongo (IXSCAN). Compara claves y
// documentos examinados contra documentos devueltos. La interpretación está en informe/03-indices.md.
//
// Uso (desde tp-mongo/, con los datos cargados):
//   docker compose exec mongo-catalogo sh -c 'mongosh --quiet -u "$MONGO_INITDB_ROOT_USERNAME" -p "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin catalogo --file /consultas/indices_explain.js'

db = db.getSiblingDB("catalogo");
const HOY = ISODate("2026-11-03T15:00:00Z");

// Recorre el plan ganador y junta las etapas (de la última a la primera) y los límites de los IXSCAN.
function resumirPlan(explicacion) {
  const etapas = [];
  const limites = [];
  const recorrer = (e) => {
    if (!e) return;
    etapas.push(e.stage + (e.indexName ? `(${e.indexName})` : ""));
    if (e.stage === "IXSCAN") limites.push(e.indexBounds);
    recorrer(e.inputStage);
    (e.inputStages || []).forEach(recorrer);
  };
  const ganador = explicacion.queryPlanner.winningPlan;
  recorrer(ganador.queryPlan || ganador);
  const s = explicacion.executionStats;
  return {
    plan: etapas.join(" <- "),
    claves: s.totalKeysExamined,
    documentos: s.totalDocsExamined,
    devueltos: s.nReturned,
    ms: s.executionTimeMillis,
    limites,
  };
}

function fila(etiqueta, r) {
  print(`  ${etiqueta.padEnd(11)} ${r.plan}`);
  print(`  ${"".padEnd(11)} claves examinadas: ${r.claves} · documentos examinados: ${r.documentos} · devueltos: ${r.devueltos} · ${r.ms} ms`);
}

function comparar(numero, titulo, consulta, { conLimites = false } = {}) {
  print(`\n=== ${numero}. ${titulo}`);
  const sin = resumirPlan(consulta().hint({ $natural: 1 }).explain("executionStats"));
  const con = resumirPlan(consulta().explain("executionStats"));
  fila("SIN índice", sin);
  fila("CON índice", con);
  if (conLimites) print(`  límites del índice: ${JSON.stringify(con.limites)}`);
  return con;
}

print(`productos: ${db.productos.countDocuments()} documentos · promociones: ${db.promociones.countDocuments()} documentos`);

// 1 · variantes_id_unico — la consulta por lote de Ventas y del panel (timeout de 1 s).
comparar(1, "variantes_id_unico · ¿qué es var-jean-mom-celeste-38?", () =>
  db.productos.find({ "variantes.id": "var-jean-mom-celeste-38" }));
comparar("1b", "variantes_id_unico · consulta por lote de 3 variantes ($in)", () =>
  db.productos.find({ "variantes.id": { $in: ["var-remera-basica-blanca-m", "var-remera-basica-negra-l", "var-jean-mom-celeste-38"] } }));

// 2 · publicado_categoria_precio — listado de la tienda. Regla ESR: igualdad, orden, rango.
comparar(2, "publicado_categoria_precio · jeans publicados entre $20.000 y $40.000, por precio", () =>
  db.productos.find({ publicado: true, categoria: "jeans", "precioBase.monto": { $gte: 2000000, $lte: 4000000 } })
    .sort({ "precioBase.monto": 1 }));
comparar("2b", "publicado_categoria_precio · el mismo listado desempatado por _id (como la API)", () =>
  db.productos.find({ publicado: true, categoria: "jeans", "precioBase.monto": { $gte: 2000000, $lte: 4000000 } })
    .sort({ "precioBase.monto": 1, _id: 1 }));
comparar("2c", "publicado_categoria_precio · sin filtrar categoría (consulta 1): se saltea un campo del índice", () =>
  db.productos.find({ publicado: true, "precioBase.monto": { $gte: 1000000, $lte: 3000000 } }), { conLimites: true });

// 3 · texto_nombre_descripcion — el buscador. $text exige el índice, así que no se puede forzar
// COLLSCAN: la alternativa sin índice es una expresión regular, que lee todo y no reduce palabras.
print(`\n=== 3. texto_nombre_descripcion · buscar "lino"`);
fila("SIN índice", resumirPlan(db.productos.find({ $or: [{ nombre: /lino/i }, { descripcion: /lino/i }] }).explain("executionStats")));
fila("CON índice", resumirPlan(db.productos.find({ $text: { $search: "lino" } }).explain("executionStats")));
print(`  "remeras": con $text ${db.productos.countDocuments({ $text: { $search: "remeras" } })} resultados · con /remeras/i ${db.productos.countDocuments({ nombre: /remeras/i })}`);

// 4 · variantes_talle_color — filtro por talle y color. Con $elemMatch el índice acota los dos
// campos; con condiciones sueltas solo puede acotar uno y el otro se revisa en cada documento.
comparar(4, "variantes_talle_color · talle M en negro, con $elemMatch", () =>
  db.productos.find({ variantes: { $elemMatch: { talle: "M", color: "Negro" } } }), { conLimites: true });
comparar("4b", "variantes_talle_color · lo mismo con condiciones sueltas (otra pregunta: 9 resultados)", () =>
  db.productos.find({ "variantes.talle": "M", "variantes.color": "Negro" }), { conLimites: true });

// 5 · vigencia — promociones vigentes hoy (desde <= hoy <= hasta).
comparar(5, "vigencia · promociones vigentes el 03/11/2026", () =>
  db.promociones.find({ "vigencia.desde": { $lte: HOY }, "vigencia.hasta": { $gte: HOY } }), { conLimites: true });

// 6 · alcance — qué promociones alcanzan a una categoría.
comparar(6, "alcance · promociones que alcanzan a los jeans", () =>
  db.promociones.find({ "alcance.tipo": "CATEGORIA", "alcance.ids": "jeans" }));

// Costo: espacio que ocupa cada índice. Además, cada escritura actualiza todos los índices; los
// multikey tienen una entrada por variante (un producto con 24 variantes suma 24 entradas).
print(`\n=== Costo: tamaño de los índices`);
for (const coleccion of ["productos", "promociones"]) {
  const s = db[coleccion].stats();
  print(`  ${coleccion}: datos ${(s.size / 1024).toFixed(1)} KB · índices ${(s.totalIndexSize / 1024).toFixed(1)} KB`);
  for (const [nombre, bytes] of Object.entries(s.indexSizes)) print(`    ${nombre.padEnd(28)} ${(bytes / 1024).toFixed(1)} KB`);
}
const variantes = db.productos.aggregate([{ $unwind: "$variantes" }, { $count: "n" }]).toArray()[0].n;
print(`  variantes_id_unico y variantes_talle_color tienen una entrada por variante: ${variantes} para ${db.productos.countDocuments()} productos`);
