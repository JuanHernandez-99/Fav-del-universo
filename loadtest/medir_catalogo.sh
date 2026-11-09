#!/usr/bin/env bash
# Corre loadtest/catalogo.js a una tasa e informa latencia, errores y cuántas consultas recibió MongoDB (ADR-007).
# Requiere el sistema levantado (docker compose up -d) y Docker.
#
#   bash loadtest/medir_catalogo.sh 200          # 200 pedidos por segundo durante 60 s
#   DURACION=30s bash loadtest/medir_catalogo.sh 500
set -u

TASA="${1:-200}"
DURACION="${DURACION:-60s}"
CARPETA="$(cd "$(dirname "$0")" && pwd)"
MONGO_USUARIO="${MONGO_INITDB_ROOT_USERNAME:-catalogo}"
MONGO_CONTRASENA="${MONGO_INITDB_ROOT_PASSWORD:-cambiar}"

consultas_mongo() {
  docker compose exec -T mongo-catalogo mongosh --quiet -u "$MONGO_USUARIO" -p "$MONGO_CONTRASENA" --authenticationDatabase admin \
    --eval 'const o = db.serverStatus().opcounters; print(Number(o.query) + Number(o.getmore))' | tr -d '\r'
}

ANTES=$(consultas_mongo)
SALIDA=$(docker run --rm -i -e TASA="$TASA" -e DURACION="$DURACION" grafana/k6 run --quiet - < "$CARPETA/catalogo.js" 2>&1)
DESPUES=$(consultas_mongo)

echo "$SALIDA" | grep -E 'http_req_duration|http_req_failed|http_reqs|dropped_iterations|\{ lectura' | sed 's/^ *//'
SEGUNDOS=$(echo "$DURACION" | tr -d 's')
echo "consultas a MongoDB: $((DESPUES - ANTES)) ($(((DESPUES - ANTES) / SEGUNDOS)) por segundo)"
