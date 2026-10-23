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

Fuente editable: [`diagrams/contexto.drawio`](diagrams/contexto.drawio). La línea punteada hacia el gimnasio indica que su contrato todavía no está definido.

## 3. Diagrama de contenedores

Muestra las piezas desplegables dentro del sistema y cómo se comunican.

![Diagrama de contenedores: el frontend web llama al API Gateway, que rutea a los servicios Usuarios, Catálogo, Inventario y Ventas, cada uno con su propia base (MySQL o MongoDB). Ventas consulta el precio a Catálogo y reserva stock en Inventario, crea pagos en Mercado Pago y envía mails. Catálogo, Inventario y Ventas intercambian eventos por RabbitMQ. Caché, motor de búsqueda y observabilidad están previstos. El grupo de proveedores entra por el gateway a Catálogo (ver el catálogo) y a Ventas (hacer pedidos). Inventario llama a la API del gimnasio.](diagrams/contenedores.png)

Fuente editable: [`diagrams/contenedores.drawio`](diagrams/contenedores.drawio). Las líneas y los bordes punteados son componentes o relaciones previstos o pendientes de definir. Al pie del diagrama, una tabla relaciona cuatro historias con los contenedores que intervienen.

**Cómo se actualizan:** se edita el `.drawio` (en [draw.io](https://app.diagrams.net) o con la extensión de VS Code) y se vuelve a exportar el `.png` con el mismo nombre. El `.png` es lo que se ve en GitHub; el `.drawio` es la fuente.

## 4. Servicios, responsabilidades y datos

| Servicio | Responsabilidad | Datos que posee |
|---|---|---|
| Catálogo | Productos, variantes, fotos, categorías, precios base, promociones y publicación. Calcula el precio vigente. Expone el catálogo al grupo de proveedores. | Productos con sus variantes, promociones |
| Inventario | Stock por variante y canal, reservas con vencimiento, transferencias, ingresos, ajustes y movimientos. Consulta y repone el stock en consignación del gimnasio. | Stock, reservas, movimientos (incluidas las salidas a consignación), reposiciones pendientes, outbox, mensajes procesados. |
| Ventas | Ventas online y del local, estados, pagos (Mercado Pago y transferencia) y envío de mails. Recibe los pedidos del grupo de proveedores. | Ventas (con el id y el email del comprador, o el grupo externo que compró), ítems, pagos, registro de mails, outbox, mensajes procesados. |
| Usuarios | Registro, inicio de sesión, emisión del token (id, email y rol) y roles. | Usuarios, hash de contraseña, rol |

### Estilo interno de cada servicio (preliminar, D2)

| Servicio | Estilo | Por qué |
|---|---|---|
| Catálogo | **Capas**: Handler → Service → Repository | Pocas reglas: casi todo es alta, edición y lectura de productos por un solo administrador. La única regla con peso es el precio vigente con promociones, y entra en la capa de servicio. La carga de lectura se resuelve con caché e índice, no con la estructura interna. **[propuesta]** Lecturas servidas por el índice de búsqueda (CQRS liviano). |
| Usuarios | **Capas** **[propuesta]** | Registro, inicio de sesión y emisión del token. El email único lo garantiza la base y no hay proveedores externos que puedan cambiar. Puertos y adaptadores agregarían piezas sin una variación que proteger. |
| Ventas | **Hexagonal** **[propuesta]** | Tiene el ciclo de estados de la venta, con transiciones que no pueden saltearse ni repetirse (no cobrar dos veces). Depende de varios sistemas externos que pueden cambiar: Mercado Pago, el proveedor de mail (todavía sin elegir), RabbitMQ y las APIs de Catálogo e Inventario. Recibe pedidos por varias entradas: comprador, administrador, aviso de Mercado Pago y eventos. Los puertos permiten probar las reglas con adaptadores falsos y cambiar un proveedor sin tocar el núcleo. |
| Inventario | **Hexagonal** **[propuesta]** | Tiene las reglas más críticas del sistema: no vender de más, reservar todo o nada, transferir sin dejar stock negativo y las transiciones de la reserva (activa, confirmada, liberada, vencida). Recibe pedidos por varias entradas: Ventas, el panel, el evento `VarianteCreada` y el proceso que vence reservas. Y consume la capacidad del gimnasio (D9), un proveedor cuyo contrato no controlamos. La carrera entre dos reservas la resuelve la base con una actualización condicional: el puerto del repositorio expresa la operación completa ("reservar si alcanza") y el adaptador de MySQL la hace atómica. El núcleo valida el resto de las reglas. |

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
| Stock, reservas y movimientos        | Inventario                                                          | Ventas reserva, libera y descuenta mediante su API. El panel consulta el stock por ids de variante. El índice de búsqueda recibe la disponibilidad online por eventos **[propuesta]**.                                                                                          |
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
| Frontend (panel) → Catálogo e Inventario                 | Vista de stock por producto y filtro _stock a corregir_: el frontend junta variantes y cantidades | Token de administrador                                                              | Si falla Catálogo, error claro en lugar de datos incompletos |
| Ventas → Catálogo                                        | Precio vigente al comprar **[propuesta]**                                                         | Timeout                                                                             | No se puede iniciar la compra; error claro                   |
| Ventas → Inventario                                      | Reservar, liberar, confirmar al pagar y descontar stock                                           | Timeout, reintento acotado solo ante fallas transitorias, clave de idempotencia     | Error claro; no queda una venta ni una reserva a medias      |
| Ventas → Mercado Pago                                    | Crear el pago                                                                                     | Timeout y circuit breaker                                                           | La compra no avanza y la reserva vence sola                  |
| Mercado Pago → Gateway → Ventas                          | Aviso del resultado del pago                                                                      | Procesamiento idempotente                                                           | Mercado Pago reintenta el aviso                              |
| Ventas → proveedor de mail                               | Enviar mails                                                                                      | Envío fuera de la transacción de la venta, con reintentos **[propuesta]**           | Se registra como fallido; la venta no se afecta              |
| Grupo de proveedores → Gateway → Catálogo y Ventas | Ver el catálogo y hacer pedidos (capacidad publicada) | Clave por grupo **[propuesta]**, clave de idempotencia, límite de pedidos por grupo | Errores definidos en el contrato |
| Inventario → Gimnasio | Consultar su stock de FAV y registrar reposiciones (capacidad consumida) | Timeout, reintento acotado, clave de idempotencia y circuit breaker **[propuesta]** | La reposición queda pendiente y se reintenta; el panel muestra que los datos del gimnasio no están disponibles |

Los valores concretos de timeouts y reintentos se definen en el **ADR-005 (D5)**.

### Capacidad publicada: catálogo y pedidos

El grupo de proveedores ve el catálogo de FAV y le hace pedidos para sí mismo, como un comprador mayorista.

| Operación | Servicio | Qué hace |
|---|---|---|
| `GET /v1/productos` | Catálogo | Productos publicados con sus variantes y el precio vigente. No informa stock. |
| `POST /v1/pedidos` | Ventas | Pide el precio a Catálogo, aparta el stock online en Inventario por 48 horas (todo o nada) y registra la venta *Pendiente de pago* por transferencia. Desde ahí sigue como una compra por transferencia (CU-02). |

Contrato: [`catalogo-pedidos-v1.openapi.yaml`](contracts/catalogo-pedidos-v1.openapi.yaml). Uso, errores y ejemplos en [`contracts/README.md`](contracts/README.md). Se justifica en el **ADR-008 (D8)**.

### Capacidad consumida: venta en consignación en un gimnasio

El gimnasio vende ropa de FAV y se queda con una comisión. **Inventario** llama a su API para consultar cuánto stock de FAV le queda y para registrar reposiciones. Al reponer, Inventario descuenta el stock físico y guarda una *reposición pendiente* en la misma transacción (con outbox); un proceso la envía al gimnasio con una clave de idempotencia y la reintenta si falla. Si el gimnasio la rechaza, las unidades vuelven al stock físico.

Inventario lo hace con un adaptador de salida que traduce el modelo del gimnasio al de FAV. **[?]** Contrato del gimnasio, pendiente. Se justifica en el **ADR-009 (D9)**.

### Asíncronas (RabbitMQ, con transactional outbox)

| Evento                                         | Publica               | Consume                              | Efecto                                             |
| ---------------------------------------------- | --------------------- | ------------------------------------ | -------------------------------------------------- |
| `ReservaVencida`                               | Inventario            | Ventas                               | Pasa la venta a _Vencida_.                         |
| `VarianteCreada` **[propuesta]**               | Catálogo              | Inventario                           | Registra la variante con stock 0 en ambos canales. |
| Cambios de producto y de stock **[propuesta]** | Catálogo e Inventario | Actualización del índice de búsqueda | Mantiene la búsqueda sincronizada (D6).            |

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
| Inventario | Navegar y buscar (la disponibilidad mostrada puede estar desactualizada) | Comprar online, registrar ventas del local, ver y mover stock, pedidos de grupos externos, reponer al gimnasio | Timeout y reintento acotado; nunca queda una venta a medias |
| Ventas | Navegar, buscar, gestionar productos y stock | Comprar, registrar ventas, ver pedidos, pedidos de grupos externos | El gateway devuelve un error claro |
| Un pico de pedidos del grupo de proveedores | Todo | — | Límite de pedidos por grupo **[propuesta]**: el exceso recibe `429` |
| El gimnasio | Todo lo de FAV | Ver el stock del gimnasio y enviarle reposiciones | Timeout y circuit breaker; las reposiciones pendientes se envían cuando vuelve |
| Usuarios                                | Navegar, buscar, comprar como invitado y las sesiones ya iniciadas, porque el gateway valida el token sin consultar a Usuarios (ADR-001) | Iniciar sesión y registrarse                                                                            | —                                                                   |
| RabbitMQ                                | Todo lo síncrono                                                                                                                         | Los efectos asíncronos se demoran: paso a _Vencida_, variantes nuevas en Inventario, índice             | Outbox: los eventos se publican cuando vuelve                       |
| Mercado Pago                            | Navegar; comprar por transferencia                                                                                                       | Pagar con Mercado Pago                                                                                  | Timeout y circuit breaker; se ofrece la transferencia               |
| Proveedor de mail                       | Todo                                                                                                                                     | Los mails se demoran                                                                                    | Reintentos y registro de mails fallidos                             |
| Redis (caché)                           | Todo, más lento                                                                                                                          | —                                                                                                       | Las lecturas van directo a la fuente                                |
| Motor de búsqueda                       | Navegar por categoría desde la base de Catálogo                                                                                          | Búsqueda por texto y filtros                                                                            | Error claro; el índice se reconstruye al volver                     |
| La base de un servicio                  | Los demás servicios                                                                                                                      | Ese servicio                                                                                            | Una base por servicio: la falla no se propaga                       |

## 9. Caché y búsqueda (propuestas, sin implementar)

- **Herramientas existentes, no implementaciones propias:** la caché usa Redis y la búsqueda un motor de búsqueda, ambos a través de sus bibliotecas cliente para Go. No se programa una caché ni un índice desde cero.
- **Qué se cachea [propuesta]:** las lecturas del catálogo, como la ficha de producto y los listados por categoría. El stock para reservar y el precio para cobrar **no** se leen de la caché, sino de la fuente.
- **Que la caché no quede mal cargada:** al cambiar un producto, un precio o una promoción, se invalida su entrada. Además, cada entrada vence por tiempo. Se mide la tasa de aciertos para comprobar que se aprovecha, comparando antes y después (**[ADR-007](adr/ADR-007-cache.md), D7**). El ADR condiciona la caché a una medición previa sin ella.
- **Búsqueda:** índice de productos publicados actualizado por eventos, con un retraso máximo definido y la posibilidad de reconstruirlo desde cero (**[ADR-006](adr/ADR-006-busqueda.md), D6**). **[propuesta]** Elasticsearch u OpenSearch; la disponibilidad no se indexa.

## 10. Observabilidad

El estado de una operación tiene que poder entenderse sin revisar cada componente a mano (**[ADR-011](adr/ADR-011-observabilidad.md), D11**).

**Implementado en Inventario y el gateway (Entrega 2):** logs JSON con `servicio`, `instancia`, `correlation_id`, `trace_id` y `span_id`; log de acceso del gateway con la réplica que atendió; trazas OpenTelemetry a Jaeger (`:16686`) con una traza del flujo mock de Ventas → Inventario → MySQL; métricas Prometheus (`:9090`) con una serie por réplica. **Falta:** tablero (Grafana), alertas cargadas, persistencia del historial y el resto de los servicios. El objetivo completo es:


- **Logs estructurados** en todos los servicios y en el gateway, con un identificador de correlación que viaja en cada llamada y en cada evento. Permite seguir una compra de punta a punta. El token nunca se escribe en los logs.
- **Métricas** por servicio: pedidos por endpoint y código de estado (incluidos 4xx y 5xx), latencia, fallas de cada dependencia, mensajes en la DLQ, aciertos de caché y estado de cada réplica.
- **Historial:** las métricas se conservan el tiempo suficiente para responder preguntas sobre el pasado, por ejemplo cuántos errores 404 hubo la semana pasada. **[propuesta]** al menos dos semanas.
- **Trazas distribuidas** del flujo de compra.
- **Tablero** con el estado general del sistema y al menos una alerta.
- **[?]** herramientas: se definen cuando se dé el teórico de observabilidad.

## 11. Pruebas de carga (previstas, guion sin ejecutar)

Se simula un **pico de demanda** como los que describe el cliente (cambio de temporada, diciembre): muchos compradores navegando y buscando a la vez, y compras concurrentes sobre las mismas variantes. Se mide latencia, errores, reparto entre las réplicas de Catálogo y el comportamiento de las reservas. El volumen real es bajo (10 a 20 prendas por semana), así que el escenario de pico es una hipótesis que se documenta como tal (**[ADR-013](adr/ADR-013-capacidad-y-costos.md), D13**). El guion de k6 está en `loadtest/` y todavía no se ejecutó.

## 12. Distribución de los componentes

- **Local:** todo se levanta con un solo comando mediante Docker Compose, en cualquier computadora con Docker y sin configuración manual: frontend, gateway, los cuatro servicios (Catálogo con dos réplicas), MongoDB, tres MySQL y RabbitMQ. Redis, el motor de búsqueda y las herramientas de observabilidad se suman cuando se incorporen. Los secretos (incluida la clave privada del token) van en un archivo `.env` que no se sube al repositorio.
- **Balanceo:** en la Entrega 2 el servicio replicado es **Inventario** (2 réplicas con `deploy.replicas`), detrás de un gateway nginx con round robin y verificación pasiva ([ADR-012](adr/ADR-012-balanceo.md), D12). Catálogo con dos réplicas sigue **[propuesta]**.
- **Estado actual del Compose:** gateway, Inventario ×2, MySQL de Inventario, mocks de Catálogo/Ventas y del gimnasio, Jaeger y Prometheus. El perfil `completo` suma MongoDB, MySQL de Ventas y Usuarios y RabbitMQ.
- **Nube:** para que el grupo de proveedores use la capacidad con una URL pública se despliegan el gateway, Catálogo, Ventas e Inventario con sus bases. **[?]** proveedor de nube.

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
│   ├── POSTMORTEM.md           # se completa tras la caída controlada
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
└── mocks/                      # contrato publicado, Catálogo/Ventas y gimnasio
    ├── catalogo-pedidos-v1/
    ├── catalogo-ventas/
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
| [ADR-002](adr/ADR-002-arquitectura-interna.md)     | D2 · Arquitectura interna         | Aceptada · Entrega 2 (implementada en Inventario)    |
| [ADR-004](adr/ADR-004-consistencia-y-concurrencia.md) | D4 · Consistencia y concurrencia | Aceptada · Entrega 2 (7 pruebas de integración)      |
| [ADR-006](adr/ADR-006-busqueda.md)                 | D6 · Búsqueda                     | **Propuesta** · Entrega 2 (sin implementar)          |
| [ADR-007](adr/ADR-007-cache.md)                    | D7 · Caché                        | **Propuesta** · Entrega 2 (sin implementar)          |
| [ADR-009](adr/ADR-009-consumo-del-gimnasio.md)     | D9 · Consumo del gimnasio         | Aceptada con contrato supuesto · Entrega 2           |
| [ADR-010](adr/ADR-010-resiliencia.md)              | D10 · Resiliencia                 | Aceptada · Entrega 2                                 |
| [ADR-011](adr/ADR-011-observabilidad.md)           | D11 · Observabilidad              | Aceptada (primera versión) · Entrega 2               |
| [ADR-012](adr/ADR-012-balanceo.md)        | D12 · Balanceo de carga           | Aceptada · Entrega 2                                 |
| [ADR-013](adr/ADR-013-capacidad-y-costos.md)       | D13 · Capacidad y costos          | **Propuesta** · Entrega 2 (sin mediciones)           |

## 15. Pendientes de arquitectura

| Tema                                                                                          | Dónde se resuelve                         |
| --------------------------------------------------------------------------------------------- | ----------------------------------------- |
| Justificación de los estilos internos de cada servicio                                        | D2 (ADR-002)                              |
| Motor de búsqueda y retraso máximo del índice                                                 | D6                                        |
| Qué se cachea exactamente y tiempo de vencimiento                                             | D7                                        |
| Tecnología del API Gateway: **resuelto, nginx** (ADR-012). Frontend: pendiente                | D2 / D12                                  |
| Herramientas de observabilidad y retención de métricas                                        | D11                                       |
| Contrato del gimnasio y cómo identifica los productos de FAV | D9 |
| Cómo se entrega y se rota la clave de cada grupo consumidor, y el límite de pedidos por grupo | D8                                        |
| Dónde se almacenan las fotos de los productos                                                 | D3                                        |
| Firma y vencimiento del token, y dónde lo guarda el frontend                                  | Implementación del gateway y del frontend |
| Proveedor de mail y de nube                                                                   | D5 / D13                                  |
| Cómo recibe el entorno local los avisos de Mercado Pago                                       | D5                                        |

## 16. Limitaciones conocidas y deuda técnica (Entrega 2)

Lo que sabemos que no está o no funciona del todo. Se declara para la presentación.

| Tema | Situación | Dónde |
| ---- | --------- | ----- |
| Servicios implementados | Solo **Inventario** es real. Catálogo y Ventas son un mock; Usuarios y el frontend no están | — |
| Gateway único | Punto único de falla; la validación del JWT no está (se usa `X-Admin-Key`) | ADR-012 |
| Contrato del gimnasio | Es **supuesto**; un `4xx` se trata siempre como rechazo definitivo | ADR-009 |
| Publicador a RabbitMQ | El outbox se escribe, pero nadie lo publica; sin consumo idempotente ni DLQ | ADR-005 |
| Tiempos entre capas | Reintentos del mock de Ventas y del gateway se suman y pueden pasar los 5 s | ADR-010 |
| Observabilidad | Sin tablero ni alertas; Jaeger y Prometheus pierden datos al reiniciar; el gateway no crea spans | ADR-011 |
| Búsqueda y caché | Solo diseño | ADR-006, ADR-007 |
| Capacidad y costos | Guion de carga sin ejecutar; costos sin completar | ADR-013 |
| Núcleo de Inventario | Sin tests unitarios de `aplicacion`; etiquetas `json` en `dominio` | ADR-002 |
| Resiliencia comprobada | Falta `docs/POSTMORTEM.md` con una falla controlada ensayada | ADR-010 |
