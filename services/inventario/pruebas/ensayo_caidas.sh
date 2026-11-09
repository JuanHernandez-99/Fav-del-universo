#!/usr/bin/env bash
# Ensayo de caídas para docs/POSTMORTEM.md: gimnasio, una réplica de Inventario y su MySQL, con tráfico constante.
# Muestra la línea de tiempo, lo que recibió cada ruta en cada etapa, las alertas y los mails. Requiere el sistema
# levantado (docker compose up), curl y Docker. Las tres juntas tardan unos 15 minutos. No usar con otra prueba corriendo.
#
#   bash services/inventario/pruebas/ensayo_caidas.sh                 las tres
#   bash services/inventario/pruebas/ensayo_caidas.sh gimnasio mysql  solo esas
set -u

BASE="${BASE:-http://localhost:8080}"
PROMETHEUS="${PROMETHEUS:-http://localhost:9090}"
MAILPIT="${MAILPIT:-http://localhost:8025}"
EMAIL="${ADMIN_EMAIL:-admin@fav.example}"
CONTRASENA="${ADMIN_PASSWORD:-cambiar-admin-local}"
API_KEY="${API_KEY:-clave-grupo-proveedores}"
VARIANTE=var-remera-basica-blanca-m
VARIANTE_CARGA=var-carga-1
SUFIJO=$(date +%s)
INICIO=$(date -u +%Y-%m-%dT%H:%M:%S)
REGISTRO=$(mktemp)
fallas=0
SONDAS=()

anotar() { printf '%s  %s\n' "$(date -u +%H:%M:%SZ)" "$*"; }
comprobar() { # descripcion esperado obtenido
  if [ "$2" = "$3" ]; then anotar "OK    $1 ($3)"; else anotar "FALLA $1: se esperaba $2 y dio $3"; fallas=$((fallas + 1)); fi
}
campo() { sed -n "s/.*\"$1\":\"\{0,1\}\([^\",}]*\).*/\1/p" | head -1; }
iniciar_sesion() {
  TOKEN=$(curl -s "$BASE/usuarios/v1/sesiones" -H 'Content-Type: application/json' \
    -d "{\"email\":\"$EMAIL\",\"contrasena\":\"$CONTRASENA\"}" | campo token)
  [ -n "$TOKEN" ] || { anotar "No se pudo iniciar sesión como administrador"; exit 1; }
  A="Authorization: Bearer $TOKEN"
}
ingresar() { # variante canal cantidad
  curl -s -o /dev/null "$BASE/inventario/v1/ingresos" -H "$A" -H "Idempotency-Key: ensayo-$1-$2-$SUFIJO" -H 'Content-Type: application/json' \
    -d "{\"varianteId\":\"$1\",\"canal\":\"$2\",\"cantidad\":$3}"
}
reservar() { # clave: imprime el cuerpo y, en la última línea, el código
  curl -s -m 10 -w '\n%{http_code}' "$BASE/inventario/v1/reservas" -H "$A" -H "Idempotency-Key: $1" -H 'Content-Type: application/json' \
    -d "{\"referencia\":\"$1\",\"vigenciaMinutos\":5,\"items\":[{\"varianteId\":\"$VARIANTE_CARGA\",\"cantidad\":1}]}"
}
pedir() { # clave: un pedido completo por Ventas, como lo hace el grupo consumidor. Última línea: código y duración
  curl -s -m 10 -w '\n%{http_code} %{time_total}' "$BASE/v1/pedidos" -H "X-API-Key: $API_KEY" -H "Idempotency-Key: $1" -H 'Content-Type: application/json' \
    -d "{\"items\":[{\"varianteId\":\"$VARIANTE\",\"cantidad\":1}]}"
}
alerta_activa() { # nombre: firing, pending o nada
  curl -s "$PROMETHEUS/api/v1/alerts" | tr '{' '\n' | grep -A2 "\"alertname\":\"$1\"" | grep -o '"state":"[a-z]*"' | head -1 | cut -d'"' -f4
}
fisico() { curl -s "$BASE/inventario/v1/stock?variantes=$VARIANTE" -H "$A" | campo fisico; }
estado_de() { curl -s "$BASE/inventario/v1/reposiciones" -H "$A" | tr '{' '\n' | grep "\"reposicionId\":\"$1\"" | campo estado; }

# Cada sonda anota: momento, ruta, código y duración.
sonda_get() { # nombre ruta
  while :; do
    printf '%s %s %s\n' "$(date +%s)" "$1" "$(curl -s -m 10 -o /dev/null -w '%{http_code} %{time_total}' -H "$A" "$BASE$2")" >>"$REGISTRO"
    sleep 0.5
  done
}
sonda_reservas() {
  local n=0
  while :; do
    n=$((n + 1))
    printf '%s reservar %s\n' "$(date +%s)" "$(curl -s -m 10 -o /dev/null -w '%{http_code} %{time_total}' "$BASE/inventario/v1/reservas" -H "$A" \
      -H "Idempotency-Key: ensayo-$SUFIJO-$n" -H 'Content-Type: application/json' \
      -d "{\"referencia\":\"ensayo-$SUFIJO-$n\",\"vigenciaMinutos\":5,\"items\":[{\"varianteId\":\"$VARIANTE_CARGA\",\"cantidad\":1}]}")" >>"$REGISTRO"
    sleep 0.5
  done
}
vigia_alertas() { # anota cuándo aparece y desaparece cada alerta activa
  local antes="" ahora nombre
  while :; do
    ahora=$(curl -s "$PROMETHEUS/api/v1/alerts" | tr '{' '\n' | grep -A2 '"alertname"' | tr -d '\n' | sed 's/"alertname":"/\n/g' |
      grep '"state":"firing"' | cut -d'"' -f1 | sort -u | tr '\n' ' ')
    for nombre in $ahora; do case " $antes " in *" $nombre "*) ;; *) anotar "ALERTA activa: $nombre" ;; esac; done
    for nombre in $antes; do case " $ahora " in *" $nombre "*) ;; *) anotar "ALERTA resuelta: $nombre" ;; esac; done
    antes=$ahora
    sleep 3
  done
}

# Qué recibió cada ruta entre dos momentos: cuántos pedidos por código y la duración máxima.
resumen() { # titulo desde hasta
  echo "--- $1 ($(($3 - $2)) s)"
  awk -v d="$2" -v h="$3" '$1 >= d && $1 < h {
      c[$2 " " $3]++; if ($4 > m[$2]) m[$2] = $4; r[$2] = 1
    } END {
      for (x in r) { l = sprintf("%-14s", x); for (k in c) { split(k, p, " "); if (p[1] == x) l = l " " p[2] ": " c[k] }
        print l sprintf("   máx %.2f s", m[x]) }
    }' "$REGISTRO" | sort
}
fin() { [ "${#SONDAS[@]}" -gt 0 ] && kill "${SONDAS[@]}" 2>/dev/null; rm -f "$REGISTRO"; }
trap fin EXIT

caida_gimnasio() {
  local antes respuesta id t0 t1 t2 actual=""
  antes=$(fisico)
  docker compose stop gimnasio-mock >/dev/null 2>&1
  t0=$(date +%s); anotar "Gimnasio apagado"
  respuesta=$(curl -s -w '\n%{http_code}' "$BASE/inventario/v1/reposiciones" -H "$A" -H "Idempotency-Key: ensayo-repo-$SUFIJO" -H 'Content-Type: application/json' -d "{\"varianteId\":\"$VARIANTE\",\"cantidad\":2}")
  comprobar "La reposición se acepta" 202 "$(echo "$respuesta" | tail -1)"
  id=$(echo "$respuesta" | campo reposicionId)
  sleep 180
  comprobar "Sigue pendiente" PENDIENTE_CONFIRMACION "$(estado_de "$id")"
  comprobar "El físico se descontó una vez" "$((antes - 2))" "$(fisico)"
  comprobar "ErroresDelServidor no se dispara por la degradación prevista" "" "$(alerta_activa ErroresDelServidor)"
  t1=$(date +%s)
  resumen "Gimnasio caído" "$t0" "$t1"
  docker compose start gimnasio-mock >/dev/null 2>&1
  anotar "Gimnasio encendido"
  while [ $(($(date +%s) - t1)) -lt 360 ]; do actual=$(estado_de "$id"); [ "$actual" = CONFIRMADA ] && break; sleep 2; done
  comprobar "La reposición se confirma sola" CONFIRMADA "$actual"
  anotar "Reposición confirmada a los $(($(date +%s) - t1)) s de volver el gimnasio"
  comprobar "El físico siguió descontado una sola vez" "$((antes - 2))" "$(fisico)"
  t2=$(date +%s); sleep 90
  resumen "Después de volver" "$t2" "$(date +%s)"
}

caida_replica() {
  local replica t0 t1
  replica=$(docker compose ps -q inventario | head -1)
  docker stop "$replica" >/dev/null
  t0=$(date +%s); anotar "Réplica apagada"
  sleep 90; t1=$(date +%s)
  resumen "Una réplica caída" "$t0" "$t1"
  docker start "$replica" >/dev/null
  anotar "Réplica encendida"
  sleep 60
  resumen "Después de volver" "$t1" "$(date +%s)"
}

caida_mysql() {
  local clave pedido respuesta primera t0 t1 t2 codigo=""
  clave="ensayo-base-$SUFIJO"; pedido="ensayo-pedido-$SUFIJO"
  docker compose stop mysql-inventario >/dev/null 2>&1
  t0=$(date +%s); anotar "MySQL apagado"
  comprobar "Reservar responde que no está disponible" 503 "$(reservar "$clave" | tail -1)"
  respuesta=$(pedir "$pedido")
  anotar "Pedido por Ventas: $(echo "$respuesta" | tail -1) s"
  comprobar "El pedido por Ventas responde que no está disponible" NO_DISPONIBLE "$(echo "$respuesta" | campo codigo)"
  sleep 150; t1=$(date +%s)
  resumen "MySQL caído" "$t0" "$t1"
  docker compose start mysql-inventario >/dev/null 2>&1
  anotar "MySQL encendido"
  while [ $(($(date +%s) - t1)) -lt 180 ]; do
    codigo=$(curl -s -o /dev/null -w '%{http_code}' -m 5 "$BASE/inventario/v1/disponibilidad?variantes=$VARIANTE")
    [ "$codigo" = 200 ] && break
    sleep 1
  done
  comprobar "Inventario vuelve a responder" 200 "$codigo"
  anotar "Inventario responde a los $(($(date +%s) - t1)) s de encender MySQL"
  respuesta=$(reservar "$clave")
  comprobar "Repetir la reserva que falló la crea" 201 "$(echo "$respuesta" | tail -1)"
  primera=$(echo "$respuesta" | campo reservaId)
  comprobar "Repetirla otra vez devuelve la misma reserva" "$primera" "$(reservar "$clave" | campo reservaId)"
  respuesta=$(pedir "$pedido")
  comprobar "Repetir el pedido que falló lo crea" 201 "$(echo "$respuesta" | tail -1 | cut -d' ' -f1)"
  primera=$(echo "$respuesta" | campo pedidoId)
  comprobar "Repetirlo otra vez devuelve el mismo pedido" "$primera" "$(pedir "$pedido" | campo pedidoId)"
  t2=$(date +%s); sleep 120
  resumen "Después de volver" "$t2" "$(date +%s)"
}

iniciar_sesion
ingresar "$VARIANTE_CARGA" ONLINE 100000
ingresar "$VARIANTE" FISICO 5
ingresar "$VARIANTE" ONLINE 5

vigia_alertas & SONDAS+=($!)
sonda_get tienda "/catalogo/v1/tienda/productos?categoria=remeras" & SONDAS+=($!)
sonda_get disponibilidad "/inventario/v1/disponibilidad?variantes=$VARIANTE" & SONDAS+=($!)
sonda_get consignacion /inventario/v1/consignacion & SONDAS+=($!)
sonda_reservas & SONDAS+=($!)

anotar "=== Situación normal"
t0=$(date +%s); sleep 60
resumen "Normal" "$t0" "$(date +%s)"

for caida in ${*:-gimnasio replica mysql}; do
  anotar "=== Caída: $caida"
  iniciar_sesion
  "caida_$caida"
done

anotar "=== Mails de alerta recibidos"
curl -s "$MAILPIT/api/v1/messages?limit=50" | tr '{' '\n' |
  sed -n 's/.*"Subject":"\(\[FAV[^"]*\)","Created":"\([^"]*\)".*/\2 \1/p' | awk -v desde="$INICIO" '$1 >= desde' | sort

if [ "$fallas" -eq 0 ]; then anotar "RESULTADO: todo OK"; else anotar "RESULTADO: $fallas fallas"; exit 1; fi
