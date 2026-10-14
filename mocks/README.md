# Mocks

Mock del contrato de la capacidad publicada por FAV ([ADR-008](../docs/adr/ADR-008-contrato-propio.md)).

| Carpeta | Contrato | Puerto |
|---|---|---|
| [`catalogo-pedidos-v1`](catalogo-pedidos-v1) | Catálogo y pedidos | 8090 |

Usa [Prism](https://github.com/stoplightio/prism) sobre el archivo OpenAPI de [`docs/contracts/`](../docs/contracts): no hay otra copia del contrato que mantener.
