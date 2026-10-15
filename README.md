# FAV Tienda Online

Arquitectura de Software 2026
Autores:
Jonathan Brua - 2212370
Juan Hernandez - 2325917

Sistema de venta online y control de stock para **FAV**, una tienda de indumentaria femenina que hoy vende solo en el local y lleva el stock en un Excel. Es también el Trabajo Práctico Integrador de **Arquitectura de Software 2026**.

> **Estado: camino a la Entrega 2.** Todavía no hay código de los servicios. Hay diseño, decisiones, el contrato de la capacidad compartida y su mock. Lo que dice _previsto_ se implementa en entregas siguientes.
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

**Capacidad que publicamos: catálogo y pedidos** (contrato 1.1.0). Otro grupo de la materia puede ver el catálogo de FAV y hacerle pedidos, como un comprador mayorista. El pedido aparta el stock y registra la venta, pendiente de pago por transferencia. FAV le avisa por webhook cuando el pedido se paga o vence.

| Operación | Ruta | Servicio |
|---|---|---|
| Ver el catálogo | `GET /v1/productos` | Catálogo |
| Hacer un pedido | `POST /v1/pedidos` | Ventas (aparta el stock en Inventario) |
| Consultar un pedido | `GET /v1/pedidos/{pedidoId}` | Ventas |
| Aviso de cambio de estado (webhook) | `POST` a la `notificacionUrl` del grupo | Ventas (lo envía FAV) |

Contrato: [`catalogo-pedidos-v1.openapi.yaml`](docs/contracts/catalogo-pedidos-v1.openapi.yaml). Cómo usarla, errores y ejemplos: [`docs/contracts/README.md`](docs/contracts/README.md). Mock para probarla, que también envía los avisos firmados: [`mocks/catalogo-pedidos-v1`](mocks/catalogo-pedidos-v1).

**Capacidad que consumimos: gimnasio.** FAV vende ropa en consignación en un gimnasio: Inventario consulta cuánto stock nuestro les queda y les registra reposiciones. **[?]** Contrato pendiente del grupo del gimnasio.

## Decisiones de arquitectura (ADR)

| ADR                                                     | Decisión                          | Estado                     |
| ------------------------------------------------------- | --------------------------------- | -------------------------- |
| [ADR-001](docs/adr/ADR-001-limites-de-los-servicios.md) | D1 · Límites de los servicios     | Aceptada                   |
| [ADR-003](docs/adr/ADR-003-persistencia.md)             | D3 · Persistencia                 | Aceptada (versión inicial) |
| [ADR-005](docs/adr/ADR-005-comunicacion.md)             | D5 · Comunicación entre servicios | Aceptada (versión inicial) |
| [ADR-008](docs/adr/ADR-008-contrato-propio.md)          | D8 · Contrato propio              | Aceptada                   |

Las demás decisiones se registran en las entregas siguientes. Plantilla: [`ADR-000`](docs/adr/ADR-000-plantilla.md).

## Cómo levantarlo

**Hoy.** Solo se puede levantar el mock de la capacidad compartida:

```bash
cd mocks/catalogo-pedidos-v1
docker compose up      # queda en http://localhost:8090
```

**Previsto (Entrega 2).** Todo el sistema con un solo comando:

```bash
cp .env.example .env   # completar los valores
docker compose up --build
```

El [`docker-compose.yml`](docker-compose.yml) de la raíz ya tiene la infraestructura (bases y RabbitMQ). Los servicios están comentados hasta que existan.

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
├── mocks/                  mocks del contrato publicado
├── docker-compose.yml      previsto: arranque local
├── .env.example            variables, sin valores secretos
└── go.work                 previsto: une los módulos Go
```

## Entregas

| Entrega                        | Contenido                                                                                       | Estado   |
| ------------------------------ | ----------------------------------------------------------------------------------------------- | -------- |
| 1 · Diseño y contrato con mock | README, SPEC, ARCHITECTURE, diagramas, límites de los servicios, contrato y ADR D1, D3, D5 y D8 | Entregada |
| 2 · Dominio propio de punta a punta (23/10) | Servicios funcionando, capacidad compartida, logs y traza, más ADR | En curso |
| Siguientes                     | Caché, búsqueda, observabilidad, pruebas de carga, caída controlada                             | Prevista |
