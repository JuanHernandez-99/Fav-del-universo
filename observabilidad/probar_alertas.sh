#!/usr/bin/env bash
# Provoca fallas reales y comprueba toda la cadena de cada alerta (ADR-011): se dispara en Prometheus, llega el mail a
# Mailpit y, al restaurar, llega el aviso de resuelta. Requiere el sistema levantado (docker compose up), curl y Docker.
# Tarda varios minutos: apaga y prende servicios. No usar con otra prueba corriendo.
#
#   bash observabilidad/probar_alertas.sh
set -u

BASE="${BASE:-http://localhost:8080}"
PROMETHEUS="${PROMETHEUS:-http://localhost:9090}"
MAILPIT="${MAILPIT:-http://localhost:8025}"
RABBIT="${RABBIT:-http://localhost:15672}"
RABBIT_USUARIO="${RABBITMQ_DEFAULT_USER:-fav}:${RABBITMQ_DEFAULT_PASS:-cambiar}"
EMAIL="${ADMIN_EMAIL:-admin@fav.example}"
CONTRASENA="${ADMIN_PASSWORD:-cambiar-admin-local}"
fallas=0

anotar() { printf '%s  %s\n' "$(date -u +%H:%M:%SZ)" "$*"; }
comprobar() { # descripcion esperado obtenido
  if [ "$2" = "$3" ]; then anotar "OK    $1"; else anotar "FALLA $1: se esperaba $2 y dio $3"; fallas=$((fallas + 1)); fi
}

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
  esperar "$alerta deja de estar activa" "estado_alerta $alerta" "" "${4:-180}"
  esperar "llega el aviso de que se resolvió" "hay_mail resolved $alerta ${antes_resuelta:-0}" si 180
}

TOKEN=$(curl -s "$BASE/usuarios/v1/sesiones" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"contrasena\":\"$CONTRASENA\"}" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
[ -n "$TOKEN" ] || { anotar "No se pudo iniciar sesión como administrador"; exit 1; }
A="Authorization: Bearer $TOKEN"

# Tráfico de fondo mientras dure una prueba.
trafico_inicio() { # ruta
  ( while :; do curl -s -o /dev/null -H "$A" "$BASE$1"; sleep 0.5; done ) &
  TRAFICO=$!
}
trafico_fin() { kill "$TRAFICO" 2>/dev/null; wait "$TRAFICO" 2>/dev/null; }

anotar "=== Mensajes en la cola de fallidos"
curl -s -o /dev/null -u "$RABBIT_USUARIO" -H 'Content-Type: application/json' -X POST "$RABBIT/api/exchanges/%2F/amq.default/publish" \
  -d '{"properties":{"delivery_mode":2},"routing_key":"catalogo.indexador","payload_encoding":"string","payload":"esto no es un evento"}'
ciclo MensajesEnColaDeFallidos 60 '
  for cola in catalogo.indexador.dlq ventas.reserva-vencida.dlq; do curl -s -o /dev/null -u "$RABBIT_USUARIO" -X DELETE "$RABBIT/api/queues/%2F/$cola/contents"; done'

anotar "=== Una réplica de Inventario caída"
REPLICA=$(docker compose ps -q inventario | head -1)
docker stop "$REPLICA" >/dev/null
ciclo ReplicaCaidaInventario 120 'docker start "$REPLICA" >/dev/null'

anotar "=== Más del 5 % de errores 5xx"
trafico_inicio /inventario/v1/disponibilidad?variantes=var-remera-basica-blanca-m
docker compose stop mysql-inventario >/dev/null 2>&1
ciclo ErroresDelServidor 240 'docker compose start mysql-inventario >/dev/null 2>&1'
trafico_fin

anotar "=== Gimnasio caído"
trafico_inicio /inventario/v1/consignacion
docker compose stop gimnasio-mock >/dev/null 2>&1
ciclo CircuitoGimnasioAbierto 120 'docker compose start gimnasio-mock >/dev/null 2>&1; trafico_fin; trafico_inicio /inventario/v1/consignacion'
trafico_fin

anotar "=== Redis caído"
trafico_inicio /catalogo/v1/tienda/productos?categoria=remeras
docker compose stop redis >/dev/null 2>&1
ciclo CacheNoDisponible 180 'docker compose start redis >/dev/null 2>&1'
trafico_fin

anotar "=== Credencial del gimnasio rechazada"
GIMNASIO_API_KEY=clave-invalida docker compose up -d --no-deps inventario >/dev/null 2>&1
sleep 12
VARIANTE=var-remera-basica-blanca-m
SUFIJO=$(date +%s)
curl -s -o /dev/null "$BASE/inventario/v1/ingresos" -H "$A" -H "Idempotency-Key: alertas-ingreso-$SUFIJO" -H 'Content-Type: application/json' -d "{\"varianteId\":\"$VARIANTE\",\"canal\":\"FISICO\",\"cantidad\":1}"
curl -s -o /dev/null "$BASE/inventario/v1/reposiciones" -H "$A" -H "Idempotency-Key: alertas-$SUFIJO" -H 'Content-Type: application/json' -d "{\"varianteId\":\"$VARIANTE\",\"cantidad\":1}"
# Se destraba en el reintento siguiente: hasta 5 minutos después de corregir la clave (ADR-018).
ciclo ReposicionBloqueada 90 'docker compose up -d --no-deps inventario >/dev/null 2>&1' 420

if [ "$fallas" -eq 0 ]; then anotar "RESULTADO: todo OK"; else anotar "RESULTADO: $fallas fallas"; exit 1; fi
