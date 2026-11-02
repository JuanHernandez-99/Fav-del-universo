#!/bin/sh
# Corre los scripts en orden. Lo ejecuta el servicio `cargador`: docker compose run --rm cargador
set -eu

URI="mongodb://mongo-catalogo:27017/${MONGO_DB}?authSource=admin"

echo "== 01 · colecciones e índices"
mongosh "$URI" -u "$MONGO_INITDB_ROOT_USERNAME" -p "$MONGO_INITDB_ROOT_PASSWORD" --quiet \
  --file /scripts/01_crear_colecciones_e_indices.js

echo "== 02 · carga de datos"
sh /scripts/02_cargar.sh

echo "== 03 · verificación"
mongosh "$URI" -u "$MONGO_INITDB_ROOT_USERNAME" -p "$MONGO_INITDB_ROOT_PASSWORD" --quiet \
  --file /scripts/03_verificar.js
