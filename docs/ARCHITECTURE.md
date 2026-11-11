# Arquitectura — FAV Tienda Online

> **Versión 0.7** · 14/10/2026 · Documento vivo.
>
> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, a validar en el ADR correspondiente · _previsto_: forma parte del diseño pero se implementa en una entrega posterior.
>
> El alcance funcional (reglas, historias y casos de uso) está en [`SPEC.md`](../SPEC.md). Las decisiones se justifican en [`docs/adr/`](adr/).
>
> **Cambios en 0.7:** la cátedra asignó los grupos. La capacidad publicada es **catálogo y pedidos** (Catálogo y Ventas) y se consume la capacidad de un **gimnasio** desde Inventario (§6). Las opciones A y B quedan descartadas (ADR-008).
>
> **Cambios en 0.6:** Inventario pasa a usar arquitectura hexagonal **[propuesta]** (§4 y §13). La tabla de servicios se separa del estilo interno de cada uno.
>
> **Cambios en 0.5:** la capacidad publicada pasa a ser una de dos opciones, A (reserva de cupos, en Inventario) o B (envío de emails, en Ventas), sin servicios nuevos (§6). La reserva se confirma con una llamada síncrona a Inventario y desaparece el evento `VentaPagada` (ADR-005).
>
> **Cambios en 0.4** (ADR-001): identidad del comprador en el token, carrito en el navegador y vista de stock del panel compuesta en el frontend.

---

## 1. Visión general

El sistema se organiza en **cuatro microservicios en Go**, cada uno con **su propia base de datos**, detrás de un **API Gateway**, con un **frontend web** como único punto de interacción del usuario:

| Componente       | Qué resuelve                                                                                                 | Base de datos |
| ---------------- | ------------------------------------------------------------------------------------------------------------ | ------------- |
| **Catálogo**     | Qué se vende y cómo se muestra: productos, variantes, fotos, categorías, precios y promociones.              | MongoDB       |
| **Inventario**   | Cuántas unidades hay y dónde: stock online y físico, reservas, transferencias y ajustes.                     | MySQL         |
| **Ventas**       | Las ventas online y del local, sus pagos (Mercado Pago y transferencia), sus estados y los mails.            | MySQL         |
| **Usuarios**     | Registro, inicio de sesión, emisión del token y roles (comprador y administrador).                           | MySQL         |
| **API Gateway**  | Punto de entrada único: autenticación, ruteo y control de tráfico.                                           | —             |
| **Frontend web** | Tienda para el comprador y panel para el administrador. Guarda el carrito en el navegador. Tecnología libre. | —             |

Cada servicio es dueño de sus datos y nadie accede a la base de otro: la información se obtiene por su API o por sus eventos (_Database per Service_).

## 2. Diagrama de contexto

Muestra el sistema como una caja y con quién interactúa.

![Diagrama de contexto: el comprador registrado, el comprador invitado y el administrador usan el Sistema FAV; el sistema crea pagos en Mercado Pago y recibe su aviso, envía mails mediante un proveedor de mail, le ofrece su catálogo y pedidos al grupo de proveedores y consume la capacidad del gimnasio.](diagrams/contexto.png)

Fuente editable: [`diagrams/contexto.drawio`](diagrams/contexto.drawio). La línea hacia el gimnasio está punteada porque se dibujó antes de que existiera su contrato; hoy está definido ([ADR-018](adr/ADR-018-consumo-del-gimnasio-contrato-real.md)).

## 3. Diagrama de contenedores

Muestra las piezas desplegables dentro del sistema y cómo se comunican.

![Diagrama de contenedores: el frontend web llama al API Gateway, que rutea a los servicios Usuarios, Catálogo, Inventario y Ventas, cada uno con su propia base (MySQL o MongoDB). Ventas consulta el precio a Catálogo y reserva stock en Inventario, crea pagos en Mercado Pago y envía mails. Catálogo, Inventario y Ventas intercambian eventos por RabbitMQ. Caché, motor de búsqueda y observabilidad están previstos. El grupo de proveedores entra por el gateway a Catálogo (ver el catálogo) y a Ventas (hacer pedidos). Inventario llama a la API del gimnasio.](diagrams/contenedores.png)

Fuente editable: [`diagrams/contenedores.drawio`](diagrams/contenedores.drawio). Las líneas y los bordes punteados son componentes o relaciones previstos o pendientes de definir. Al pie del diagrama, una tabla relaciona cuatro historias con los contenedores que intervienen.

**Diferencias con lo implementado (04/11/2026).** Los diagramas muestran el diseño del 14/10 y se actualizan una sola vez, cuando estén decididos el frontend y la búsqueda:

- Gateway y observabilidad ya tienen tecnología: nginx, Prometheus, Grafana y MySQL para los logs (ADR-020). Inventario y Catálogo tienen dos réplicas.
- Mercado Pago y el proveedor de mail se reemplazan en local por un simulador y Mailpit ([ADR-014](adr/ADR-014-pagos-mails-y-orden-de-la-compra.md)).
- Ventas llama a Catálogo e Inventario por la entrada interna del gateway, no directo. Ventas no publica eventos ni tiene outbox: solo consume `ReservaVencida`.
- Estados: también llegan a *A verificar* una venta pendiente cuya reserva venció al confirmar y una cancelada que recibe un pago. Un pago rechazado la deja *Cancelada* **[propuesta]**.

**Cómo se actualizan:** se edita el `.drawio` (en [draw.io](https://app.diagrams.net) o con la extensión de VS Code) y se vuelve a exportar el `.png` con el mismo nombre. El `.png` es lo que se ve en GitHub; el `.drawio` es la fuente.

## 4. Servicios, responsabilidades y datos

| Servicio | Responsabilidad | Datos que posee |
|---|---|---|
| Catálogo | Productos, variantes, fotos, categorías, precios base, promociones y publicación. Calcula el precio vigente. Expone el catálogo al grupo de proveedores. | Productos con sus variantes, promociones |
| Inventario | Stock por variante y canal, reservas con vencimiento, transferencias, ingresos, ajustes y movimientos. Consulta y repone el stock en consignación del gimnasio. | Stock, reservas, movimientos (incluidas las salidas a consignación), reposiciones pendientes, outbox, mensajes procesados. |
| Ventas | Ventas online y del local, estados, pagos (Mercado Pago y transferencia) y envío de mails. Recibe los pedidos del grupo de proveedores. | Ventas (con el id y el email del comprador, o el grupo externo que compró), ítems, pagos, tareas pendientes hacia Inventario, registro de mails, mensajes procesados. |
| Usuarios | Registro, inicio de sesión, emisión del token (id, email y rol) y roles. | Usuarios, hash de contraseña, rol |

### Estilo interno de cada servicio (D2)

| Servicio | Estilo | Por qué |
|---|---|---|
| Catálogo | **Capas**: Handler → Service → Repository | Pocas reglas: casi todo es alta, edición y lectura de productos por un solo administrador. La única regla con peso es el precio vigente con promociones, y entra en la capa de servicio. La carga de lectura se resuelve con caché e índice, no con la estructura interna. **[propuesta]** Lecturas servidas por el índice de búsqueda (CQRS liviano). |
| Usuarios | **Capas** | Registro, inicio de sesión y emisión del token. El email único lo garantiza la base y no hay proveedores externos que puedan cambiar. Puertos y adaptadores agregarían piezas sin una variación que proteger. |
| Ventas | **Hexagonal** | Tiene el ciclo de estados de la venta, con transiciones que no pueden saltearse ni repetirse (no cobrar dos veces). Depende de varios sistemas externos que pueden cambiar: Mercado Pago, el proveedor de mail (todavía sin elegir), RabbitMQ y las APIs de Catálogo e Inventario. Recibe pedidos por varias entradas: comprador, administrador, aviso de Mercado Pago y eventos. Los puertos permiten probar las reglas con adaptadores falsos y cambiar un proveedor sin tocar el núcleo. |
| Inventario | **Hexagonal** | Tiene las reglas más críticas del sistema: no vender de más, reservar todo o nada, transferir sin dejar stock negativo y las transiciones de la reserva (activa, confirmada, liberada, vencida). Recibe pedidos por varias entradas: Ventas, el panel, el evento `VarianteCreada` y el proceso que vence reservas. Y consume la capacidad del gimnasio (D9), un proveedor cuyo contrato no controlamos. La carrera entre dos reservas la resuelve la base con una actualización condicional: el puerto del repositorio expresa la operación completa ("reservar si alcanza") y el adaptador de MySQL la hace atómica. El núcleo valida el resto de las reglas. |

Con esto se cumplen las dos arquitecturas internas distintas que pide la consigna: **capas** (Catálogo y Usuarios) y **hexagonal** (Ventas e Inventario). El criterio sigue el apunte de estilos de backend: capas cuando hay pocas reglas y consultas sencillas; hexagonal cuando hay invariantes de negocio o el núcleo depende de proveedores externos que pueden cambiar.

### Qué no hace cada servicio

- **Catálogo** no sabe cuántas unidades hay: la disponibilidad es de Inventario.
- **Inventario** no conoce precios, compradores ni nombres de productos: solo ids de variante, cantidades y canales.
- **Ventas** no modifica stock directamente: se lo pide a Inventario.
- **Usuarios** no conoce ventas: Ventas guarda la referencia al comprador.
- **El gateway** no tiene lógica de negocio ni compone respuestas de varios servicios.

### Por qué estos límites

Se resumen acá y se justifican en el [**ADR-001 (D1)**](adr/ADR-001-limites-de-los-servicios.md):

- **Catálogo e Inventario están separados** porque tienen cargas y garantías distintas: el catálogo se lee mucho y tolera unos segundos de atraso; el stock no puede venderse de más ni perder unidades. Separarlos permite escalar y cachear el catálogo sin tocar el stock, y deja la reserva y la transferencia contenidas en un solo servicio.
- **Los pagos están dentro de Ventas**, como adaptador hacia Mercado Pago, porque no hay reglas de pago propias que justifiquen otro servicio.
- **No hay servicio de notificaciones:** Ventas envía los mails mediante un puerto propio. Si en el futuro hace falta, el adaptador de mail se puede extraer a un servicio sin cambiar la lógica de negocio.
- **Las integraciones con otros grupos no suman servicios:** el catálogo se expone desde Catálogo, los pedidos entran a Ventas y la consignación sale desde Inventario. Cada una es una entrada o salida aparte, que se puede desactivar cuando el sistema pase al cliente.
- **No hay servicio de carrito:** el carrito no reserva stock, así que no tiene reglas ni garantías. Vive en el navegador y se envía completo a Ventas al comprar, que vuelve a validar precio, publicación y stock.
- **Ventas no consulta a Usuarios:** la identidad del comprador llega en el token (ver §6). Comprar no depende de que Usuarios esté disponible.
- **Inventario no copia datos de Catálogo:** la vista de stock del panel la compone el frontend con datos de los dos servicios.

## 5. Propiedad de los datos

| Dato                                 | Dueño                                                               | Quién más lo usa y cómo                                                                                                                                                                                                                                                         |
| ------------------------------------ | ------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Producto, variante, fotos, categoría | Catálogo                                                            | Inventario conoce solo el identificador de la variante. Ventas guarda una copia de nombre, talle, color y precio en cada venta **[propuesta]**, para que una venta registrada conserve su precio (HU-05). El frontend del panel los combina con el stock de Inventario (HU-09). |
| Precio base y promociones            | Catálogo                                                            | Ventas consulta el precio vigente al momento de comprar **[propuesta]**.                                                                                                                                                                                                        |
| Stock, reservas y movimientos        | Inventario                                                          | Ventas reserva, libera y descuenta mediante su API. El panel consulta el stock por ids de variante. La tienda consulta las unidades online disponibles con `GET /v1/disponibilidad`, sin físico ni reservadas.                                                                                          |
| Venta, ítems, pagos y estados        | Ventas                                                              | El comprador registrado los consulta a través del gateway; se filtran por su id de usuario.                                                                                                                                                                                     |
| Usuario, credenciales y rol          | Usuarios                                                            | El gateway valida el token. Ventas recibe el id y el email del comprador desde el token y los guarda en cada venta.                                                                                                                                                             |
| Carrito                              | Frontend (navegador del comprador)                                  | No llega al backend hasta que el comprador aprieta "Comprar".                                                                                                                                                                                                                   |
| Pedidos de grupos externos | Ventas (la venta) e Inventario (el stock apartado) | Se identifican por la clave del grupo. Se registran como ventas con el canal *grupo externo*. |
| Stock en consignación | Gimnasio | FAV lo consulta por su API. Inventario registra las salidas a consignación y las reposiciones. |
| Índice de búsqueda                   | Modelo de lectura derivado                                          | No es fuente de verdad: se reconstruye a partir de Catálogo e Inventario.                                                                                                                                                                                                       |
| Caché                                | Copia temporal                                                      | No es fuente de verdad: puede estar ausente o vencida.                                                                                                                                                                                                                          |

**Una base de datos por servicio:** cada servicio tiene su propia instancia (un contenedor MongoDB para Catálogo y un contenedor MySQL para cada uno de Inventario, Ventas y Usuarios). Así lo pide la cátedra, y además la caída de una base afecta solo a su servicio. Se justifica en el **ADR-003 (D3)**.

## 6. Comunicaciones

### Identidad entre el gateway y los servicios

Al iniciar sesión, Usuarios emite un JWT con `sub` (id), `email`, `role`, `iat` y `exp`. En cada pedido, el gateway valida la firma y el vencimiento **sin llamar a Usuarios**, descarta los encabezados de identidad que vengan del cliente y reenvía `X-User-Id`, `X-User-Email` y `X-User-Role` a los servicios, que confían en ellos solo dentro de la red interna. **[propuesta]** Firma RS256 (Usuarios firma con la clave privada; el gateway verifica con la pública) y vencimiento de 1 hora, sin refresh token en v1. Se justifica en el **ADR-001**.

### Síncronas (HTTP)

| Origen → destino                                         | Para qué                                                                                          | Protección                                                                          | Si falla o no responde a tiempo                              |
| -------------------------------------------------------- | ------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- | ------------------------------------------------------------ |
| Frontend → Gateway → servicios                           | Todas las operaciones del usuario                                                                 | Token validado en el gateway; rate limiting                                         | Error claro en la interfaz                                   |
| Frontend (tienda) → Catálogo e Inventario | Ver productos y si hay stock: son dos pedidos y el frontend junta las respuestas | Rutas públicas de solo lectura | Si falla Inventario, se ven los productos sin saber si hay stock |
| Frontend (panel) → Catálogo e Inventario                 | Vista de stock por producto y filtro _stock a corregir_: el frontend junta variantes y cantidades | Token de administrador                                                              | Si falla Catálogo, error claro en lugar de datos incompletos |
| Ventas → Catálogo                                        | Precio vigente al comprar **[propuesta]**                                                         | Timeout                                                                             | No se puede iniciar la compra; error claro                   |
| Ventas → Inventario                                      | Reservar, liberar, confirmar al pagar y descontar stock                                           | Timeout, reintento acotado solo ante fallas transitorias, clave de idempotencia     | Error claro; no queda una venta ni una reserva a medias      |
| Ventas → Mercado Pago                                    | Crear el pago                                                                                     | Timeout y circuit breaker                                                           | La compra no avanza y la reserva vence sola                  |
| Mercado Pago → Gateway → Ventas                          | Aviso del resultado del pago                                                                      | Procesamiento idempotente                                                           | Mercado Pago reintenta el aviso                              |
| Ventas → proveedor de mail                               | Enviar mails                                                                                      | Envío fuera de la transacción de la venta, con reintentos **[propuesta]**           | Se registra como fallido; la venta no se afecta              |
| Grupo de proveedores → Gateway → Catálogo y Ventas | Ver el catálogo y hacer pedidos (capacidad publicada) | Clave por grupo **[propuesta]**, clave de idempotencia, límite de pedidos por grupo | Errores definidos en el contrato |
| Grupo del gimnasio → Gateway → Catálogo e Inventario | Ver el catálogo y la disponibilidad (capacidad publicada, sin pedidos) | Clave propia y límite por grupo | Errores definidos en el contrato |
| Inventario → Gimnasio | Consultar su stock de FAV y registrar reposiciones (capacidad consumida) | Timeout, reintentos con la misma clave y circuit breaker (ADR-018) | La reposición queda pendiente y se reintenta; el panel muestra que los datos del gimnasio no están disponibles |

Los valores concretos de timeouts y reintentos se definen en el **ADR-005 (D5)**.

### Capacidad publicada: catálogo y pedidos

El grupo de proveedores ve el catálogo de FAV y le hace pedidos para sí mismo, como un comprador mayorista.

| Operación | Servicio | Qué hace |
|---|---|---|
| `GET /v1/productos` | Catálogo | Productos publicados con sus variantes y el precio vigente. No informa stock. |
| `GET /v1/disponibilidad` | Inventario | Unidades online sin reservar por variante **[propuesta]**. También para el grupo del gimnasio, con su clave (contrato 1.1.0). |
| `POST /v1/pedidos` | Ventas | Pide el precio a Catálogo, aparta el stock online en Inventario por 48 horas (todo o nada) y registra la venta *Pendiente de pago* por transferencia. Desde ahí sigue como una compra por transferencia (CU-02). |

Contrato: [`catalogo-pedidos-v1.openapi.yaml`](contracts/catalogo-pedidos-v1.openapi.yaml). Uso, errores y ejemplos en [`contracts/README.md`](contracts/README.md). Se justifica en el **ADR-008 (D8)**.

### Capacidad consumida: venta en consignación en un gimnasio

La integración con el gimnasio va en los dos sentidos:

```
FAV consume:   Panel → Gateway → Inventario ──(X-API-Key)──► API de consignaciones del gimnasio
FAV publica:   Tienda del gimnasio ──(X-API-Key)──► Gateway → Catálogo (productos) e Inventario (disponibilidad)
```

El gimnasio vende ropa de FAV y se queda con una comisión. **Inventario** llama a su API para consultar cuánto stock de FAV le queda y para registrar reposiciones. Al reponer, Inventario descuenta el stock físico y guarda una *reposición pendiente* en la misma transacción (con outbox); un proceso la envía al gimnasio con una clave de idempotencia y la reintenta si el resultado es incierto. Solo un rechazo definitivo devuelve las unidades al stock físico: un `401`, `403` o `409` no.

Inventario lo hace con un adaptador de salida que traduce el modelo del gimnasio al de FAV. Contrato: [`externos/gimnasio/`](contracts/externos/gimnasio/). Se justifica en el **ADR-018 (D9)**, que reemplaza al ADR-009. **[?]** URL pública y clave del gimnasio: hasta tenerlas, se usa el mock local.

### Asíncronas (RabbitMQ, con transactional outbox)

| Evento                                         | Publica               | Consume                              | Efecto                                             |
| ---------------------------------------------- | --------------------- | ------------------------------------ | -------------------------------------------------- |
| `ReservaVencida`                               | Inventario            | Ventas                               | Pasa la venta a _Vencida_.                         |
| `VarianteCreada` **[propuesta]**               | Catálogo              | Inventario                           | Registra la variante con stock 0 en ambos canales. |
| `ProductoCambiado` | Catálogo | Catálogo (indexador) | Actualiza el índice de búsqueda (ADR-006). |

- **Consumidores idempotentes:** procesar dos veces el mismo mensaje tiene el mismo efecto que procesarlo una vez.
- **Mensajes que no se pueden procesar:** después de agotar los reintentos van a una cola de mensajes fallidos (DLQ), donde quedan para revisarlos.
- **Si RabbitMQ no está disponible:** los eventos quedan guardados en el outbox de cada servicio y se publican cuando vuelve. Lo síncrono sigue funcionando.

## 7. Operaciones críticas

Las dos operaciones que no pueden duplicarse ni perder información (requisito 2.8) viven en **Inventario**:

- **Reserva de stock online** (CU-01, CU-02): nunca reservar más de lo disponible, ni dos veces la misma compra.
- **Transferencia entre stocks** (CU-03): restar y sumar en una sola transacción, sin quedar negativo ante operaciones concurrentes.

La venta del local es la excepción deliberada: no se bloquea si el stock físico registrado es insuficiente, porque la prenda puede estar en el local aunque el registro esté mal. En ese caso el stock físico queda en negativo y la variante se marca como _stock a corregir_ (SPEC, RN-06 y RN-25). El stock online nunca queda negativo.

El tratamiento de concurrencia, duplicados y fallas parciales se detalla en el **ADR-004 (D4)**.

## 8. Comportamiento ante fallas (preliminar)

Si algo se cae, lo demás tiene que seguir funcionando y tiene que poder saberse qué pasó. Este es el comportamiento esperado; se valida con caídas provocadas y se justifica en el **ADR-010 (D10)**.

| Se cae                                  | Sigue funcionando                                                                                                                        | Deja de funcionar                                                                                       | Cómo se contiene                                                    |
| --------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------- |
| Una réplica de Catálogo                 | Todo: la otra réplica atiende                                                                                                            | —                                                                                                       | Balanceo con verificación de disponibilidad (D12)                   |
| Catálogo (todas las réplicas)           | Iniciar sesión, panel de ventas; venta del local con el monto cargado a mano **[propuesta]**                                             | Navegar, buscar, comprar online, ver el stock con nombres en el panel y la capacidad publicada (catálogo y pedidos)                                  | Timeout desde Ventas; error claro                                   |
| Inventario | Navegar y buscar, sin saber si hay stock | Comprar online, registrar ventas del local, ver y mover stock, pedidos de grupos externos, reponer al gimnasio | Timeout y reintento acotado; nunca queda una venta a medias |
| Ventas | Navegar, buscar, gestionar productos y stock | Comprar, registrar ventas, ver pedidos, pedidos de grupos externos | El gateway devuelve un error claro |
| Un pico de pedidos del grupo de proveedores | Todo | — | Límite de pedidos por grupo **[propuesta]**: el exceso recibe `429` |
| El gimnasio | Todo lo de FAV | Ver el stock del gimnasio y enviarle reposiciones | Timeout y circuit breaker; las reposiciones pendientes se envían cuando vuelve |
| Usuarios                                | Navegar, buscar, comprar como invitado y las sesiones ya iniciadas, porque el gateway valida el token sin consultar a Usuarios (ADR-001) | Iniciar sesión y registrarse                                                                            | —                                                                   |
| RabbitMQ                                | Todo lo síncrono                                                                                                                         | Los efectos asíncronos se demoran: paso a _Vencida_, variantes nuevas en Inventario, índice             | Outbox: los eventos se publican cuando vuelve                       |
| Mercado Pago                            | Navegar; comprar por transferencia                                                                                                       | Pagar con Mercado Pago                                                                                  | Timeout y circuit breaker; se ofrece la transferencia               |
| Proveedor de mail                       | Todo                                                                                                                                     | Los mails se demoran                                                                                    | Reintentos y registro de mails fallidos                             |
| Memcached (caché)                           | Todo, más lento                                                                                                                          | —                                                                                                       | Las lecturas van directo a la fuente                                |
| Motor de búsqueda                       | Navegar por categoría desde la base de Catálogo                                                                                          | Búsqueda por texto y filtros                                                                            | Error claro; el índice se reconstruye al volver                     |
| La base de un servicio                  | Los demás servicios                                                                                                                      | Ese servicio                                                                                            | Una base por servicio: la falla no se propaga                       |

## 9. Caché y búsqueda

- **Herramientas existentes, no implementaciones propias:** la caché usa Memcached y la búsqueda un motor de búsqueda, ambos a través de sus bibliotecas cliente para Go. No se programa una caché ni un índice desde cero.
- **Caché (implementada):** Memcached delante de las lecturas de la tienda: ficha, listados sin texto y promociones para mostrar. El stock y el precio para cobrar **no** se leen de la caché (**[ADR-007](adr/ADR-007-cache.md) y [ADR-019](adr/ADR-019-cache-con-memcached.md), D7**).
- **Que la caché no quede mal cargada:** al cambiar un producto o una promoción sube una generación que forma parte de cada clave, y cada entrada vence por tiempo. Con la caché, las consultas a MongoDB de estas lecturas bajan más de un 99 %; ver los resultados en el ADR-007.
- **Búsqueda (implementada):** Elasticsearch con un índice de productos publicados, actualizado por eventos con un outbox dentro de cada producto. Retraso máximo de 10 s **[propuesta]**, medido hasta 1,9 s, y reconstrucción desde cero sin corte (**[ADR-006](adr/ADR-006-busqueda.md), D6**). La disponibilidad no se indexa: la tienda la consulta a Inventario.

## 10. Observabilidad

El estado de una operación tiene que poder entenderse sin revisar cada componente a mano (**[ADR-011](adr/ADR-011-observabilidad.md), D11**).

**Implementado en los cuatro servicios y el gateway:** logs JSON por stdout con `servicio`, `instancia` y `correlation_id` (el track id, que viaja en cada llamada HTTP y en cada evento); las líneas de INFO o más de los servicios también van a la tabla `app_logs` de `mysql-logs`, y en Grafana el tablero "FAV · Logs" muestra todo lo de un track id: una compra se ve desde los precios en Catálogo hasta el pago, la confirmación en Inventario y los mails (**[ADR-020](adr/ADR-020-logs-y-track-id-modelo-catedra.md)**); log de acceso del gateway, solo en stdout, con la réplica que atendió; métricas Prometheus (`:9090`) con una serie por réplica y 15 días de historial; tablero en Grafana (`:3000`); 14 alertas que Alertmanager avisa por mail, a Mailpit en local. **Falta:** el destino real de las alertas en la nube. El objetivo completo es:

- **Logs estructurados** en todos los servicios y en el gateway, con un identificador de correlación que viaja en cada llamada y en cada evento. Permite seguir una compra de punta a punta. El token nunca se escribe en los logs.
- **Métricas** por servicio: pedidos por endpoint y código de estado (incluidos 4xx y 5xx), latencia, fallas de cada dependencia, mensajes en la DLQ, aciertos de caché y estado de cada réplica.
- **Historial:** las métricas se conservan el tiempo suficiente para responder preguntas sobre el pasado, por ejemplo cuántos errores 404 hubo la semana pasada. **[propuesta]** al menos dos semanas.
- **Seguimiento de una compra entre servicios** con el track id (ADR-020).
- **Tablero** con el estado general del sistema y al menos una alerta.
- **Herramientas:** MySQL con GORM para los logs (ADR-020), Prometheus, Grafana y Alertmanager (ADR-011).

## 11. Pruebas de carga

Se simula un **pico de demanda** como los que describe el cliente (cambio de temporada, diciembre): muchos compradores navegando y buscando a la vez, y compras concurrentes sobre las mismas variantes. Se mide latencia, errores, reparto entre las réplicas de Catálogo y el comportamiento de las reservas. El volumen real es bajo (10 a 20 prendas por semana), así que el escenario de pico es una hipótesis que se documenta como tal (**[ADR-013](adr/ADR-013-capacidad-y-costos.md), D13**). Los guiones de k6 están en `loadtest/`; los resultados, el principal límite y los costos, en el ADR-013.

## 12. Distribución de los componentes

- **Local:** todo se levanta con un solo comando mediante Docker Compose, en cualquier computadora con Docker y sin configuración manual: frontend, gateway, los cuatro servicios (Inventario y Catálogo con dos réplicas), MongoDB, cuatro MySQL (Inventario, Ventas, Usuarios y logs), RabbitMQ, Memcached, Elasticsearch y las herramientas de observabilidad. Los secretos van en un archivo `.env` que no se sube al repositorio, y las claves del token las genera `usuarios-init` en volúmenes de Docker (ADR-015).
- **Balanceo:** en la Entrega 2 el servicio replicado es **Inventario** (2 réplicas con `deploy.replicas`), detrás de un gateway nginx con round robin y verificación pasiva ([ADR-012](adr/ADR-012-balanceo.md), D12). Catálogo también tiene 2 réplicas, con el mismo mecanismo.
- **Estado actual del Compose:** gateway, Inventario ×2 y su MySQL, Catálogo ×2 y su MongoDB (con `catalogo-init`, que prepara la base), Elasticsearch, Memcached, Ventas y su MySQL, RabbitMQ, Mailpit, Usuarios y su MySQL (con `usuarios-init`, que genera las claves del token), simulador de Mercado Pago, mock del gimnasio, `mysql-logs`, Prometheus, Alertmanager y Grafana.
- **Nube:** una VM de Azure con el mismo Docker Compose, detrás de Caddy con HTTPS. Desde internet solo se llega a la tienda, el panel, la capacidad publicada y la página de pago simulada ([`desplegar/README.md`](../desplegar/README.md)).

## 13. Estructura del repositorio

Lo que todavía no existe en el repositorio es previsto y se agrega en entregas siguientes.

```
/
├── README.md
├── SPEC.md
├── docker-compose.yml          # arranque local con un solo comando
├── .env.example                # variables necesarias, sin valores secretos
├── .gitignore                  # incluye .env
├── go.work                     # une los módulos Go para desarrollo local
├── .github/
│   └── pull_request_template.md
├── docs/
│   ├── ARCHITECTURE.md
│   ├── POSTMORTEM.md           # ensayo de caídas controladas
│   ├── diagrams/
│   │   ├── contexto.drawio     # fuente editable
│   │   ├── contexto.png        # exportado, se muestra en ARCHITECTURE.md
│   │   ├── contenedores.drawio
│   │   ├── contenedores.png
│   │   ├── estados.drawio      # estados de la venta online, se muestra en SPEC.md
│   │   └── estados.png
│   ├── adr/
│   │   ├── ADR-000-plantilla.md
│   │   ├── ADR-001 … ADR-013   # uno por decisión de la consigna (D1 a D13)
│   │   └── ADR-008-contrato-propio.md
│   └── contracts/
│       ├── README.md
│       └── catalogo-pedidos-v1.openapi.yaml   # capacidad publicada
├── services/
│   ├── catalogo/
│   ├── inventario/
│   ├── ventas/
│   └── usuarios/
├── gateway/                    # nginx: entrada pública, balanceo y límites
├── observabilidad/             # configuración de Prometheus
├── loadtest/                   # guion de carga (k6)
├── frontend/
└── mocks/                      # contrato publicado, Mercado Pago y gimnasio
    ├── catalogo-pedidos-v1/
    ├── mercadopago-simulador/
    └── gimnasio-consignacion/
```

**Cada servicio** tiene su propio `go.mod`, `Dockerfile` y `README.md`, y se puede construir y desplegar por separado. Su organización interna sigue el estilo del servicio, que se decide en el D2. Estas estructuras son provisorias:

```
services/catalogo/              # capas
├── cmd/catalogo/main.go
├── internal/
│   ├── handler/
│   ├── service/
│   └── repository/
├── Dockerfile
├── go.mod
└── README.md

services/ventas/                # hexagonal
├── cmd/ventas/main.go
├── internal/
│   ├── dominio/                # venta, estados y reglas, sin dependencias externas
│   ├── aplicacion/             # casos de uso
│   ├── puertos/                # interfaces que necesita el núcleo
│   └── adaptadores/            # http, mysql, mercadopago, mail, rabbitmq
├── Dockerfile
├── go.mod
└── README.md

services/inventario/            # hexagonal
├── cmd/inventario/main.go
├── internal/
│   ├── dominio/                # stock, reserva, transferencia y sus reglas
│   ├── aplicacion/             # casos de uso: reservar, confirmar, liberar, transferir, ajustar
│   ├── puertos/                # interfaces que necesita el núcleo
│   └── adaptadores/            # http, mysql, rabbitmq, proceso de vencimiento
├── Dockerfile
├── go.mod
└── README.md
```

**Numeración de los ADR:** el número coincide con la decisión de la consigna (ADR-001 = D1, …, ADR-013 = D13). Las decisiones adicionales empiezan en ADR-014.

## 14. Decisiones registradas

| ADR                                                | Decisión                          | Estado                                               |
| -------------------------------------------------- | --------------------------------- | ---------------------------------------------------- |
| [ADR-000](adr/ADR-000-plantilla.md)                | Plantilla                         | —                                                    |
| [ADR-001](adr/ADR-001-limites-de-los-servicios.md) | D1 · Límites de los servicios     | Aceptada · Entrega 1                                 |
| [ADR-003](adr/ADR-003-persistencia.md)             | D3 · Persistencia                 | Aceptada (versión inicial) · Entrega 1               |
| [ADR-005](adr/ADR-005-comunicacion.md)             | D5 · Comunicación entre servicios | Aceptada (versión inicial) · Entrega 1               |
| [ADR-008](adr/ADR-008-contrato-propio.md) | D8 · Contrato propio | Aceptada · Entrega 2 |
| [ADR-002](adr/ADR-002-arquitectura-interna.md)     | D2 · Arquitectura interna         | Aceptada · implementada en los cuatro servicios      |
| [ADR-004](adr/ADR-004-consistencia-y-concurrencia.md) | D4 · Consistencia y concurrencia | Aceptada · Entrega 2 (7 pruebas de integración)      |
| [ADR-006](adr/ADR-006-busqueda.md)                 | D6 · Búsqueda                     | Aceptada e implementada                              |
| [ADR-007](adr/ADR-007-cache.md)                    | D7 · Caché                        | Reemplazada en el motor por ADR-019                  |
| [ADR-019](adr/ADR-019-cache-con-memcached.md)      | D7 · Caché con Memcached          | Aceptada                                             |
| [ADR-009](adr/ADR-009-consumo-del-gimnasio.md)     | D9 · Consumo del gimnasio         | Reemplazada por ADR-018                              |
| [ADR-018](adr/ADR-018-consumo-del-gimnasio-contrato-real.md) | D9 · Consumo del gimnasio, contrato real | Aceptada · Presentación grupal |
| [ADR-010](adr/ADR-010-resiliencia.md)              | D10 · Resiliencia                 | Aceptada · Entrega 2                                 |
| [ADR-011](adr/ADR-011-observabilidad.md)           | D11 · Observabilidad              | Aceptada en métricas, tablero y alertas; trazas y logs reemplazados por ADR-020 |
| [ADR-020](adr/ADR-020-logs-y-track-id-modelo-catedra.md) | D11 · Logs en MySQL y track id | Aceptada · implementada en local y en la nube |
| [ADR-012](adr/ADR-012-balanceo.md)        | D12 · Balanceo de carga           | Aceptada · Entrega 2                                 |
| [ADR-013](adr/ADR-013-capacidad-y-costos.md)       | D13 · Capacidad y costos          | Aceptada con mediciones                              |

### Tecnologías respecto de las usadas en clase

La consigna establece qué condiciones cumplir y no cómo: "las decisiones de diseño, arquitectura y tecnología serán responsabilidad de cada grupo". Esta tabla resume en qué se parece y en qué se aparta FAV de lo usado en clase, y por qué.

| En clase | En FAV | Por qué | Dónde |
|---|---|---|---|
| Capas con repositorios, interfaces y mocks | Igual en Catálogo y Usuarios; **hexagonal** en Inventario y Ventas | La consigna pide dos estilos. Hexagonal protege las reglas de stock y aísla al gimnasio | ADR-002 |
| Go | Igual | Los procesos en segundo plano (vencer reservas, enviar reposiciones, publicar eventos) son gorrutinas en el mismo servicio. Compila a un solo binario: una réplica de Catálogo usó unos 16 MB de memoria en reposo | — |
| MySQL | Igual | Reservar toca varias variantes a la vez: necesita transacciones, bloqueo de filas y restricciones únicas | ADR-003 |
| MongoDB | Igual en Catálogo | Un producto con sus variantes y atributos variables se lee y se escribe como un solo documento. Escribir un documento es atómico: el outbox de búsqueda no necesita transacciones | ADR-003, ADR-006 |
| RabbitMQ | Igual | Con este volumen, resuelve colas, reintentos y DLQ. Kafka no justifica su costo de operación | ADR-005 |
| NGINX | Igual | Un solo componente hace de gateway y balanceador, y descubre réplicas por el DNS de Docker. Traefik o HAProxy suman una pieza más | ADR-012 |
| Docker Compose | Igual | La consigna pide un arranque único y automatizado, sin configuración manual. Kubernetes es un bonus opcional y sumaría operación sin una necesidad en este volumen | RNF-07 |
| `go test` | Igual | Viene con el lenguaje. Los tests de integración corren contra la dependencia real cuando se define su variable de entorno, y se saltean si no | — |
| Prometheus | Igual | Va a buscar las métricas de cada réplica, lo que muestra el reparto del balanceo. Guarda historial para consultar el pasado | ADR-011 |
| Grafana | Igual | Muestra el estado general en una pantalla, y el tablero se versiona como JSON | ADR-011 |
| Track id | Igual: es el `X-Correlation-Id` (también se acepta `X-Track-ID`) | Permite seguir un pedido entre servicios y eventos. La venta lo guarda, así el pago y los mails quedan con el de la compra | ADR-005, ADR-020 |
| ccache o Memcached | **Memcached** | ccache vive en cada réplica: responderían distinto y la invalidación no llegaría a todas | ADR-019 |
| Go con Gin | Go con **`net/http`** | El router estándar ya cubre lo necesario, sin una dependencia más | ADR-002 |
| MySQL con GORM | MySQL con **`database/sql`** en las bases de negocio; **GORM** para los logs | Las garantías de stock dependen de consultas exactas, y así se ve y se prueba lo que se ejecuta. Los logs son solo inserciones por tandas | ADR-003, ADR-020 |
| Apache Solr | **Elasticsearch** | Resuelve de forma más directa los errores de tipeo: con una opción de la consulta, mientras que Solr necesitó código en el cliente y un campo extra (sin eso, 0 resultados con "remra"). Se compararon en 16 casos: los dos cumplen, y Solr usó menos memoria | ADR-006 |
| Logs en MySQL | Igual, en una base propia (`mysql-logs`), y además por stdout | Se escriben por tandas y sin esperar: con la base caída, las compras siguen y las líneas quedan en stdout | ADR-020 |

**Agregados que pide la consigna:** circuit breaker (2.9), prueba de carga con k6 (2.11), test de contrato con `kin-openapi` (punto 4) y alerta con Alertmanager (defensa). El frontend, con React y Vite, se justifica en ADR-017.

## 15. Pendientes de arquitectura

| Tema                                                                                          | Dónde se resuelve                         |
| --------------------------------------------------------------------------------------------- | ----------------------------------------- |
| URL pública y clave del gimnasio; prueba conjunta | D9 (ADR-018) |
| Cómo se entrega y se rota la clave de cada grupo consumidor, y el límite de pedidos por grupo | D8                                        |
| Dónde se almacenan las fotos de los productos                                                 | D3                                        |

## 16. Limitaciones conocidas y deuda técnica

Lo que sabemos que no está o no funciona del todo. Se declara para la presentación.

| Tema | Situación | Dónde |
| ---- | --------- | ----- |
| Servicios implementados | **Inventario** es real. **Catálogo** es real: productos, promociones, consulta por lote, catálogo público, búsqueda con Elasticsearch, caché con Memcached y métricas. **Ventas** y **Usuarios** son reales. La capacidad publicada ya no pasa por mocks. El frontend tiene la tienda, la compra, "Mis compras" y el panel; la política de cambios está publicada, sin el texto ni los contactos del cliente (ADR-017) | — |
| Gateway único | Punto único de falla | ADR-012 |
| Encabezados de identidad | Los servicios confían en `X-User-*`: en la nube sus puertos no pueden quedar abiertos | ADR-016 |
| Integración con el gimnasio | Contrato real aplicado, pero probada solo contra el mock local: falta su URL pública, su clave y la prueba conjunta. Una reposición `REQUIERE_REVISION` no se puede resolver desde el sistema | ADR-018 |
| Mercado Pago simulado | El adaptador no se probó contra la API real; los mails van a Mailpit | ADR-014 |
| Observabilidad | Alertas probadas solo en local, con aviso a Mailpit; una falla puede avisar varias veces. Sin tiempos por paso: el track id muestra qué pasó y en qué orden, no cuánto tardó cada llamada dentro de un pedido. El gateway no escribe en `app_logs` (su log queda en stdout). Bajo una carga mayor a la que se aguanta, o con `mysql-logs` caído, se descartan líneas de la tabla (quedan en stdout y se cuentan en `logs_descartados_total`). `app_logs` crece sin límite: la retención está pendiente | ADR-011, ADR-020 |
| Búsqueda | Sin carga real: el retraso se midió con un administrador. Buscar pendientes recorre la colección | ADR-006 |
| Caché | Medida en una sola máquina y con 19 productos: la latencia con la base sin límite no fue concluyente. Cualquier cambio vacía toda la caché | ADR-007, ADR-019 |
| Capacidad y costos | Medida en una notebook, no en la VM: el objetivo se cumple hasta 800 pedidos/s. Se midió con la observabilidad anterior a ADR-020; con la actual solo se repitió la corrida de 600 pedidos/s. Falta confirmar el tamaño de la VM para cerrar los costos | ADR-013 |
| Núcleo de Inventario | Etiquetas `json` en `dominio`. Los tests de `aplicacion` (68 % de cobertura) usan dobles de los puertos: la atomicidad de la reserva se prueba contra MySQL real, en el adaptador | ADR-002 |
| Resiliencia comprobada | Ensayo en local de 3 caídas de dependencias de Inventario, en [`POSTMORTEM.md`](POSTMORTEM.md). Falta la jornada con el gimnasio real y probar la compra desde la tienda durante una caída | ADR-010 |
