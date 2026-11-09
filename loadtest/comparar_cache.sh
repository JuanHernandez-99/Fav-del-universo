#!/usr/bin/env bash
# Compara las lecturas de la tienda con y sin caché (ADR-007), sobre el mismo código: mismas tasas, dos veces cada una,
# alternando el orden para que el ruido de la máquina no favorezca a un lado. Requiere el sistema levantado.
#
#   bash loadtest/comparar_cache.sh
set -u

TASAS="${TASAS:-300 600 1000}"
REPETICIONES="${REPETICIONES:-2}"
export DURACION="${DURACION:-30s}"
CARPETA="$(cd "$(dirname "$0")" && pwd)"

configurar() {
  CACHE_ENABLED="$1" docker compose up -d --no-deps catalogo >/dev/null 2>&1
  until [ "$(docker compose ps --format '{{.Service}} {{.Status}}' | grep -c '^catalogo .*(healthy)')" = 2 ]; do sleep 1; done
  sleep 6 # que el gateway vuelva a resolver las réplicas
}

medir() { # tasa modo
  configurar "$2"
  # Calienta la caché, para no comparar el arranque en frío. Con la caché apagada solo es la misma carga inicial.
  DURACION=5s bash "$CARPETA/medir_catalogo.sh" 100 >/dev/null 2>&1
  echo "--- tasa $1, caché $2"
  bash "$CARPETA/medir_catalogo.sh" "$1"
  if [ "$2" = true ]; then
    docker compose exec -T gateway sh -c 'for i in 1 2; do wget -qO- http://catalogo:8080/metrics; done' 2>/dev/null | grep -E '^cache_operaciones_total' | awk '{split($1,a,"resultado=\""); split(a[2],b,"\""); s[b[1]]+=$2} END {for (k in s) printf "%s=%d ", k, s[k]; print ""}' | sed 's/^/operaciones de caché (acumuladas, una réplica al azar por pedido): /'
  fi
}

for repeticion in $(seq 1 "$REPETICIONES"); do
  for tasa in $TASAS; do
    if [ $((repeticion % 2)) = 1 ]; then orden="false true"; else orden="true false"; fi
    for modo in $orden; do medir "$tasa" "$modo"; done
  done
done
configurar true
