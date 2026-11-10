#!/usr/bin/env bash
# Escalones del pico de la tienda (ADR-013): corre loadtest/tienda.js a cada tasa y, a mitad de cada escalón, toma la
# CPU de los contenedores. Muestra en qué tasa se rompe el objetivo (p95 < 300 ms, menos de 1 % de errores) y qué
# se satura primero. Requiere el sistema levantado (docker compose up -d), Docker y Node.
#
#   bash loadtest/pico_tienda.sh                       # 50 100 200 400 800 1200 pedidos por segundo, 60 s cada una
#   TASAS="300 600" DURACION=30s bash loadtest/pico_tienda.sh
set -u

TASAS="${TASAS:-50 100 200 400 800 1200}"
DURACION="${DURACION:-60s}"
CARPETA="$(cd "$(dirname "$0")" && pwd)"
SALIDA="${SALIDA:-$(mktemp -d)}"
SEGUNDOS=$(echo "$DURACION" | tr -d 's')
MYSQL_ROOT="${INVENTARIO_DB_ROOT_PASSWORD:-cambiar}"
# En Git Bash (Windows), Docker y Node necesitan la ruta de Windows, y Git Bash no tiene que reescribir la de Docker.
MONTAJE="$SALIDA"
command -v cygpath > /dev/null && MONTAJE="$(cygpath -w "$SALIDA")"

echo "Resultados crudos en $SALIDA"

# Esperas por bloqueo de filas en el MySQL de Inventario: si suben, hay contención sobre el stock (hipótesis 1).
esperas_bloqueo() {
  docker compose exec -T mysql-inventario mysql -uroot -p"$MYSQL_ROOT" -N -e \
    "SELECT variable_value FROM performance_schema.global_status WHERE variable_name IN ('Innodb_row_lock_waits','Innodb_row_lock_time') ORDER BY variable_name DESC" 2>/dev/null | tr -d '\r' | paste -sd' '
}

for TASA in $TASAS; do
  echo
  echo "=== $TASA pedidos por segundo durante $DURACION"
  read -r ESPERAS_ANTES MS_ANTES <<< "$(esperas_bloqueo)"
  MSYS_NO_PATHCONV=1 docker run --rm -i -e TASA="$TASA" -e DURACION="$DURACION" -v "$MONTAJE:/salida" grafana/k6 run --quiet \
    --summary-export "/salida/tasa-$TASA.json" - < "$CARPETA/tienda.js" > "$SALIDA/tasa-$TASA.txt" 2>&1 &
  K6=$!
  # El setup carga stock unos segundos; se mide la CPU con la carga ya estable.
  sleep $((SEGUNDOS / 2 + 5))
  docker stats --no-stream --format '{{.Name}};{{.CPUPerc}};{{.MemUsage}}' > "$SALIDA/cpu-$TASA.txt"
  wait $K6
  read -r ESPERAS_DESPUES MS_DESPUES <<< "$(esperas_bloqueo)"

  node -e '
    const r = require(process.argv[1]).metrics, m = (n) => r[n] || {};
    const f = (x) => (x === undefined ? "-" : x.toFixed(0));
    console.log(`pedidos/s logrados: ${r.http_reqs.rate.toFixed(0)} · errores: ${(r.http_req_failed.value * 100).toFixed(2)} %` +
      ` · sin enviar (k6 sin usuarios libres): ${r.dropped_iterations ? r.dropped_iterations.count : 0}`);
    console.log(`p95 total ${f(r.http_req_duration["p(95)"])} ms · p99 ${f(r.http_req_duration["p(99)"])} ms`);
    console.log("p95 por operación (ms): " + ["verFicha", "verListado", "buscar", "verDisponibles", "comprar"]
      .map((o) => `${o} ${f(m(`http_req_duration{operacion:${o}}`)["p(95)"])}`).join(" · "));
    console.log(`p95 de la compra (ms): variante popular ${f(m("http_req_duration{variante:popular}")["p(95)"])}` +
      ` · otras ${f(m("http_req_duration{variante:otra}")["p(95)"])}`);
  ' "$MONTAJE/tasa-$TASA.json"
  echo "esperas por bloqueo en MySQL de Inventario: $((ESPERAS_DESPUES - ESPERAS_ANTES)) ($((MS_DESPUES - MS_ANTES)) ms en total)"
  awk -F';' '{gsub("%","",$2); total += $2} END {printf "CPU de todos los contenedores: %.0f %% (100 %% = un núcleo)\n", total}' "$SALIDA/cpu-$TASA.txt"
  echo "CPU más alta:"
  sed 's/fav-deluniverso-//' "$SALIDA/cpu-$TASA.txt" | sort -t';' -k2 -rn | head -6 | awk -F';' '{printf "  %-24s %8s  %s\n", $1, $2, $3}'
  sleep 10 # que se vacíen las colas antes del escalón siguiente
done
