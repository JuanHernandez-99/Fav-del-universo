# FAV Tienda Online

Arquitectura de Software 2026
Autores:
Jonathan Brua - 2212370
Juan Hernandez - 2325917

Sistema de venta online y control de stock para **FAV**, una tienda de indumentaria femenina que hoy vende solo en el local y lleva el stock en un Excel. Es también el Trabajo Práctico Integrador de **Arquitectura de Software 2026**.

> **Estado: Entrega 2.** **Inventario** funciona (2 réplicas detrás de un gateway nginx, con MySQL, logs, trazas y métricas). Catálogo y Ventas están simulados con un mock y Usuarios y el frontend todavía no existen. Ver las limitaciones en [ARCHITECTURE §16](docs/ARCHITECTURE.md). Lo que dice _previsto_ se implementa en entregas siguientes.
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

**Capacidad que consumimos: gimnasio.** FAV vende ropa en consignación en un gimnasio: Inventario consulta cuánto stock nuestro les queda y les registra reposiciones. **[?]** Contrato pendiente del grupo del gimnasio.

## Decisiones de arquitectura (ADR)

| ADR                                                     | Decisión                          | Estado                     |
| ------------------------------------------------------- | --------------------------------- | -------------------------- |
| [ADR-001](docs/adr/ADR-001-limites-de-los-servicios.md) | D1 · Límites de los servicios     | Aceptada                   |
| [ADR-002](docs/adr/ADR-002-arquitectura-interna.md)     | D2 · Arquitectura interna         | Aceptada                   |
| [ADR-003](docs/adr/ADR-003-persistencia.md)             | D3 · Persistencia                 | Aceptada (versión inicial) |
| [ADR-004](docs/adr/ADR-004-consistencia-y-concurrencia.md) | D4 · Consistencia y concurrencia | Aceptada                   |
| [ADR-005](docs/adr/ADR-005-comunicacion.md)             | D5 · Comunicación entre servicios | Aceptada (versión inicial) |
| [ADR-006](docs/adr/ADR-006-busqueda.md)                 | D6 · Búsqueda                     | Propuesta                  |
| [ADR-007](docs/adr/ADR-007-cache.md)                    | D7 · Caché                        | Propuesta                  |
| [ADR-008](docs/adr/ADR-008-contrato-propio.md)          | D8 · Contrato propio              | Aceptada                   |
| [ADR-009](docs/adr/ADR-009-consumo-del-gimnasio.md)     | D9 · Consumo del gimnasio         | Aceptada (contrato supuesto) |
| [ADR-010](docs/adr/ADR-010-resiliencia.md)              | D10 · Resiliencia                 | Aceptada                   |
| [ADR-011](docs/adr/ADR-011-observabilidad.md)           | D11 · Observabilidad              | Aceptada (primera versión) |
| [ADR-012](docs/adr/ADR-012-balanceo-de-carga.md)        | D12 · Balanceo de carga           | Aceptada                   |
| [ADR-013](docs/adr/ADR-013-capacidad-y-costos.md)       | D13 · Capacidad y costos          | Propuesta                  |

Las decisiones marcadas como _Propuesta_ (D6, D7, D13) son diseño, sin implementar ni medir. Plantilla: [`ADR-000`](docs/adr/ADR-000-plantilla.md).

## Cómo levantarlo

Con Docker, desde la raíz:

```bash
docker compose up --build
```

No hace falta crear un `.env`: todas las variables tienen valores por defecto para desarrollo. Levanta Inventario (2 réplicas), su MySQL con stock de ejemplo, el gateway, los mocks de Catálogo/Ventas y del gimnasio, Jaeger y Prometheus.

| Qué | Dónde |
|---|---|
| Gateway (API) | http://localhost:8080 |
| Jaeger (trazas) | http://localhost:16686 |
| Prometheus (métricas) | http://localhost:9090 |

Cómo probar Inventario: [`services/inventario/README.md`](services/inventario/README.md). Perfil con el resto de la infraestructura (MongoDB, MySQL de Ventas y Usuarios, RabbitMQ): `docker compose --profile completo up`.

## Estructura del repositorio

```
├── SPEC.md                 alcance funcional
├── docs/
│   ├── ARCHITECTURE.md     arquitectura
│   ├── adr/                decisiones de arquitectura
│   ├── contracts/          contratos OpenAPI de la capacidad compartida
│   └── diagrams/           contexto, contenedores y estados (.drawio y .png)
├── services/               catalogo, inventario, ventas, usuarios
├── gateway/
├── frontend/
├── mocks/                  mocks de Catálogo/Ventas y del gimnasio
├── observabilidad/         configuración de Prometheus
├── loadtest/               guion de prueba de carga (k6, sin ejecutar)
├── docker-compose.yml      arranque local
├── .env.example            variables, sin valores secretos
└── go.work                 une los módulos Go
```

## Entregas

| Entrega                        | Contenido                                                                                       | Estado   |
| ------------------------------ | ----------------------------------------------------------------------------------------------- | -------- |
| 1 · Diseño y contrato con mock | README, SPEC, ARCHITECTURE, diagramas, límites de los servicios, contrato y ADR D1, D3, D5 y D8 | Entregada |
| 2 · Dominio propio de punta a punta (23/10) | Servicios funcionando, capacidad compartida, logs y traza, más ADR | En curso |
| Siguientes                     | Caché, búsqueda, observabilidad, pruebas de carga, caída controlada                             | Prevista |
