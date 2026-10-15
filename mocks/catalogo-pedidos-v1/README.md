# Mock · catalogo-pedidos-v1 (1.1.0)

Mock del contrato [`catalogo-pedidos-v1.openapi.yaml`](../../docs/contracts/catalogo-pedidos-v1.openapi.yaml). Responde con los ejemplos del contrato y **envía el webhook `pedidoActualizado` firmado**, para integrar antes de que el servicio real esté listo.

## Levantarlo

```bash
docker compose up
```

Queda en `http://localhost:8090`.

Sin Docker (Node 22 o más nuevo):

```bash
npx @stoplight/prism-cli mock -p 4010 ../../docs/contracts/catalogo-pedidos-v1.openapi.yaml   # en una terminal
node servidor.mjs                                                                           # en otra
```

## Cómo está armado

| Pieza | Qué hace |
|---|---|
| [Prism](https://github.com/stoplightio/prism) | Responde las operaciones con los ejemplos del contrato y valida cada pedido contra el contrato. No hay otra copia del contrato que mantener. |
| `servidor.mjs` | Punto de entrada en el puerto 8090. Reenvía todo a Prism y, si el pedido trae `notificacionUrl`, envía el aviso **firmado**. Prism solo no alcanza: su callback sale sin los headers `X-FAV-Firma` ni `X-FAV-Evento-Id`. |
| `avisos.mjs`, `firma.mjs` | Arman, firman y envían el aviso igual que FAV. `firma.mjs` también tiene `verificar()`, la lógica que tiene que implementar el receptor. |
| `enviar-aviso.mjs` | Herramienta para mandar un aviso firmado a cualquier URL, sin hacer un pedido. |

Sin dependencias: solo Node y Prism.

## Probar el catálogo y los pedidos

```bash
curl http://localhost:8090/v1/productos -H "X-API-Key: clave-de-prueba"
curl http://localhost:8090/v1/pedidos/7d9f2c1e-4b3a-4e8f-9c2d-1a0b3c4d5e6f -H "X-API-Key: clave-de-prueba"
```

Más ejemplos en [`docs/contracts/README.md`](../../docs/contracts/README.md).

- Cada pedido válido recibe el ejemplo principal de la respuesta exitosa. La consulta devuelve el pedido de ejemplo en estado `PAGADO`.
- **Valida contra el contrato:** sin `X-API-Key` responde `401`; sin `Idempotency-Key`, con un cuerpo inválido o con una `notificacionUrl` que no empieza con `http://` o `https://` responde `400 PEDIDO_INVALIDO`. La clave puede ser cualquier texto. El cuerpo del error es el ejemplo del contrato, no el detalle real del campo.
- **Para ver un error**, mandar el header `Prefer`:

  | Header | Respuesta |
  |---|---|
  | `Prefer: code=409` | `STOCK_INSUFICIENTE` |
  | `Prefer: code=409, example=claveReutilizada` | `CLAVE_IDEMPOTENCIA_REUTILIZADA` |
  | `Prefer: code=400, example=varianteInexistente` | `VARIANTE_INEXISTENTE` |
  | `Prefer: code=404` (en la consulta) | `PEDIDO_INEXISTENTE` |
  | `Prefer: code=429` | `LIMITE_EXCEDIDO` |
  | `Prefer: code=503` | `NO_DISPONIBLE` |

- **No guarda estado:** no aparta stock, no controla la idempotencia y devuelve siempre el mismo pedido. Eso lo hace el servicio real.

## Probar el webhook

**1. Con un pedido.** Si el pedido trae `notificacionUrl`, a los 2 segundos el mock envía el aviso a esa URL, firmado con el secreto `secreto-de-prueba`:

```bash
curl -X POST http://localhost:8090/v1/pedidos \
  -H "X-API-Key: clave-de-prueba" -H "Content-Type: application/json" \
  -H "Idempotency-Key: 6f1c2b8e-3d4a-4e5f-9a7b-1c2d3e4f5a6b" \
  -d '{ "referenciaExterna": "OC-2026-0457",
        "notificacionUrl": "http://host.docker.internal:9000/webhooks/fav",
        "items": [ { "varianteId": "var-remera-basica-blanca-m", "cantidad": 3 } ] }'
```

- Con el mock en Docker y el receptor en la máquina, usar `host.docker.internal` en lugar de `localhost`. Sin Docker, `localhost` funciona.
- Headers solo del mock, para elegir qué aviso llega:

  | Header | Efecto |
  |---|---|
  | (ninguno) | Aviso `PAGADO` |
  | `X-Mock-Aviso: VENCIDO` o `A_VERIFICAR` | Aviso con ese estado |
  | `X-Mock-Aviso: NINGUNO` | No envía aviso |
  | `X-Mock-Aviso-Duplicado: true` | Envía el mismo aviso dos veces (mismo `eventoId`): el receptor tiene que procesarlo una sola vez |

- Si el receptor no responde `2xx`, el mock reintenta a los 2 y 5 segundos con el mismo `eventoId`. Si responde `401`, no reintenta. FAV real usa esperas más largas (ver el contrato).

**2. Sin hacer un pedido**, con la herramienta:

```bash
docker compose run --rm enviar-aviso --url http://host.docker.internal:9000/webhooks/fav --estado VENCIDO
# sin Docker: node enviar-aviso.mjs --url http://localhost:9000/webhooks/fav --estado VENCIDO
```

| Opción | Para probar que el receptor… |
|---|---|
| `--duplicado` | procesa una sola vez el mismo `eventoId` |
| `--firma-invalida` | rechaza una firma hecha con otro secreto (`401`) |
| `--viejo` | rechaza un aviso firmado hace 10 minutos (`401`) |
| `--secreto <texto>` | funciona con otro secreto |
