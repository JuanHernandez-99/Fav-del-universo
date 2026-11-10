// Prueba de la validación de esquema ($jsonSchema): cada inserción inválida tiene que ser rechazada.
// Uso, desde la raíz del repo:
//   docker compose exec -T mongo-catalogo sh -c 'mongosh --quiet -u "$MONGO_INITDB_ROOT_USERNAME" -p "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin catalogo' < services/catalogo/pruebas/validacion_esquema.js
const base = { _id: "prod-x", nombre: "X", categoria: "remeras", sistemaTalles: "LETRAS", precioBase: { monto: NumberLong("100"), moneda: "ARS" }, publicado: false, variantes: [{ id: "var-x-b-m", talle: "M", color: "B" }], creadoEn: new Date(), actualizadoEn: new Date(), version: 1 };
const casos = {
  "precio decimal": { ...base, precioBase: { monto: 15.5, moneda: "ARS" } },
  "sin variantes": { ...base, variantes: [] },
  "talle inexistente": { ...base, variantes: [{ id: "v", talle: "XXXL", color: "B" }] },
  "moneda USD": { ...base, precioBase: { monto: NumberLong("100"), moneda: "USD" } },
  "sistema inválido": { ...base, sistemaTalles: "RARO" },
};
for (const [n, d] of Object.entries(casos)) { try { db.productos.insertOne(d); print("ACEPTADO (mal): " + n); db.productos.deleteOne({ _id: d._id }); } catch (e) { print("rechazado OK: " + n + " (código " + e.code + ")"); } }
try { db.promociones.insertOne({ _id: "p", nombre: "n", tipo: "PORCENTAJE", porcentaje: 10, medioPago: null, vigencia: { desde: new Date("2026-12-01"), hasta: new Date("2026-11-01") }, alcance: { tipo: "TIENDA", ids: [] } }); print("ACEPTADO (mal): vigencia invertida"); db.promociones.deleteOne({_id:"p"}); } catch (e) { print("rechazado OK: vigencia invertida"); }
try { db.promociones.insertOne({ _id: "p", nombre: "n", tipo: "MEDIO_PAGO", porcentaje: 10, medioPago: null, vigencia: { desde: new Date("2026-11-01"), hasta: new Date("2026-12-01") }, alcance: { tipo: "TIENDA", ids: [] } }); print("ACEPTADO (mal): MEDIO_PAGO sin medio"); db.promociones.deleteOne({_id:"p"}); } catch (e) { print("rechazado OK: MEDIO_PAGO sin medioPago"); }
