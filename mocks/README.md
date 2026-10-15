# Mocks

Mock del contrato de la capacidad publicada por FAV ([ADR-008](../docs/adr/ADR-008-contrato-propio.md)).

| Carpeta | Contrato | Puerto |
|---|---|---|
| [`catalogo-pedidos-v1`](catalogo-pedidos-v1) | Catálogo y pedidos (1.1.0), con avisos por webhook | 8090 |

Usa [Prism](https://github.com/stoplightio/prism) sobre el archivo OpenAPI de [`docs/contracts/`](../docs/contracts): no hay otra copia del contrato que mantener. Un servidor chico en Node, adelante de Prism, envía los avisos firmados (detalle en el README de la carpeta).
