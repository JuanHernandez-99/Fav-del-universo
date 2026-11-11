# FAV Tienda Online

Arquitectura de Software 2026
Autores:
Jonathan Brua - 2212370
Juan Hernandez - 2325917

Sistema de venta online y control de stock para **FAV**, una tienda de indumentaria femenina que hoy vende solo en el local y lleva el stock en un Excel. Es también el Trabajo Práctico Integrador de **Arquitectura de Software 2026**.

> **Estado: rumbo a la presentación grupal. Desplegado en Azure:** https://fav-ucc.chilecentral.cloudapp.azure.com. Los cuatro servicios funcionan detrás de un gateway nginx, con logs, métricas y alertas. En esta rama los logs se centralizan en MySQL y una compra se sigue por su track id ([ADR-020](docs/adr/ADR-020-logs-y-track-id-modelo-catedra.md)); la VM de Azure todavía corre `main`, con Jaeger (ADR-011). **Inventario** y **Catálogo**, con 2 réplicas cada uno. **Catálogo** tiene búsqueda con Elasticsearch y caché con Memcached. **Ventas** usa Mercado Pago simulado y manda mails a Mailpit ([ADR-014](docs/adr/ADR-014-pagos-mails-y-orden-de-la-compra.md)). **Usuarios** emite tokens y el gateway los valida. El **frontend** muestra la tienda, el carrito, el inicio de sesión, la compra y "Mis compras", y en el panel las ventas, la venta del local, los productos, las promociones y el stock ([ADR-017](docs/adr/ADR-017-frontend.md)). Ver las limitaciones en [ARCHITECTURE §16](docs/ARCHITECTURE.md).
>
> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar · _previsto_: parte del diseño, todavía no implementado.

## Qué resuelve

- Catálogo de productos con variantes (talle y color), fotos, categorías, precios y promociones.
- Stock separado en **online** y **físico**, con ingresos, ajustes y transferencias.
- Compra online (con o sin cuenta) con Mercado Pago o transferencia, con reserva temporal del stock.
- Registro de las ventas del local.
- Panel de administración y mails al comprador y al administrador.

El alcance completo, con reglas de negocio, historias de usuario y casos de uso, está en [`SPEC.md`](SPEC.md).

## Arquitectura en una mirada

Cuatro microservicios en **Go**, cada uno con su propia base de datos, detrás de un API Gateway, con un frontend web. Se comunican por HTTP y por eventos (RabbitMQ, con _transactional outbox_).

| Componente  | Responsabilidad                                             | Base de datos | Carpeta                                      |
| ----------- | ----------------------------------------------------------- | ------------- | -------------------------------------------- |
| Catálogo    | Productos, variantes, fotos, precios y promociones          | MongoDB       | [`services/catalogo`](services/catalogo)     |
| Inventario  | Stock por variante y canal, reservas, transferencias        | MySQL         | [`services/inventario`](services/inventario) |
| Ventas      | Ventas, pagos y mails                                       | MySQL         | [`services/ventas`](services/ventas)         |
| Usuarios    | Registro, inicio de sesión, token y roles                   | MySQL         | [`services/usuarios`](services/usuarios)     |
| API Gateway | Punto de entrada: autenticación, ruteo y control de tráfico | —             | [`gateway`](gateway)                         |
| Frontend    | Tienda y panel de administración                            | —             | [`frontend`](frontend)                       |

Detalle, diagramas, comunicaciones y comportamiento ante fallas: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).

## Integración con otros grupos

**Capacidad que publicamos: catálogo y pedidos.** Otro grupo de la materia puede ver el catálogo de FAV y hacerle pedidos, como un comprador mayorista. El pedido aparta el stock y registra la venta, pendiente de pago por transferencia.

| Operación | Ruta | Servicio |
|---|---|---|
| Ver el catálogo | `GET /v1/productos` | Catálogo |
| Hacer un pedido | `POST /v1/pedidos` | Ventas (aparta el stock en Inventario) |

Contrato: [`catalogo-pedidos-v1.openapi.yaml`](docs/contracts/catalogo-pedidos-v1.openapi.yaml). Cómo usarla, errores y ejemplos: [`docs/contracts/README.md`](docs/contracts/README.md). Mock para probarla: [`mocks/catalogo-pedidos-v1`](mocks/catalogo-pedidos-v1).

El grupo del gimnasio también nos consume: con su propia clave ve el catálogo y la disponibilidad (`GET /v1/disponibilidad`, Inventario), sin hacer pedidos.

**Capacidad que consumimos: gimnasio.** FAV vende ropa en consignación en un gimnasio: Inventario consulta cuánto stock nuestro les queda y les registra reposiciones. Contrato: [`docs/contracts/externos/gimnasio/`](docs/contracts/externos/gimnasio/). En local se usa un mock: falta la URL pública y la clave del gimnasio ([ADR-018](docs/adr/ADR-018-consumo-del-gimnasio-contrato-real.md)).

## Decisiones de arquitectura (ADR)

| ADR                                                     | Decisión                          | Estado                     |
| ------------------------------------------------------- | --------------------------------- | -------------------------- |
| [ADR-001](docs/adr/ADR-001-limites-de-los-servicios.md) | D1 · Límites de los servicios     | Aceptada                   |
| [ADR-002](docs/adr/ADR-002-arquitectura-interna.md)     | D2 · Arquitectura interna         | Aceptada                   |
| [ADR-003](docs/adr/ADR-003-persistencia.md)             | D3 · Persistencia                 | Aceptada (versión inicial) |
| [ADR-004](docs/adr/ADR-004-consistencia-y-concurrencia.md) | D4 · Consistencia y concurrencia | Aceptada                   |
| [ADR-005](docs/adr/ADR-005-comunicacion.md)             | D5 · Comunicación entre servicios | Aceptada (versión inicial) |
| [ADR-006](docs/adr/ADR-006-busqueda.md)                 | D6 · Búsqueda                     | Aceptada e implementada    |
| [ADR-007](docs/adr/ADR-007-cache.md)                    | D7 · Caché                        | Aceptada; el motor, reemplazado por ADR-019 |
| [ADR-019](docs/adr/ADR-019-cache-con-memcached.md)      | D7 · Caché con Memcached          | Aceptada                   |
| [ADR-008](docs/adr/ADR-008-contrato-propio.md)          | D8 · Contrato propio              | Aceptada                   |
| [ADR-009](docs/adr/ADR-009-consumo-del-gimnasio.md)     | D9 · Consumo del gimnasio         | Reemplazada por ADR-018 |
| [ADR-018](docs/adr/ADR-018-consumo-del-gimnasio-contrato-real.md) | D9 · Consumo del gimnasio, contrato real | Aceptada |
| [ADR-010](docs/adr/ADR-010-resiliencia.md)              | D10 · Resiliencia                 | Aceptada                   |
| [ADR-011](docs/adr/ADR-011-observabilidad.md)           | D11 · Observabilidad              | Aceptada; trazas y logs reemplazados por ADR-020 |
| [ADR-020](docs/adr/ADR-020-logs-y-track-id-modelo-catedra.md) | D11 · Logs en MySQL y track id (modelo de la cátedra) | Aceptada |
| [ADR-012](docs/adr/ADR-012-balanceo.md)        | D12 · Balanceo de carga           | Aceptada                   |
| [ADR-013](docs/adr/ADR-013-capacidad-y-costos.md)       | D13 · Capacidad y costos          | Aceptada con mediciones    |
| [ADR-014](docs/adr/ADR-014-pagos-mails-y-orden-de-la-compra.md) | Adicional · Simulador de pagos, mails y orden de la compra | Aceptada |
| [ADR-015](docs/adr/ADR-015-claves-del-token-y-cuenta-de-administrador.md) | Adicional · Claves del token, administrador y contraseñas | Aceptada |
| [ADR-016](docs/adr/ADR-016-validacion-del-token-en-el-gateway.md) | Adicional · Validación del token en el gateway | Aceptada |
| [ADR-017](docs/adr/ADR-017-frontend.md) | Adicional · Frontend | Aceptada |

Plantilla: [`ADR-000`](docs/adr/ADR-000-plantilla.md).

## Cómo levantarlo

Con Docker, desde la raíz:

```bash
docker compose up --build
```

No hace falta crear un `.env`: todas las variables tienen valores por defecto para desarrollo. Levanta Inventario (2 réplicas) y su MySQL con stock de ejemplo, Catálogo (2 réplicas) con su MongoDB, los productos de ejemplo, Elasticsearch y Memcached, Ventas y su MySQL, Usuarios y su MySQL, RabbitMQ, Mailpit, el gateway, el simulador de Mercado Pago, el mock del gimnasio, la base de logs (`mysql-logs`), Prometheus, Alertmanager y Grafana.

| Qué | Dónde |
|---|---|
| Gateway (API) | http://localhost:8080 |
| Prometheus (métricas) | http://localhost:9090 |
| Grafana (tablero, sin iniciar sesión) | http://localhost:3000 |
| Logs de una compra | Grafana → tablero **FAV · Logs**: pegar el `X-Correlation-Id` que devuelve el gateway |
| MySQL de logs (`root` / `cambiar`) | `localhost:3310`, solo desde esta máquina |
| Alertmanager (alertas) | http://localhost:9093 |
| Mailpit (mails) | http://localhost:8025 |
| Simulador de Mercado Pago | http://localhost:8096 |
| RabbitMQ (`fav` / `cambiar`) | http://localhost:15672 |

## Sistema desplegado

Una VM de Azure con el mismo Docker Compose, detrás de Caddy con HTTPS. Cómo se despliega y se actualiza: [`desplegar/README.md`](desplegar/README.md).

| Qué | Dónde |
|---|---|
| Tienda y panel de administración | https://fav-ucc.chilecentral.cloudapp.azure.com |
| Capacidad publicada (otros grupos) | `https://fav-ucc.chilecentral.cloudapp.azure.com/v1/…`, con la `X-API-Key` que se le pasa a cada grupo por un canal privado ([contrato](docs/contracts/README.md)) |
| Grafana, Prometheus y Mailpit | No se publican: se ven desde la VM con un túnel SSH ([cómo](desplegar/README.md#6-paneles-por-túnel-ssh)) |

Cómo probar Inventario: [`services/inventario/README.md`](services/inventario/README.md). Catálogo: [`services/catalogo/README.md`](services/catalogo/README.md). Ventas: [`services/ventas/README.md`](services/ventas/README.md). Usuarios: [`services/usuarios/README.md`](services/usuarios/README.md).

## Estructura del repositorio

```
├── SPEC.md                 alcance funcional
├── docs/
│   ├── ARCHITECTURE.md     arquitectura
│   ├── POSTMORTEM.md       ensayo de caídas controladas
│   ├── adr/                decisiones de arquitectura
│   ├── contracts/          contratos OpenAPI de la capacidad compartida
│   └── diagrams/           contexto, contenedores y estados (.drawio y .png)
├── services/               catalogo, inventario, ventas, usuarios
├── gateway/
├── frontend/
├── mocks/                  Mercado Pago, contrato con Prism y gimnasio
├── observabilidad/         Prometheus, alertas, Grafana, base de logs (mysql-logs) y prueba de alertas
├── loadtest/               pruebas de carga (k6)
├── desplegar/              despliegue en la VM de Azure (Caddy y scripts)
├── docker-compose.yml      arranque local
├── docker-compose.cloud.yml  cambios para la nube
├── .env.example            variables, sin valores secretos
└── go.work                 une los módulos Go
```

## Entregas

| Entrega                        | Contenido                                                                                       | Estado   |
| ------------------------------ | ----------------------------------------------------------------------------------------------- | -------- |
| 1 · Diseño y contrato con mock | README, SPEC, ARCHITECTURE, diagramas, límites de los servicios, contrato y ADR D1, D3, D5 y D8 | Entregada |
| 2 · Dominio propio de punta a punta (23/10) | Servicios funcionando, capacidad compartida, logs y traza, más ADR | Entregada |
| Presentación grupal (11 o 13/11) | Frontend, autenticación, tres servicios, capacidad en la nube, mensajería, búsqueda y caché | En curso. Listos: los cuatro servicios, mensajería, búsqueda, caché y el despliegue en Azure. Falta la integración con el gimnasio real |
| Defensa individual | Tests de contrato y carga, balanceo, caída controlada, tablero y alerta | En curso. Listos: tablero y alertas en local, prueba de carga ([ADR-013](docs/adr/ADR-013-capacidad-y-costos.md)) y ensayo de caídas ([POSTMORTEM](docs/POSTMORTEM.md)) |
