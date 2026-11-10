# Mock · gimnasio (capacidad consumida)

Simula en local la API del gimnasio que vende ropa de FAV en consignación, según su [contrato 1.0.0](../../docs/contracts/externos/gimnasio/gimnasio-consignaciones-1.0.0.openapi.yaml) ([ADR-018](../../docs/adr/ADR-018-consumo-del-gimnasio-contrato-real.md)). No reemplaza la prueba conjunta con el grupo del gimnasio.

Usa [WireMock](https://wiremock.org). Se levanta con el `docker-compose.yml` de la raíz y queda en `http://localhost:8095`. No guarda estado: el stock es fijo y no recuerda entregas ni claves.

| Pedido | Respuesta |
|---|---|
| `GET /api/v1/consignaciones/stock?proveedorId=fav` | `200` con `items[]` |
| `POST /api/v1/consignaciones/reposiciones` | `201` `{reposicionId, estado: RECIBIDA}` |
| `Idempotency-Key` que empieza con `probar-rechazo-` | `422 STOCK_NO_REPRESENTABLE`: las unidades vuelven al stock físico |
| `Idempotency-Key` que empieza con `probar-conflicto-` | `409 REFERENCIA_REUTILIZADA`: queda `REQUIERE_REVISION` |
| `X-API-Key: clave-invalida` (`GIMNASIO_API_KEY=clave-invalida`) | `401 NO_AUTENTICADO`: queda `BLOQUEADA_CONFIGURACION` |

## Provocar fallas

```bash
docker compose stop gimnasio-mock     # caída: timeout y circuit breaker en Inventario
docker compose start gimnasio-mock

# Lentitud: todas las respuestas tardan 4 s (más que el timeout de 3 s)
curl -X POST localhost:8095/__admin/settings -d '{"fixedDelay": 4000}'
curl -X POST localhost:8095/__admin/settings -d '{"fixedDelay": 0}'
```
