# Mock · catalogo-pedidos-v1

Mock del contrato [`catalogo-pedidos-v1.openapi.yaml`](../../docs/contracts/catalogo-pedidos-v1.openapi.yaml). Responde con los ejemplos del contrato, para integrar antes de que el servicio real esté listo.

## Levantarlo

```bash
docker compose up
```

Queda en `http://localhost:8090`. Sin Docker: `npx @stoplight/prism-cli mock -p 8090 ../../docs/contracts/catalogo-pedidos-v1.openapi.yaml`.

## Probarlo

```bash
curl http://localhost:8090/v1/productos -H "X-API-Key: clave-de-prueba"
```

Más ejemplos en [`docs/contracts/README.md`](../../docs/contracts/README.md).

## Qué hace y qué no

- Cada pedido válido recibe el ejemplo principal de la respuesta exitosa.
- **Valida contra el contrato:** sin `X-API-Key` responde `401`; sin `Idempotency-Key` o con un cuerpo inválido responde `400 PEDIDO_INVALIDO`. La clave puede ser cualquier texto.
- **Para ver un error**, mandar el header `Prefer`:

  | Header | Respuesta |
  |---|---|
  | `Prefer: code=409` | `STOCK_INSUFICIENTE` |
  | `Prefer: code=409, example=claveReutilizada` | `CLAVE_IDEMPOTENCIA_REUTILIZADA` |
  | `Prefer: code=400, example=varianteInexistente` | `VARIANTE_INEXISTENTE` |
  | `Prefer: code=429` | `LIMITE_EXCEDIDO` |
  | `Prefer: code=503` | `NO_DISPONIBLE` |

- **No guarda estado:** no aparta stock, no controla la idempotencia y devuelve siempre el mismo pedido. Eso lo hace el servicio real.
