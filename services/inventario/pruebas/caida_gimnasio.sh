#!/usr/bin/env bash
# Caída provocada del gimnasio (mock local), por el gateway y con el sistema levantado (docker compose up).
# Comprueba: la reposición queda pendiente, el panel recibe "no disponible" y Reservas sigue atendiendo; al volver el
# gimnasio se confirma una sola vez. Su salida sirve de evidencia para docs/POSTMORTEM.md. Requiere curl y Docker.
#
#   bash services/inventario/pruebas/caida_gimnasio.sh
set -u

BASE="${BASE:-http://localhost:8080}"
EMAIL="${ADMIN_EMAIL:-admin@fav.example}"
CONTRASENA="${ADMIN_PASSWORD:-cambiar-admin-local}"
VARIANTE="${VARIANTE:-var-remera-basica-blanca-m}"
ESPERA_MAXIMA="${ESPERA_MAXIMA:-120}"
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

fisico() { curl -s "$BASE/inventario/v1/stock?variantes=$VARIANTE" -H "$A" | campo fisico; }
estado_de() {
  curl -s "$BASE/inventario/v1/reposiciones" -H "$A" | tr '{' '\n' | grep "\"reposicionId\":\"$1\"" | campo estado
}

CLAVE="ingreso-$(date +%s)"
curl -s -o /dev/null "$BASE/inventario/v1/ingresos" -H "$A" -H "Idempotency-Key: $CLAVE" -H 'Content-Type: application/json' \
  -d "{\"varianteId\":\"$VARIANTE\",\"canal\":\"FISICO\",\"cantidad\":5}"
ANTES=$(fisico)
anotar "Stock físico de $VARIANTE antes de la caída: $ANTES"

anotar "--- Se apaga el gimnasio"
docker compose stop gimnasio-mock >/dev/null 2>&1

CODIGO=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/inventario/v1/consignacion" -H "$A")
comprobar "El panel informa que el stock del gimnasio no está disponible" 503 "$CODIGO"

CLAVE_REP="caida-$(date +%s)"
CUERPO="{\"varianteId\":\"$VARIANTE\",\"cantidad\":2}"
RESPUESTA=$(curl -s -w '\n%{http_code}' "$BASE/inventario/v1/reposiciones" -H "$A" -H "Idempotency-Key: $CLAVE_REP" -H 'Content-Type: application/json' -d "$CUERPO")
comprobar "La reposición se acepta con el gimnasio caído" 202 "$(echo "$RESPUESTA" | tail -1)"
ID=$(echo "$RESPUESTA" | campo reposicionId)
comprobar "El stock físico se descontó una vez" "$((ANTES - 2))" "$(fisico)"

sleep 8 # le da tiempo al proceso a intentar y fallar
comprobar "Sigue pendiente, sin devolver unidades" PENDIENTE_CONFIRMACION "$(estado_de "$ID")"
comprobar "El stock físico no cambió" "$((ANTES - 2))" "$(fisico)"
CODIGO=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/inventario/v1/disponibilidad?variantes=$VARIANTE")
comprobar "El resto de Inventario sigue atendiendo" 200 "$CODIGO"

RESPUESTA=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/inventario/v1/reposiciones" -H "$A" -H "Idempotency-Key: $CLAVE_REP" -H 'Content-Type: application/json' -d "$CUERPO")
comprobar "Repetir el pedido con la misma clave no crea otra" 202 "$RESPUESTA"
comprobar "El stock físico sigue descontado una sola vez" "$((ANTES - 2))" "$(fisico)"

anotar "--- Vuelve el gimnasio"
docker compose start gimnasio-mock >/dev/null 2>&1
inicio=$(date +%s)
actual=""
while [ $(($(date +%s) - inicio)) -lt "$ESPERA_MAXIMA" ]; do
  actual=$(estado_de "$ID")
  [ "$actual" = CONFIRMADA ] && break
  sleep 3
done
comprobar "La reposición se confirma sola" CONFIRMADA "$actual"
anotar "Recuperación: $(($(date +%s) - inicio)) s desde que volvió el gimnasio"
comprobar "El stock físico quedó descontado una sola vez" "$((ANTES - 2))" "$(fisico)"

if [ "$fallas" -eq 0 ]; then anotar "RESULTADO: todo OK"; else anotar "RESULTADO: $fallas fallas"; exit 1; fi
