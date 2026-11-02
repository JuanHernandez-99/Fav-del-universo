// 01 · Crea las colecciones y los índices del catálogo (PROYECTO.md 5.3 y 5.8).
// Idempotente: se puede correr varias veces. createIndex no hace nada si el índice ya existe igual.
// Uso: mongosh -u ... -p ... --authenticationDatabase admin catalogo --file 01_crear_colecciones_e_indices.js

const colecciones = ["categorias", "productos", "promociones"];
for (const nombre of colecciones) {
  if (!db.getCollectionNames().includes(nombre)) {
    db.createCollection(nombre);
    print(`colección creada: ${nombre}`);
  }
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
