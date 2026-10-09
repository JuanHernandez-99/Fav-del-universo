# Mocks

Un mock por cada contrato de la capacidad compartida ([ADR-008](../docs/adr/ADR-008-contrato-propio.md)). Se publica una sola opción; cuando se elija, se borra el mock de la otra.

| Carpeta | Opción | Puerto |
|---|---|---|
| [`cupos-v1`](cupos-v1) | A · Reserva de cupos | 8090 **[propuesta]** |
| [`emails-v1`](emails-v1) | B · Envío de emails | 8091 **[propuesta]** |

Ambos usan [Prism](https://github.com/stoplightio/prism) sobre el archivo OpenAPI de [`docs/contracts/`](../docs/contracts): no hay otra copia del contrato que mantener.
