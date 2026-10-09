# Mock · cupos-v1 (opción A · Reserva de cupos)

Mock del contrato [`cupos-v1.openapi.yaml`](../../docs/contracts/cupos-v1.openapi.yaml). No guarda estado: responde siempre con los ejemplos del contrato, para que el grupo consumidor pueda armar su integración antes de que el servicio real exista.

## Levantarlo

```bash
docker compose up
```

Queda en `http://localhost:8090` (puerto **[propuesta]**). Sin Docker: `npx @stoplight/prism-cli mock -p 8090 ../../docs/contracts/cupos-v1.openapi.yaml`.

## Probarlo

```bash
curl http://localhost:8090/v1/cupos/tortas-2026-10-10 -H "X-API-Key: clave-de-prueba"
```

Más ejemplos de uso en [`docs/contracts/README.md`](../../docs/contracts/README.md).

## Qué hace y qué no

- Cada pedido recibe el **ejemplo principal** de la respuesta exitosa. Para ver una respuesta de error, el pedido manda `Prefer: code=409` (o el código que se quiera ver; tiene que estar definido en el contrato) y, si hay varios ejemplos, `Prefer: example=<nombre>`.
- **Valida** los pedidos contra el contrato: un cuerpo inválido recibe un error de validación.
- **No** descuenta cupos, no vence reservas y no controla idempotencia ni la clave. Eso lo hace el servicio real (Entrega 2).
- El mock no reemplaza al servicio real. Se retira cuando el servicio real está desplegado. Si se elige la opción B, este mock se borra (ADR-008).
