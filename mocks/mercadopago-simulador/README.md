# Simulador de Mercado Pago

Reemplaza a Mercado Pago en el entorno local ([ADR-014](../../docs/adr/ADR-014-pagos-mails-y-orden-de-la-compra.md)): el sistema se levanta con un comando, sin credenciales ni túnel. No cobra nada.

| Ruta | Qué hace |
|---|---|
| `POST /checkout/preferences` | Crea el pago y devuelve `init_point` |
| `GET /checkout/{id}` | Página con botones para aprobar o rechazar (http://localhost:8096) |
| `POST /checkout/{id}/aprobar`, `/rechazar` | Resuelve el pago y envía el aviso firmado a Ventas. `?duplicar=1` lo envía dos veces. Desde un navegador responde una página con el resultado; si no, JSON |
| `GET /v1/payments/{id}` | Estado del pago |

**Caída:** `docker compose stop mercadopago-simulador`. Los pagos viven en memoria: se pierden si el contenedor se reinicia.

**No está probado contra la API real** de Mercado Pago.
