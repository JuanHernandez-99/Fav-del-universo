# Mock · emails-v1 (opción B · Envío de emails)

Mock del contrato [`emails-v1.openapi.yaml`](../../docs/contracts/emails-v1.openapi.yaml). No guarda estado: responde siempre con los ejemplos del contrato, para que el grupo consumidor pueda armar su integración antes de que el servicio real exista.

## Levantarlo

```bash
docker compose up
```

Queda en `http://localhost:8091` (puerto **[propuesta]**). Sin Docker: `npx @stoplight/prism-cli mock -p 8091 ../../docs/contracts/emails-v1.openapi.yaml`.

## Probarlo

```bash
curl http://localhost:8091/v1/emails/9b2e7c41-0d3a-4f6e-8a51-2c7d9e0f1a23 -H "X-API-Key: clave-de-prueba"
```

Más ejemplos de uso en [`docs/contracts/README.md`](../../docs/contracts/README.md).

## Qué hace y qué no

- Cada pedido recibe el **ejemplo principal** de la respuesta exitosa. Para ver una respuesta de error, el pedido manda `Prefer: code=409` (o el código que se quiera ver; tiene que estar definido en el contrato) y, si hay varios ejemplos, `Prefer: example=<nombre>`.
- **Valida** los pedidos contra el contrato: un cuerpo inválido recibe un error de validación.
- **No** envía emails, no programa envíos y no controla idempotencia ni la clave. Eso lo hace el servicio real (Entrega 2).
- El mock no reemplaza al servicio real. Se retira cuando el servicio real está desplegado. Si se elige la opción A, este mock se borra (ADR-008).
