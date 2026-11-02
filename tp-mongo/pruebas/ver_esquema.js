// Muestra el validador $jsonSchema que MongoDB tiene guardado en cada colección (lo instala el script 01).
// Uso, desde tp-mongo/:
//   docker compose exec mongo-catalogo sh -c 'mongosh --quiet -u "$MONGO_INITDB_ROOT_USERNAME" -p "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin catalogo --file /pruebas/ver_esquema.js'

for (const nombre of ["productos", "promociones"]) {
  const { options } = db.getCollectionInfos({ name: nombre })[0];
  print(`\n=== ${nombre} · validationLevel: ${options.validationLevel}, validationAction: ${options.validationAction}`);
  printjson(options.validator);
}
