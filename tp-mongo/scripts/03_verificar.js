// 03 · Verifica cantidades y consistencia básica de los datos cargados (PROYECTO.md sección 8).
// Cada chequeo es una consulta. Si alguno falla, termina con código 1 y el cargador falla.
// Uso: mongosh -u ... -p ... --authenticationDatabase admin catalogo --file 03_verificar.js

const TALLES = {
  LETRAS: ["XS", "S", "M", "L", "XL", "XXL"],
  NUMERICO_PAR: ["32", "34", "36", "38", "40", "42", "44", "46"],
  NUMERICO_1_4: ["1", "2", "3", "4"],
  UNICO: ["U"],
};

let errores = 0;
function chequear(descripcion, problemas) {
  if (problemas.length === 0) {
    print(`OK    ${descripcion}`);
  } else {
    errores++;
    print(`FALLA ${descripcion}: ${JSON.stringify(problemas.slice(0, 5))}${problemas.length > 5 ? " ..." : ""}`);
  }
}

// Cantidades
const cantidades = {
  categorias: db.categorias.countDocuments(),
  productos: db.productos.countDocuments(),
  promociones: db.promociones.countDocuments(),
};
const variantes = db.productos.aggregate([{ $unwind: "$variantes" }, { $count: "n" }]).toArray()[0]?.n ?? 0;
const total = cantidades.categorias + cantidades.productos + cantidades.promociones;
print(`documentos: ${JSON.stringify(cantidades)} · total ${total} · variantes ${variantes}`);
chequear("al menos 100 documentos (consigna 5.2)", total >= 100 ? [] : [`hay ${total}`]);

// Productos
const categorias = db.categorias.distinct("_id");
chequear("toda categoría usada existe en categorias",
  db.productos.distinct("categoria").filter((c) => !categorias.includes(c)));

chequear("todo producto tiene al menos una variante (RN-01)",
  db.productos.find({ $or: [{ variantes: { $exists: false } }, { variantes: { $size: 0 } }] }, { _id: 1 })
    .toArray().map((p) => p._id));

chequear("precioBase.monto es entero largo y mayor a 0 (D13)",
  db.productos.find({ $or: [{ "precioBase.monto": { $not: { $type: "long" } } }, { "precioBase.monto": { $lte: 0 } }] },
    { _id: 1 }).toArray().map((p) => p._id));

// El índice único evita ids repetidos entre productos, pero no dentro del mismo arreglo (5.9).
chequear("ids de variante únicos en todo el catálogo",
  db.productos.aggregate([
    { $unwind: "$variantes" },
    { $group: { _id: "$variantes.id", n: { $sum: 1 } } },
    { $match: { n: { $gt: 1 } } },
  ]).toArray().map((v) => v._id));

chequear("sin talle y color repetidos dentro de un producto (HU-03.4)",
  db.productos.aggregate([
    { $unwind: "$variantes" },
    { $group: { _id: { p: "$_id", talle: "$variantes.talle", color: "$variantes.color" }, n: { $sum: 1 } } },
    { $match: { n: { $gt: 1 } } },
  ]).toArray().map((v) => v._id));

const tallesFuera = [];
db.productos.find({}, { sistemaTalles: 1, variantes: 1 }).forEach((p) => {
  const validos = TALLES[p.sistemaTalles];
  if (!validos) return tallesFuera.push(`${p._id}: sistema ${p.sistemaTalles}`);
  p.variantes.filter((v) => !validos.includes(v.talle)).forEach((v) => tallesFuera.push(v.id));
});
chequear("cada talle pertenece al sistema de su producto (RN-02)", tallesFuera);

chequear("ids de variante de hasta 100 caracteres",
  db.productos.aggregate([
    { $unwind: "$variantes" },
    { $match: { $expr: { $gt: [{ $strLenCP: "$variantes.id" }, 100] } } },
  ]).toArray().map((v) => v.variantes.id));

// Promociones
chequear("vigencia con desde < hasta (HU-05.2)",
  db.promociones.find({ $expr: { $gte: ["$vigencia.desde", "$vigencia.hasta"] } }, { _id: 1 })
    .toArray().map((p) => p._id));

chequear("MEDIO_PAGO lleva medioPago y PORCENTAJE no (RN-05)",
  db.promociones.find({ $or: [
    { tipo: "MEDIO_PAGO", medioPago: { $nin: ["EFECTIVO", "TRANSFERENCIA", "MERCADO_PAGO"] } },
    { tipo: "PORCENTAJE", medioPago: { $ne: null } },
  ] }, { _id: 1 }).toArray().map((p) => p._id));

const alcanceRoto = [];
db.promociones.find({ "alcance.tipo": { $in: ["CATEGORIA", "PRODUCTO"] } }).forEach((p) => {
  const coleccion = p.alcance.tipo === "CATEGORIA" ? db.categorias : db.productos;
  p.alcance.ids.filter((id) => coleccion.countDocuments({ _id: id }) === 0)
    .forEach((id) => alcanceRoto.push(`${p._id} → ${id}`));
});
chequear("los ids del alcance de cada promoción existen", alcanceRoto);

print(errores === 0 ? "verificación OK" : `verificación con ${errores} falla(s)`);
if (errores > 0) quit(1);
