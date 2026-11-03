// 01 · Crea las colecciones y los índices del catálogo (DISENO.md 5.3 y 5.8).
// Idempotente: se puede correr varias veces. createIndex no hace nada si el índice ya existe igual.
// Uso: mongosh -u ... -p ... --authenticationDatabase admin catalogo --file 01_crear_colecciones_e_indices.js

const colecciones = ["categorias", "productos", "promociones"];
for (const nombre of colecciones) {
  if (!db.getCollectionNames().includes(nombre)) {
    db.createCollection(nombre);
    print(`colección creada: ${nombre}`);
  }
}

// ---------- Validación de esquema ($jsonSchema) ----------
// Segunda barrera: la base rechaza lo que no cumple aunque la escritura no pase por la API (un
// import, mongosh a mano). Es el mismo contrato que valida el servicio Go; si cambia uno, se cambia
// el otro. Los talles de TALLES son los de `modelo.SistemasTalles` (Go): un test del servicio compara
// las dos listas. Lo que depende de la base o del cruce de campos (que la categoría exista, que el
// talle sea del sistema de ese producto) lo siguen controlando el servicio y 03_verificar.js.
const TALLES = {
  LETRAS: ["XS", "S", "M", "L", "XL", "XXL"],
  NUMERICO_PAR: ["32", "34", "36", "38", "40", "42", "44", "46"],
  NUMERICO_1_4: ["1", "2", "3", "4"],
  UNICO: ["U"],
};
const MEDIOS_DE_PAGO = ["EFECTIVO", "TRANSFERENCIA", "MERCADO_PAGO"];
const texto = { bsonType: "string", minLength: 1 };

const validadorProductos = {
  $jsonSchema: {
    bsonType: "object",
    required: ["_id", "nombre", "categoria", "sistemaTalles", "precioBase", "publicado", "variantes", "creadoEn", "actualizadoEn", "version"],
    properties: {
      _id: { bsonType: "string", maxLength: 100 }, // el límite de VARCHAR(100) de Inventario
      nombre: texto,
      descripcion: { bsonType: "string" },
      categoria: texto,
      sistemaTalles: { enum: Object.keys(TALLES) },
      precioBase: {
        bsonType: "object",
        required: ["monto", "moneda"],
        properties: {
          monto: { bsonType: "long", minimum: 1 }, // centavos enteros, nunca decimales (D13)
          moneda: { enum: ["ARS"] },
        },
      },
      publicado: { bsonType: "bool" },
      atributos: { bsonType: "object" },
      fotos: {
        bsonType: "array",
        items: {
          bsonType: "object",
          required: ["url"],
          properties: { url: texto, color: { bsonType: ["string", "null"] } },
        },
      },
      variantes: {
        bsonType: "array",
        minItems: 1, // RN-01
        items: {
          bsonType: "object",
          required: ["id", "talle", "color"],
          properties: {
            id: { bsonType: "string", minLength: 1, maxLength: 100 },
            talle: { enum: [...new Set(Object.values(TALLES).flat())] },
            color: texto,
          },
        },
      },
      creadoEn: { bsonType: "date" },
      actualizadoEn: { bsonType: "date" },
      version: { bsonType: ["int", "long"], minimum: 1 },
    },
  },
};

const validadorPromociones = {
  $and: [
    {
      $jsonSchema: {
        bsonType: "object",
        required: ["nombre", "tipo", "porcentaje", "vigencia", "alcance"],
        properties: {
          nombre: texto,
          tipo: { enum: ["PORCENTAJE", "MEDIO_PAGO"] },
          porcentaje: { bsonType: "number", minimum: 1, maximum: 100 },
          medioPago: { enum: [...MEDIOS_DE_PAGO, null] },
          vigencia: {
            bsonType: "object",
            required: ["desde", "hasta"], // la vigencia es obligatoria (HU-05.2)
            properties: { desde: { bsonType: "date" }, hasta: { bsonType: "date" } },
          },
          alcance: {
            bsonType: "object",
            required: ["tipo", "ids"],
            properties: { tipo: { enum: ["PRODUCTO", "CATEGORIA", "TIENDA"] }, ids: { bsonType: "array" } },
          },
        },
      },
    },
    // Reglas que cruzan campos: $jsonSchema no puede, $expr sí.
    { $expr: { $lt: ["$vigencia.desde", "$vigencia.hasta"] } },
    { $expr: { $or: [{ $ne: ["$tipo", "MEDIO_PAGO"] }, { $in: ["$medioPago", MEDIOS_DE_PAGO] }] } },
  ],
};

// collMod en vez de createCollection con validador: también sirve si la colección ya existía (por
// ejemplo, si la API arrancó antes y creó el índice único) y se puede repetir.
for (const [coleccion, validator] of [["productos", validadorProductos], ["promociones", validadorPromociones]]) {
  db.runCommand({ collMod: coleccion, validator, validationLevel: "strict", validationAction: "error" });
  print(`validación de esquema: ${coleccion}`);
}

// Consulta por lote de variantes (Ventas y panel). Único: un id de variante no se repite en todo el
// catálogo. Es multikey porque variantes es un arreglo: hay una entrada por cada variante.
db.productos.createIndex({ "variantes.id": 1 }, { name: "variantes_id_unico", unique: true });

// Listado de la tienda: publicados, por categoría y rango de precio, ordenado por precio.
db.productos.createIndex(
  { publicado: 1, categoria: 1, "precioBase.monto": 1 },
  { name: "publicado_categoria_precio" }
);

// Búsqueda por texto en nombre y descripción. Una colección admite un solo índice de texto.
db.productos.createIndex(
  { nombre: "text", descripcion: "text" },
  { name: "texto_nombre_descripcion", default_language: "spanish" }
);

// Filtro por talle y color. Los dos campos están dentro del mismo arreglo, por eso se permite el
// índice compuesto multikey.
db.productos.createIndex(
  { "variantes.talle": 1, "variantes.color": 1 },
  { name: "variantes_talle_color" }
);

// Promociones vigentes en una fecha.
db.promociones.createIndex({ "vigencia.desde": 1, "vigencia.hasta": 1 }, { name: "vigencia" });

// Promociones que alcanzan a un producto, a su categoría o a toda la tienda.
db.promociones.createIndex({ "alcance.tipo": 1, "alcance.ids": 1 }, { name: "alcance" });

for (const nombre of colecciones) {
  print(`${nombre}: ${db[nombre].getIndexes().map((i) => i.name).join(", ")}`);
}
