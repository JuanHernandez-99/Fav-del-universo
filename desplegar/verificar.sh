#!/usr/bin/env bash
# Verifica FAV desplegado DESDE AFUERA de la VM (tu computadora o el celular con datos, no la VM).
#   DOMINIO=fav-ucc.chilecentral.cloudapp.azure.com CAPACIDAD_API_KEY=... CAPACIDAD_API_KEY_GIMNASIO=... \
#     bash desplegar/verificar.sh
# Con el certificado de prueba de Let's Encrypt (staging), agregar INSEGURO=1: curl no lo reconoce.
#
# Ojo: hace un pedido real de 1 unidad, que aparta stock por 48 horas (lo libera el vencimiento).
set -uo pipefail

: "${DOMINIO:?falta DOMINIO}" "${CAPACIDAD_API_KEY:?falta CAPACIDAD_API_KEY}" "${CAPACIDAD_API_KEY_GIMNASIO:?falta CAPACIDAD_API_KEY_GIMNASIO}"
URL="https://$DOMINIO"
CURL=(curl -s --max-time 10)
[ "${INSEGURO:-0}" = 1 ] && CURL+=(-k)

fallas=0
ok()    { echo "  OK    $1"; }
falla() { echo "  FALLA $1"; fallas=$((fallas + 1)); }
codigo() { "${CURL[@]}" -o /dev/null -w '%{http_code}' "$@"; }
espera() { local desc=$1 esperado=$2; shift 2; local c; c=$(codigo "$@"); [ "$c" = "$esperado" ] && ok "$desc ($c)" || falla "$desc: $c, se esperaba $esperado"; }

echo "HTTPS y tienda"
espera "HTTP redirige a HTTPS" 308 "http://$DOMINIO/"
espera "La tienda carga" 200 "$URL/"

echo "Capacidad publicada (contrato catalogo-pedidos-v1)"
espera "GET /v1/productos sin clave" 401 "$URL/v1/productos"
espera "GET /v1/productos con clave falsa" 401 "$URL/v1/productos" -H "X-API-Key: clave-falsa"
espera "GET /v1/productos con la clave de proveedores" 200 "$URL/v1/productos" -H "X-API-Key: $CAPACIDAD_API_KEY"
espera "GET /v1/productos con la clave del gimnasio" 200 "$URL/v1/productos" -H "X-API-Key: $CAPACIDAD_API_KEY_GIMNASIO"

variante=$("${CURL[@]}" "$URL/v1/productos" -H "X-API-Key: $CAPACIDAD_API_KEY" | grep -o '"varianteId":"[^"]*"' | head -n1 | cut -d'"' -f4)
if [ -z "$variante" ]; then
  falla "No se pudo leer una variante del catálogo"
else
  espera "GET /v1/disponibilidad con la clave del gimnasio" 200 "$URL/v1/disponibilidad?variantes=$variante" -H "X-API-Key: $CAPACIDAD_API_KEY_GIMNASIO"
  cuerpo="{\"referenciaExterna\":\"verificar-despliegue\",\"items\":[{\"varianteId\":\"$variante\",\"cantidad\":1}]}"
  clave="verificar-$(date +%s)-$RANDOM"
  pedir() { "${CURL[@]}" -X POST "$URL/v1/pedidos" -H "X-API-Key: $1" -H "Content-Type: application/json" -H "Idempotency-Key: $clave" -d "$cuerpo" -w '\n%{http_code}'; }
  espera "POST /v1/pedidos con la clave del gimnasio" 401 -X POST "$URL/v1/pedidos" -H "X-API-Key: $CAPACIDAD_API_KEY_GIMNASIO" \
    -H "Content-Type: application/json" -H "Idempotency-Key: $clave-gim" -d "$cuerpo"
  primera=$(pedir "$CAPACIDAD_API_KEY"); segunda=$(pedir "$CAPACIDAD_API_KEY")
  c1=$(tail -n1 <<<"$primera"); c2=$(tail -n1 <<<"$segunda")
  id1=$(grep -o '"pedidoId":"[^"]*"' <<<"$primera"); id2=$(grep -o '"pedidoId":"[^"]*"' <<<"$segunda")
  if [ "$c1" = 201 ] && [ "$c2" = 201 ] && [ -n "$id1" ] && [ "$id1" = "$id2" ]; then
    ok "POST /v1/pedidos 201, y el reintento con la misma Idempotency-Key devuelve el mismo pedido ($id1)"
  elif [ "$c1" = 409 ] && grep -q STOCK_INSUFICIENTE <<<"$primera"; then
    ok "POST /v1/pedidos responde 409 STOCK_INSUFICIENTE: funciona, pero la variante $variante no tiene stock online"
  else
    falla "POST /v1/pedidos: $c1 y $c2 ($(head -n1 <<<"$primera" | cut -c1-200))"
  fi
fi

echo "Rutas que no tienen que estar abiertas"
c=$(codigo -X POST "$URL/checkout/preferences" -H "Content-Type: application/json" -d '{"external_reference":"x"}')
[ "$c" != 201 ] && ok "Crear pagos del simulador desde afuera: bloqueado ($c)" || falla "Se pueden crear pagos del simulador desde afuera"
espera "Consultar pagos del simulador desde afuera" 404 "$URL/v1/payments/1"

echo "Puertos cerrados desde afuera"
for p in 3307 3308 3309 5672 15672 8025 8095 8096 8080 9090 9093 9200 3000 16686; do
  if timeout 3 bash -c "</dev/tcp/$DOMINIO/$p" 2>/dev/null; then falla "Puerto $p abierto"; else ok "Puerto $p cerrado"; fi
done

echo
[ "$fallas" = 0 ] && echo "RESULTADO: todo OK" || { echo "RESULTADO: $fallas fallas"; exit 1; }
