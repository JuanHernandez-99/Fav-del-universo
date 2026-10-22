# Mock · gimnasio (capacidad consumida)

Simula la API del gimnasio que vende ropa de FAV en consignación. **El grupo del gimnasio todavía no publicó su contrato:** estas rutas son un **contrato supuesto** por FAV (ADR-009). Cuando llegue el real, se ajustan los mappings y el adaptador `services/inventario/internal/adaptadores/gimnasio`.

Usa [WireMock](https://wiremock.org). Se levanta con el `docker-compose.yml` de la raíz y queda en `http://localhost:8095`.

| Ruta supuesta | Respuesta |
|---|---|
| `GET /api/v1/consignaciones/stock?proveedorId=fav` | `200` con `items[]`: `productoCodigo`, `unidadesDisponibles`, `unidadesVendidas` |
| `POST /api/v1/consignaciones/reposiciones` (con `X-API-Key` e `Idempotency-Key`) | `201` `{reposicionId, estado: RECIBIDA}` |
| Lo mismo con `unidades` > 20 | `422 CAPACIDAD_EXCEDIDA` (rechazo: las unidades vuelven al stock físico) |

## Provocar fallas

```bash
docker compose stop gimnasio-mock     # caída: timeout y circuit breaker en Inventario
docker compose start gimnasio-mock

# Lentitud: todas las respuestas tardan 3 s (más que el timeout de 2 s)
curl -X POST localhost:8095/__admin/settings -d '{"fixedDelay": 3000}'
curl -X POST localhost:8095/__admin/settings -d '{"fixedDelay": 0}'
```
