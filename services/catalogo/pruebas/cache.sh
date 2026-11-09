#!/usr/bin/env bash
# Validación de la caché (ADR-007), por el gateway y con el sistema levantado (docker compose up): invalidación al editar
# y al cambiar una promoción, avalancha de pedidos iguales con la caché vacía y caída de Redis. Requiere curl y Docker.
#
#   bash services/catalogo/pruebas/cache.sh
set -u

BASE="${BASE:-http://localhost:8080}"
EMAIL="${ADMIN_EMAIL:-admin@fav.example}"
CONTRASENA="${ADMIN_PASSWORD:-cambiar-admin-local}"
fallas=0

anotar() { printf '%s  %s\n' "$(date -u +%H:%M:%SZ)" "$*"; }
comprobar() { # descripcion esperado obtenido
  if [ "$2" = "$3" ]; then anotar "OK    $1 ($3)"; else anotar "FALLA $1: se esperaba $2 y dio $3"; fallas=$((fallas + 1)); fi
}
campo() { sed -n "s/.*\"$1\":\"\{0,1\}\([^\",}]*\).*/\1/p" | head -1; }

TOKEN=$(curl -s "$BASE/usuarios/v1/sesiones" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"contrasena\":\"$CONTRASENA\"}" | campo token)
[ -n "$TOKEN" ] || { anotar "No se pudo iniciar sesión como administrador"; exit 1; }
A="Authorization: Bearer $TOKEN"
SUFIJO="$(date +%s)"

metrica() { # suma de una métrica en las dos réplicas, leyendo cada una por su IP
  local total=0 ip valor
  for ip in $(docker compose ps -q catalogo | xargs docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}'); do
    valor=$(docker compose exec -T gateway wget -qO- "http://$ip:8080/metrics" | grep -F "$1" | awk '{print $2}' | head -1)
    total=$((total + ${valor:-0}))
  done
  echo "$total"
}
ficha() { curl -s "$BASE/catalogo/v1/tienda/productos/$ID"; }
# El primero de la respuesta: el del producto. La promoción aplicada también tiene nombre.
nombre() { grep -o '"nombre":"[^"]*"' | head -1 | cut -d'"' -f4; }
vaciar() { docker compose exec -T redis redis-cli flushall >/dev/null; }

anotar "--- Producto de prueba"
RESPUESTA=$(curl -s "$BASE/catalogo/v1/admin/productos" -H "$A" -H 'Content-Type: application/json' \
  -d "{\"nombre\":\"Remera cache $SUFIJO\",\"categoria\":\"remeras\",\"sistemaTalles\":\"LETRAS\",\"precioBase\":{\"monto\":10000},\"variantes\":[{\"talle\":\"M\",\"color\":\"Blanco\"}]}")
ID=$(echo "$RESPUESTA" | campo productoId)
[ -n "$ID" ] || { anotar "No se pudo crear el producto: $RESPUESTA"; exit 1; }
comprobar "Sin publicar responde 404" 404 "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/catalogo/v1/tienda/productos/$ID")"
curl -s -o /dev/null -X POST "$BASE/catalogo/v1/admin/productos/$ID/publicar" -H "$A"
comprobar "Publicarlo se ve enseguida" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/catalogo/v1/tienda/productos/$ID")"

anotar "--- Invalidación"
ficha >/dev/null; ficha >/dev/null
VERSION=$(curl -s "$BASE/catalogo/v1/admin/productos/$ID" -H "$A" | sed -n 's/.*"version":\([0-9]*\).*/\1/p')
curl -s -o /dev/null -X PUT "$BASE/catalogo/v1/admin/productos/$ID" -H "$A" -H 'Content-Type: application/json' \
  -d "{\"nombre\":\"Remera editada $SUFIJO\",\"categoria\":\"remeras\",\"precioBase\":{\"monto\":12000},\"version\":$VERSION}"
comprobar "La lectura siguiente a una edición trae el nombre nuevo" "Remera editada $SUFIJO" "$(ficha | nombre)"
comprobar "Y el precio nuevo" 12000 "$(ficha | campo precioBase)"
comprobar "El listado también" 1 "$(curl -s "$BASE/catalogo/v1/tienda/productos?categoria=remeras&tamano=100" | grep -c "Remera editada $SUFIJO")"

DESDE=$(date -u -d '-1 hour' +%Y-%m-%dT%H:%M:%SZ); HASTA=$(date -u -d '+1 hour' +%Y-%m-%dT%H:%M:%SZ)
curl -s -o /dev/null "$BASE/catalogo/v1/promociones" -H "$A" -H 'Content-Type: application/json' \
  -d "{\"nombre\":\"Promo cache $SUFIJO\",\"tipo\":\"PORCENTAJE\",\"porcentaje\":50,\"vigencia\":{\"desde\":\"$DESDE\",\"hasta\":\"$HASTA\"},\"alcance\":{\"tipo\":\"PRODUCTO\",\"ids\":[\"$ID\"]}}"
comprobar "Una promoción nueva se aplica en la lectura siguiente" 6000 "$(ficha | campo precio)"

anotar "--- Avalancha con la caché vacía"
sleep 1
vaciar
ANTES=$(metrica 'cache_lecturas_fuente_total{recurso="ficha"}')
for i in $(seq 1 100); do curl -s -o /dev/null "$BASE/catalogo/v1/tienda/productos/$ID" & done
wait
DESPUES=$(metrica 'cache_lecturas_fuente_total{recurso="ficha"}')
LECTURAS=$((DESPUES - ANTES))
anotar "100 pedidos iguales simultáneos: $LECTURAS lecturas a MongoDB"
comprobar "Hacen muy pocas lecturas a MongoDB (a lo sumo una por réplica en cada tanda)" si "$([ "$LECTURAS" -le 10 ] && echo si || echo no)"

anotar "--- Caída de Redis"
docker compose stop redis >/dev/null 2>&1
errores=0; inicio=$(date +%s%3N)
for i in $(seq 1 50); do
  [ "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/catalogo/v1/tienda/productos/$ID")" = 200 ] || errores=$((errores + 1))
done
anotar "50 lecturas sin Redis en $(($(date +%s%3N) - inicio)) ms"
comprobar "Sin Redis, ninguna lectura falla" 0 "$errores"
comprobar "El listado también responde" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/catalogo/v1/tienda/productos?categoria=remeras")"
VERSION=$(curl -s "$BASE/catalogo/v1/admin/productos/$ID" -H "$A" | sed -n 's/.*"version":\([0-9]*\).*/\1/p')
comprobar "Se puede editar sin Redis" 200 "$(curl -s -o /dev/null -w '%{http_code}' -X PUT "$BASE/catalogo/v1/admin/productos/$ID" -H "$A" -H 'Content-Type: application/json' \
  -d "{\"nombre\":\"Remera sin redis $SUFIJO\",\"categoria\":\"remeras\",\"precioBase\":{\"monto\":12000},\"version\":$VERSION}")"
comprobar "Y se lee el dato nuevo" "Remera sin redis $SUFIJO" "$(ficha | nombre)"
docker compose start redis >/dev/null 2>&1
until [ "$(docker compose exec -T redis redis-cli ping 2>/dev/null | tr -d '\r')" = PONG ]; do sleep 1; done
ficha >/dev/null
comprobar "Al volver Redis, se vuelve a llenar" si "$([ "$(docker compose exec -T redis redis-cli --scan --pattern 'catalogo:*' | grep -c producto)" -ge 1 ] && echo si || echo no)"

curl -s -o /dev/null -X DELETE "$BASE/catalogo/v1/admin/productos/$ID" -H "$A"
comprobar "Despublicar se ve enseguida" 404 "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/catalogo/v1/tienda/productos/$ID")"

if [ "$fallas" -eq 0 ]; then anotar "RESULTADO: todo OK"; else anotar "RESULTADO: $fallas fallas"; exit 1; fi
