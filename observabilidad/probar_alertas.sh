#!/usr/bin/env bash
# Provoca fallas reales y comprueba toda la cadena de cada alerta (ADR-011): se dispara en Prometheus, llega el mail a
# Mailpit y, al restaurar, llega el aviso de resuelta. Requiere el sistema levantado (docker compose up), curl y Docker.
# Todas juntas tardan unos 40 minutos: apaga y prende servicios. No usar con otra prueba corriendo. Deja en la base
# local las reposiciones y el producto de prueba.
#
#   bash observabilidad/probar_alertas.sh                         todas
#   bash observabilidad/probar_alertas.sh LatenciaAlta VentasCaido solo esas
set -u

BASE="${BASE:-http://localhost:8080}"
PROMETHEUS="${PROMETHEUS:-http://localhost:9090}"
MAILPIT="${MAILPIT:-http://localhost:8025}"
RABBIT="${RABBIT:-http://localhost:15672}"
GIMNASIO="${GIMNASIO:-http://localhost:8095}"
RABBIT_USUARIO="${RABBITMQ_DEFAULT_USER:-fav}:${RABBITMQ_DEFAULT_PASS:-cambiar}"
EMAIL="${ADMIN_EMAIL:-admin@fav.example}"
CONTRASENA="${ADMIN_PASSWORD:-cambiar-admin-local}"
VARIANTE=var-remera-basica-blanca-m
PEDIDOS=()
fallas=0

anotar() { printf '%s  %s\n' "$(date -u +%H:%M:%SZ)" "$*"; }

estado_alerta() { # nombre: firing, pending o nada
  curl -s "$PROMETHEUS/api/v1/alerts" | tr '{' '\n' | grep -A2 "\"alertname\":\"$1\"" | grep -o '"state":"[a-z]*"' | head -1 | cut -d'"' -f4
}
mails() { curl -s "$MAILPIT/api/v1/search?query=subject:%22FAV%20$1%22%20subject:$2" | sed -n 's/.*"messages_count":\([0-9]*\).*/\1/p'; }
esperar() { # descripcion comando esperado segundos: repite el comando hasta que imprima lo esperado
  local inicio obtenido
  inicio=$(date +%s)
  while [ $(($(date +%s) - inicio)) -lt "$4" ]; do
    obtenido=$(eval "$2")
    if [ "$obtenido" = "$3" ]; then anotar "OK    $1 (a los $(($(date +%s) - inicio)) s)"; return 0; fi
    sleep 3
  done
  anotar "FALLA $1: tras $4 s dio '$obtenido'"; fallas=$((fallas + 1)); return 1
}
hay_mail() { local n; n=$(mails "$1" "$2"); [ "${n:-0}" -gt "$3" ] && echo si || echo no; }

# Comprueba el ciclo completo: dispara, avisa por mail y avisa al resolverse.
ciclo() { # alerta segundos_disparo comando_restaurar [segundos_para_resolverse]
  local alerta=$1 antes_firing antes_resuelta
  antes_firing=$(mails firing "$alerta"); antes_resuelta=$(mails resolved "$alerta")
  esperar "$alerta se dispara" "estado_alerta $alerta" firing "$2"
  esperar "llega el mail de $alerta" "hay_mail firing $alerta ${antes_firing:-0}" si 120
  anotar "--- Se restaura"
  eval "$3"
  esperar "$alerta deja de estar activa" "estado_alerta $alerta" "" "${4:-240}"
  esperar "llega el aviso de que se resolvió" "hay_mail resolved $alerta ${antes_resuelta:-0}" si 180
}

# El token puede vencer durante la corrida: se pide uno por prueba.
iniciar_sesion() {
  TOKEN=$(curl -s "$BASE/usuarios/v1/sesiones" -H 'Content-Type: application/json' \
    -d "{\"email\":\"$EMAIL\",\"contrasena\":\"$CONTRASENA\"}" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
  [ -n "$TOKEN" ] || { anotar "No se pudo iniciar sesión como administrador"; exit 1; }
  A="Authorization: Bearer $TOKEN"
}

# Pedidos de fondo mientras dure una prueba. Si no hay tráfico, no hay tasa de errores ni latencia que medir.
trafico_inicio() { # ruta [cuantos en paralelo]
  local i
  for i in $(seq 1 "${2:-1}"); do
    ( while :; do curl -s -o /dev/null -H "$A" "$BASE$1"; sleep 0.5; done ) &
    PEDIDOS+=($!)
  done
}
trafico_fin() { [ "${#PEDIDOS[@]}" -gt 0 ] && kill "${PEDIDOS[@]}" 2>/dev/null; wait 2>/dev/null; PEDIDOS=(); }

sql_inventario() { docker compose exec -T mysql-inventario mysql -uroot -p"${INVENTARIO_DB_ROOT_PASSWORD:-cambiar}" inventario -e "$1" 2>/dev/null; }

# Una reposición necesita stock físico: primero ingresa una unidad.
reponer() { # clave
  curl -s -o /dev/null "$BASE/inventario/v1/ingresos" -H "$A" -H "Idempotency-Key: ingreso-$1" -H 'Content-Type: application/json' \
    -d "{\"varianteId\":\"$VARIANTE\",\"canal\":\"FISICO\",\"cantidad\":1}"
  curl -s -o /dev/null "$BASE/inventario/v1/reposiciones" -H "$A" -H "Idempotency-Key: $1" -H 'Content-Type: application/json' \
    -d "{\"varianteId\":\"$VARIANTE\",\"cantidad\":1}"
}

probar_MensajesEnColaDeFallidos() {
  curl -s -o /dev/null -u "$RABBIT_USUARIO" -H 'Content-Type: application/json' -X POST "$RABBIT/api/exchanges/%2F/amq.default/publish" \
    -d '{"properties":{"delivery_mode":2},"routing_key":"catalogo.indexador","payload_encoding":"string","payload":"esto no es un evento"}'
  ciclo MensajesEnColaDeFallidos 60 '
    for cola in catalogo.indexador.dlq ventas.reserva-vencida.dlq; do curl -s -o /dev/null -u "$RABBIT_USUARIO" -X DELETE "$RABBIT/api/queues/%2F/$cola/contents"; done'
}

probar_ReplicaCaidaInventario() {
  REPLICA=$(docker compose ps -q inventario | head -1)
  docker stop "$REPLICA" >/dev/null
  ciclo ReplicaCaidaInventario 120 'docker start "$REPLICA" >/dev/null'
}

probar_ReplicaCaidaCatalogo() {
  REPLICA=$(docker compose ps -q catalogo | head -1)
  docker stop "$REPLICA" >/dev/null
  ciclo ReplicaCaidaCatalogo 120 'docker start "$REPLICA" >/dev/null'
}

probar_VentasCaido() {
  docker compose stop ventas >/dev/null 2>&1
  ciclo VentasCaido 120 'docker compose start ventas >/dev/null 2>&1'
}

probar_UsuariosCaido() {
  docker compose stop usuarios >/dev/null 2>&1
  ciclo UsuariosCaido 120 'docker compose start usuarios >/dev/null 2>&1'
}

probar_RabbitMQCaido() {
  docker compose stop rabbitmq >/dev/null 2>&1
  ciclo RabbitMQCaido 120 'docker compose start rabbitmq >/dev/null 2>&1'
}

probar_ErroresDelServidor() {
  trafico_inicio "/inventario/v1/disponibilidad?variantes=$VARIANTE"
  docker compose stop mysql-inventario >/dev/null 2>&1
  ciclo ErroresDelServidor 240 'docker compose start mysql-inventario >/dev/null 2>&1' 300
  trafico_fin
}

# El gimnasio responde lento, sin errores: cada llamada tarda 1 s, menos que el tiempo de espera de 3 s de Inventario.
probar_LatenciaAlta() {
  curl -s -o /dev/null -X POST "$GIMNASIO/__admin/settings" -H 'Content-Type: application/json' -d '{"fixedDelay":1000}'
  trafico_inicio /inventario/v1/consignacion 4
  ciclo LatenciaAlta 300 'curl -s -o /dev/null -X POST "$GIMNASIO/__admin/settings" -H "Content-Type: application/json" -d "{\"fixedDelay\":0}"; trafico_fin' 300
}

probar_CircuitoGimnasioAbierto() {
  trafico_inicio /inventario/v1/consignacion
  docker compose stop gimnasio-mock >/dev/null 2>&1
  ciclo CircuitoGimnasioAbierto 120 'docker compose start gimnasio-mock >/dev/null 2>&1; trafico_fin; trafico_inicio /inventario/v1/consignacion'
  trafico_fin
}

probar_CacheNoDisponible() {
  trafico_inicio "/catalogo/v1/tienda/productos?categoria=remeras"
  docker compose stop memcached >/dev/null 2>&1
  ciclo CacheNoDisponible 180 'docker compose start memcached >/dev/null 2>&1'
  trafico_fin
}

# Una sola edición con Memcached caído: es el caso real, y una sola falla ya tiene que avisar.
probar_InvalidacionDeCacheFallida() {
  local id
  id=$(curl -s "$BASE/catalogo/v1/admin/productos" -H "$A" -H 'Content-Type: application/json' \
    -d "{\"nombre\":\"Remera alerta $(date +%s)\",\"categoria\":\"remeras\",\"sistemaTalles\":\"LETRAS\",\"precioBase\":{\"monto\":10000},\"variantes\":[{\"talle\":\"M\",\"color\":\"Blanco\"}]}" |
    sed -n 's/.*"productoId":"\([^"]*\)".*/\1/p')
  [ -n "$id" ] || { anotar "FALLA no se pudo crear el producto de prueba"; fallas=$((fallas + 1)); return; }
  docker compose stop memcached >/dev/null 2>&1
  curl -s -o /dev/null -X POST "$BASE/catalogo/v1/admin/productos/$id/publicar" -H "$A"
  # La regla mira 5 minutos hacia atrás: se resuelve sola cuando esa falla sale de la ventana.
  ciclo InvalidacionDeCacheFallida 90 'docker compose start memcached >/dev/null 2>&1; curl -s -o /dev/null -X DELETE "$BASE/catalogo/v1/admin/productos/'"$id"'" -H "$A"' 480
}

probar_ReposicionBloqueada() {
  GIMNASIO_API_KEY=clave-invalida docker compose up -d --no-deps inventario >/dev/null 2>&1
  sleep 12
  reponer "alertas-$(date +%s)"
  # Se destraba en el reintento siguiente: hasta 5 minutos después de corregir la clave (ADR-018).
  ciclo ReposicionBloqueada 90 'docker compose up -d --no-deps inventario >/dev/null 2>&1' 480
}

# El mock responde 409 a estas claves. El sistema no tiene cómo resolver una reposición en conflicto (ADR-018):
# para que la alerta se apague hay que cambiarla en la base.
probar_ReposicionARevisar() {
  local clave
  clave="probar-conflicto-$(date +%s)"
  reponer "$clave"
  ciclo ReposicionARevisar 90 "sql_inventario \"UPDATE reposiciones SET estado='RECHAZADA' WHERE clave_idempotencia='$clave'\""
}

# Con el gimnasio caído se envejece la reposición en la base, para no esperar los 10 minutos reales.
probar_ReposicionPendienteAntigua() {
  local clave
  clave="alertas-antigua-$(date +%s)"
  docker compose stop gimnasio-mock >/dev/null 2>&1
  reponer "$clave"
  sql_inventario "UPDATE reposiciones SET creada_en = UTC_TIMESTAMP() - INTERVAL 11 MINUTE WHERE clave_idempotencia='$clave'"
  ciclo ReposicionPendienteAntigua 90 'docker compose start gimnasio-mock >/dev/null 2>&1' 300
}

TODAS="MensajesEnColaDeFallidos ReplicaCaidaInventario ReplicaCaidaCatalogo VentasCaido UsuariosCaido RabbitMQCaido
  ErroresDelServidor LatenciaAlta CircuitoGimnasioAbierto CacheNoDisponible InvalidacionDeCacheFallida
  ReposicionBloqueada ReposicionARevisar ReposicionPendienteAntigua"

for alerta in ${*:-$TODAS}; do
  anotar "=== $alerta"
  iniciar_sesion
  "probar_$alerta"
done

if [ "$fallas" -eq 0 ]; then anotar "RESULTADO: todo OK"; else anotar "RESULTADO: $fallas fallas"; exit 1; fi
