#!/usr/bin/env bash
# Arranca FAV en la VM con el archivo de la nube. Se corre desde la raíz del repo:  bash desplegar/arrancar.sh
# Antes de levantar, revisa que el .env no tenga los valores públicos de .env.example.
set -euo pipefail
cd "$(dirname "$0")/.."

[ -f .env ] || { echo "Falta .env: cp .env.example .env y completarlo (desplegar/README.md, paso 2)"; exit 1; }
chmod 600 .env

valor() { grep -E "^$1=" .env | tail -n1 | cut -d= -f2-; }

errores=0
for v in ADMIN_PASSWORD INVENTARIO_DB_ROOT_PASSWORD INVENTARIO_DB_PASSWORD VENTAS_DB_ROOT_PASSWORD VENTAS_DB_PASSWORD \
         USUARIOS_DB_ROOT_PASSWORD USUARIOS_DB_PASSWORD MONGO_INITDB_ROOT_PASSWORD RABBITMQ_DEFAULT_PASS \
         GRAFANA_ADMIN_PASSWORD MERCADOPAGO_WEBHOOK_SECRET CAPACIDAD_API_KEY CAPACIDAD_API_KEY_GIMNASIO; do
  case "$(valor "$v")" in
    "" ) echo "  $v: vacía" ; errores=1 ;;
    cambiar|cambiar-admin-local|secreto-del-simulador|clave-grupo-proveedores|clave-grupo-gimnasio)
         echo "  $v: tiene el valor público de .env.example"; errores=1 ;;
  esac
done
case "$(valor RABBITMQ_DEFAULT_PASS)" in
  *[!A-Za-z0-9_-]*) echo "  RABBITMQ_DEFAULT_PASS: usar solo letras y números (va dentro de una URL)"; errores=1 ;;
esac
if [ "$(valor CAPACIDAD_API_KEY)" = "$(valor CAPACIDAD_API_KEY_GIMNASIO)" ]; then
  echo "  CAPACIDAD_API_KEY y CAPACIDAD_API_KEY_GIMNASIO tienen que ser distintas"; errores=1
fi
[ "$errores" = 0 ] || { echo "Corregir .env y volver a correr."; exit 1; }

docker compose -f docker-compose.yml -f docker-compose.cloud.yml up -d --build

echo "Esperando que los servicios queden sanos (hasta 3 minutos)..."
for _ in $(seq 36); do
  if ! docker compose -f docker-compose.yml -f docker-compose.cloud.yml ps --format '{{.Status}}' | grep -qE 'starting|unhealthy|Restarting'; then
    break
  fi
  sleep 5
done
docker compose -f docker-compose.yml -f docker-compose.cloud.yml ps --format 'table {{.Service}}\t{{.Status}}'
