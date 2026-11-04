# Mocks

Mock del contrato de la capacidad publicada por FAV ([ADR-008](../docs/adr/ADR-008-contrato-propio.md)) y dependencias simuladas.

| Carpeta | Contrato | Puerto |
|---|---|---|
| [`catalogo-pedidos-v1`](catalogo-pedidos-v1) | Catálogo y pedidos | 8090 |

[`catalogo-pedidos-v1`](catalogo-pedidos-v1) usa [Prism](https://github.com/stoplightio/prism) sobre el archivo OpenAPI de [`docs/contracts/`](../docs/contracts): no hay otra copia del contrato que mantener.

| Carpeta | Simula | Puerto |
|---|---|---|
| [`mercadopago-simulador`](mercadopago-simulador) | Mercado Pago ([ADR-014](../docs/adr/ADR-014-pagos-mails-y-orden-de-la-compra.md)) | 8096 |
| [`gimnasio-consignacion`](gimnasio-consignacion) | La API del gimnasio ([ADR-009](../docs/adr/ADR-009-consumo-del-gimnasio.md)) | 8095 |
