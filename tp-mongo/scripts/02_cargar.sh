#!/bin/sh
# 02 · Carga los datos de prueba de datos/*.json (los genera datos/generar.py).
# Vacía cada colección y la vuelve a llenar, así se puede repetir sin duplicar. No usa
# `mongoimport --drop` porque eso borraría también los índices que crea 01.
#
# CARGA=completa (por defecto): los 2 productos del contrato + el volumen del TP + promociones.
# CARGA=minimo: solo categorías y los 2 productos del contrato, lo que usa FAV (PROYECTO.md sección 8).
set -eu

CARGA="${CARGA:-completa}"
URI="mongodb://mongo-catalogo:27017/${MONGO_DB}?authSource=admin"

importar() { # colección archivo
  mongoimport --uri "$URI" -u "$MONGO_INITDB_ROOT_USERNAME" -p "$MONGO_INITDB_ROOT_PASSWORD" \
    --collection "$1" --file "$2" --jsonArray --stopOnError
}

mongosh "$URI" -u "$MONGO_INITDB_ROOT_USERNAME" -p "$MONGO_INITDB_ROOT_PASSWORD" --quiet --eval '
  for (const c of ["categorias", "productos", "promociones"]) {
    print(`${c}: ${db[c].deleteMany({}).deletedCount} documentos borrados`);
  }'

importar categorias /datos/categorias.json
importar productos /datos/productos_minimo.json
if [ "$CARGA" = "completa" ]; then
  importar productos /datos/productos.json
  importar promociones /datos/promociones.json
fi
echo "carga $CARGA terminada"
