#!/bin/sh
# Prepara la base de Catálogo. Lo corre el servicio `catalogo-init` del Compose cada vez que se levanta:
#   1. Siempre: colecciones, esquema e índices (01). Es idempotente y no toca los datos.
#   2. Solo si `productos` está vacía, o con RECARGAR=true: carga los datos de ejemplo (02) y los verifica (03).
# Así, levantar de nuevo nunca borra lo que cargó el administrador.
#   CARGA=minimo (por defecto en FAV) | completa     RECARGAR=true vacía las colecciones y vuelve a cargar
set -eu

URI="mongodb://mongo-catalogo:27017/${MONGO_DB}?authSource=admin"
consola() { mongosh "$URI" -u "$MONGO_INITDB_ROOT_USERNAME" -p "$MONGO_INITDB_ROOT_PASSWORD" --quiet "$@"; }

echo "== 01 · colecciones, esquema e índices"
consola --file /scripts/01_crear_colecciones_e_indices.js

productos=$(consola --eval 'db.productos.countDocuments()')
if [ "$productos" != "0" ] && [ "${RECARGAR:-false}" != "true" ]; then
  echo "== la base ya tiene $productos productos: no se cargan datos (RECARGAR=true para vaciar y recargar)"
  exit 0
fi

echo "== 02 · carga de datos (${CARGA:-completa})"
sh /scripts/02_cargar.sh

echo "== 03 · verificación"
consola --file /scripts/03_verificar.js
