#!/usr/bin/env bash
# Validación de la búsqueda (ADR-006), por el gateway y con el sistema levantado (docker compose up). Mide el retraso
# entre un cambio y su aparición, y provoca: eventos repetidos, caída de Elasticsearch, caída de RabbitMQ, un mensaje
# inválido y una reconstrucción. Requiere curl y Docker.
#
#   bash services/catalogo/pruebas/busqueda.sh
set -u

BASE="${BASE:-http://localhost:8080}"
RABBIT="${RABBIT:-http://localhost:15672}"
RABBIT_USUARIO="${RABBITMQ_DEFAULT_USER:-fav}:${RABBITMQ_DEFAULT_PASS:-cambiar}"
EMAIL="${ADMIN_EMAIL:-admin@fav.example}"
CONTRASENA="${ADMIN_PASSWORD:-cambiar-admin-local}"
RETRASO_MAXIMO_MS="${RETRASO_MAXIMO_MS:-10000}"
fallas=0

anotar() { printf '%s  %s\n' "$(date -u +%H:%M:%SZ)" "$*"; }
comprobar() { # descripcion esperado obtenido
  if [ "$2" = "$3" ]; then anotar "OK    $1 ($3)"; else anotar "FALLA $1: se esperaba $2 y dio $3"; fallas=$((fallas + 1)); fi
}
campo() { sed -n "s/.*\"$1\":\"\{0,1\}\([^\",}]*\).*/\1/p" | head -1; }
ms() { date +%s%3N; }

TOKEN=$(curl -s "$BASE/usuarios/v1/sesiones" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"contrasena\":\"$CONTRASENA\"}" | campo token)
[ -n "$TOKEN" ] || { anotar "No se pudo iniciar sesión como administrador"; exit 1; }
A="Authorization: Bearer $TOKEN"

# Una palabra única por corrida, para no encontrar productos de corridas anteriores.
SUFIJO="$(date +%s)"
PALABRA="galaxia${SUFIJO}"
total_de() { curl -s "$BASE/catalogo/v1/tienda/productos?q=$1" | sed -n 's/.*"total":\([0-9]*\).*/\1/p'; }
codigo_de() { curl -s -o /dev/null -w '%{http_code}' "$BASE/catalogo/v1/tienda/productos?q=$1"; }
esperar_total() { # palabra total limite_ms: imprime los ms que tardó, o -1
  local inicio limite
  inicio=$(ms); limite=$((inicio + $3))
  while [ "$(ms)" -lt "$limite" ]; do
    if [ "$(total_de "$1")" = "$2" ]; then echo $(($(ms) - inicio)); return; fi
    sleep 0.1
  done
  echo -1
}
version_de() { curl -s "$BASE/catalogo/v1/admin/productos/$1" -H "$A" | sed -n 's/.*"version":\([0-9]*\).*/\1/p'; }
editar() { # id nombre
  local version
  version=$(version_de "$1")
  curl -s -o /dev/null -w '%{http_code}' -X PUT "$BASE/catalogo/v1/admin/productos/$1" -H "$A" -H 'Content-Type: application/json' \
    -d "{\"nombre\":\"$2\",\"categoria\":\"remeras\",\"precioBase\":{\"monto\":15000},\"version\":$version}"
}
publicar_en_rabbit() { # cuerpo
  curl -s -o /dev/null -u "$RABBIT_USUARIO" -H 'Content-Type: application/json' -X POST \
    "$RABBIT/api/exchanges/%2F/catalogo.eventos/publish" \
    -d "{\"properties\":{\"content_type\":\"application/json\",\"delivery_mode\":2},\"routing_key\":\"ProductoCambiado\",\"payload_encoding\":\"string\",\"payload\":$1}"
}
en_dlq() { curl -s -u "$RABBIT_USUARIO" "$RABBIT/api/queues/%2F/catalogo.indexador.dlq" | sed -n 's/.*"messages":\([0-9]*\).*/\1/p' | head -1; }

anotar "--- Búsqueda básica"
RESPUESTA=$(curl -s "$BASE/catalogo/v1/admin/productos" -H "$A" -H 'Content-Type: application/json' \
  -d "{\"nombre\":\"Remera $PALABRA\",\"categoria\":\"remeras\",\"sistemaTalles\":\"LETRAS\",\"precioBase\":{\"monto\":15000},\"variantes\":[{\"talle\":\"M\",\"color\":\"Blanco\"}]}")
ID=$(echo "$RESPUESTA" | campo productoId)
[ -n "$ID" ] || { anotar "No se pudo crear el producto: $RESPUESTA"; exit 1; }
sleep 3
comprobar "Un producto sin publicar no aparece" 0 "$(total_de "$PALABRA")"

anotar "--- Retraso entre un cambio y la búsqueda (objetivo: $RETRASO_MAXIMO_MS ms)"
curl -s -o /dev/null -X POST "$BASE/catalogo/v1/admin/productos/$ID/publicar" -H "$A"
MEDIDA=$(esperar_total "$PALABRA" 1 30000)
anotar "Publicar: $MEDIDA ms"
maximo=$MEDIDA
for i in 1 2 3 4 5 6 7 8 9 10; do
  # Palabras sin parecido entre sí: con errores de tipeo tolerados, x1 y x2 coincidirían antes de indexar.
  NUEVA="q$(echo "$SUFIJO$i" | md5sum | cut -c1-14)"
  editar "$ID" "Remera $PALABRA $NUEVA" >/dev/null
  MEDIDA=$(esperar_total "$NUEVA" 1 30000)
  anotar "Edición $i: $MEDIDA ms"
  if [ "$MEDIDA" -lt 0 ]; then maximo=99999999; elif [ "$MEDIDA" -gt "$maximo" ]; then maximo=$MEDIDA; fi
done
anotar "Peor caso medido: $maximo ms"
comprobar "El retraso está dentro del objetivo" si "$([ "$maximo" -le "$RETRASO_MAXIMO_MS" ] && echo si || echo no)"
TIPEO="$(echo "$PALABRA" | sed s/galaxia/galxia/)"
comprobar "Tolera un error de tipeo" 1 "$(total_de "$TIPEO")"

anotar "--- Eventos repetidos y mensajes inválidos"
VERSION=$(version_de "$ID")
publicar_en_rabbit "\"{\\\"id\\\":\\\"repetido-$SUFIJO\\\",\\\"tipo\\\":\\\"ProductoCambiado\\\",\\\"version\\\":1,\\\"datos\\\":{\\\"productoId\\\":\\\"$ID\\\",\\\"version\\\":1}}\""
publicar_en_rabbit "\"{\\\"id\\\":\\\"repetido-$SUFIJO\\\",\\\"tipo\\\":\\\"ProductoCambiado\\\",\\\"version\\\":1,\\\"datos\\\":{\\\"productoId\\\":\\\"$ID\\\",\\\"version\\\":$VERSION}}\""
sleep 3
comprobar "Un evento viejo y uno repetido no cambian el resultado" 1 "$(total_de "$NUEVA")"
ANTES_DLQ=$(en_dlq)
publicar_en_rabbit '"esto no es un evento"'
# Las estadísticas de RabbitMQ se actualizan cada pocos segundos.
inicio=$(date +%s)
until [ "$(en_dlq)" = "$((ANTES_DLQ + 1))" ] || [ $(($(date +%s) - inicio)) -gt 20 ]; do sleep 1; done
comprobar "Un mensaje inválido va a la cola de fallidos" "$((ANTES_DLQ + 1))" "$(en_dlq)"

anotar "--- Caída de Elasticsearch"
docker compose stop elasticsearch >/dev/null 2>&1
comprobar "Buscar por texto responde 503" 503 "$(codigo_de "$PALABRA")"
comprobar "Navegar por categoría sigue funcionando" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/catalogo/v1/tienda/productos?categoria=remeras")"
comprobar "La ficha sigue funcionando" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/catalogo/v1/tienda/productos/$ID")"
DURANTE="caidaes${SUFIJO}"
comprobar "Se puede editar con el motor caído" 200 "$(editar "$ID" "Remera $PALABRA $DURANTE")"
sleep 8 # el evento se publica y el indexador lo reintenta
docker compose start elasticsearch >/dev/null 2>&1
inicio=$(date +%s)
until [ "$(codigo_de "$PALABRA")" = 200 ] || [ $(($(date +%s) - inicio)) -gt 120 ]; do sleep 2; done
anotar "Elasticsearch volvió en $(($(date +%s) - inicio)) s"
MEDIDA=$(esperar_total "$DURANTE" 1 30000)
anotar "El cambio hecho durante la caída apareció a los $MEDIDA ms"
comprobar "El cambio hecho durante la caída no se perdió" 1 "$(total_de "$DURANTE")"
comprobar "No fue a la cola de fallidos" "$((ANTES_DLQ + 1))" "$(en_dlq)"

anotar "--- Caída de RabbitMQ"
docker compose stop rabbitmq >/dev/null 2>&1
DURANTE="caidarabbit${SUFIJO}"
comprobar "Se puede editar sin RabbitMQ" 200 "$(editar "$ID" "Remera $PALABRA $DURANTE")"
sleep 4
comprobar "La búsqueda sigue, con el dato anterior" 0 "$(total_de "$DURANTE")"
docker compose start rabbitmq >/dev/null 2>&1
MEDIDA=$(esperar_total "$DURANTE" 1 90000)
anotar "Al volver RabbitMQ, el cambio apareció a los $MEDIDA ms"
comprobar "El cambio pendiente se avisó solo" 1 "$(total_de "$DURANTE")"

anotar "--- Reconstrucción sin corte"
ANTES=$(curl -s "localhost:9200/_alias/productos" | sed -n 's/^{"\([^"]*\)".*/\1/p')
curl -s -o /dev/null -w '' -X POST "$BASE/catalogo/v1/admin/busqueda/reconstruir" -H "$A"
errores=0; consultas=0; limite=$(($(date +%s) + 8))
while [ "$(date +%s)" -lt "$limite" ]; do
  [ "$(total_de "$DURANTE")" = 1 ] || errores=$((errores + 1))
  consultas=$((consultas + 1))
done
DESPUES=$(curl -s "localhost:9200/_alias/productos" | sed -n 's/^{"\([^"]*\)".*/\1/p')
anotar "Índice antes: $ANTES; después: $DESPUES; $consultas consultas durante el cambio"
comprobar "El alias cambió de índice" si "$([ "$ANTES" != "$DESPUES" ] && [ -n "$DESPUES" ] && echo si || echo no)"
comprobar "Ninguna búsqueda falló durante la reconstrucción" 0 "$errores"

anotar "--- Despublicar"
curl -s -o /dev/null -X DELETE "$BASE/catalogo/v1/admin/productos/$ID" -H "$A"
MEDIDA=$(esperar_total "$DURANTE" 0 30000)
anotar "Salió de la búsqueda a los $MEDIDA ms"
comprobar "Un producto despublicado sale de la búsqueda" 0 "$(total_de "$DURANTE")"

if [ "$fallas" -eq 0 ]; then anotar "RESULTADO: todo OK"; else anotar "RESULTADO: $fallas fallas"; exit 1; fi
