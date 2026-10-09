# ADR-001 · Límites de los servicios

| | |
|---|---|
| **Estado** | Aceptada · Entrega 1 |
| **Fecha** | 09/10/2026 |
| **Decisión de la consigna** | D1 · Límites de los servicios |
| **Relacionadas** | [ADR-003](ADR-003-persistencia.md) (persistencia), [ADR-005](ADR-005-comunicacion.md) (comunicación), [ADR-008](ADR-008-contrato-propio.md) (contrato propio) |

## Contexto

FAV vende ropa en un local y empieza a vender online. El sistema tiene que unificar el stock de los dos canales, registrar todas las ventas y permitir compras online con reserva temporal de stock (SPEC §2). La consigna pide al menos tres microservicios y un API Gateway, con una base de datos por servicio.

Lo que pesa al decidir la división:

- **Cargas distintas.** El catálogo se lee mucho y cambia poco. En un pico (cambio de temporada, diciembre) es lo que más tráfico recibe. Las reservas y transferencias de stock son pocas, pero no pueden duplicarse ni perder unidades (requisito 2.8).
- **Garantías distintas.** Que un precio o una descripción tarde unos segundos en verse es aceptable. Que dos compradores se lleven la última unidad, no.
- **Volumen bajo.** Entre 10 y 20 prendas por semana y un solo administrador. Cada servicio extra suma despliegue, base, observabilidad y casos de falla, así que no se separa nada sin un motivo.
- **Capacidad publicada.** Otro grupo, con otro dominio, va a consumir una capacidad de FAV. Se proponen dos opciones, A (reserva de cupos) y B (envío de emails), y se elige una según el consumidor (SPEC §11). En cualquiera de las dos, conviene que dependa de un único servicio y que no sume servicios.

## Criterios de separación

1. **Una responsabilidad de negocio con reglas propias.** Se separa cuando hay reglas, estados o invariantes que pertenecen a un solo concepto.
2. **Garantías y carga.** Lo que necesita consistencia fuerte no comparte servicio con lo que se lee masivamente y tolera atraso.
3. **Un solo dueño por dato.** Cada dato tiene un único servicio que lo escribe. Los demás lo leen por API o por eventos, o guardan una copia explícita y justificada.
4. **Pocas dependencias en el camino crítico.** Una operación, sobre todo la compra, no debería necesitar más servicios que los indispensables.
5. **No separar sin motivo.** Lo que no tiene reglas propias ni una carga distinta queda como adaptador dentro de otro servicio.

## Decisión

Cuatro microservicios, cada uno con su base de datos, detrás de un API Gateway, con un frontend web como único punto de interacción.

| Componente | Responsabilidad | Dueño de | Invariantes que protege |
|---|---|---|---|
| **Catálogo** | Qué se vende y cómo se muestra: productos, variantes, fotos, categorías, precio base, promociones y publicación. Calcula el precio vigente. | Productos, variantes, fotos, promociones | Un solo sistema de talles por producto (RN-02). Sin variantes repetidas. Promociones con vigencia (RN-05). |
| **Inventario** | Cuántas unidades hay y dónde: stock online y físico por variante, reservas con vencimiento, ingresos, ajustes, transferencias y movimientos. Expone la capacidad publicada con la opción A. | Stock, reservas, movimientos. Con la opción A, los cupos de cada grupo consumidor. | El stock online nunca queda negativo. No se reserva más de lo disponible ni dos veces la misma compra. Las transferencias son atómicas (RN-07, RN-10). |
| **Ventas** | Ventas online y del local, sus estados, los pagos con Mercado Pago y transferencia, y los mails. Expone la capacidad publicada con la opción B. | Ventas, ítems, pagos, registro de mails. Con la opción B, los emails de cada grupo consumidor, en una tabla propia. | Transiciones de estado válidas (RN-19). Un pago no se confirma dos veces. El precio lo calcula el servidor (RN-22). |
| **Usuarios** | Registro, inicio de sesión, roles y emisión del token. | Usuarios, hash de contraseña, rol | Email único. La contraseña nunca se expone (HU-18). |
| **API Gateway** | Punto de entrada único: valida el token, rutea y controla el tráfico. No tiene lógica de negocio ni compone respuestas. | — | — |
| **Frontend** | Tienda y panel de administración. Guarda el carrito y compone las vistas del panel que cruzan datos de dos servicios. | Carrito (en el navegador) | — |

### Relaciones entre servicios

| Relación | Tipo | Para qué |
|---|---|---|
| Ventas → Catálogo | Síncrona | Precio vigente y datos de la variante, al comprar o al registrar una venta del local. |
| Ventas → Inventario | Síncrona | Reservar, liberar, confirmar (descuento definitivo al pagar) y descontar stock. La confirmación es síncrona para que Inventario decida la carrera entre pago y vencimiento (ADR-005). |
| Inventario → Ventas | Evento `ReservaVencida` | Pasar la venta a *Vencida*. |
| Catálogo → Inventario | Evento `VarianteCreada` | Registrar la variante con stock 0 en ambos canales. |
| Usuarios | Ninguna | El gateway valida el token con la clave pública de Usuarios, sin llamarlo. |

Usuarios solo interviene en el inicio de sesión y el registro. Catálogo e Inventario no se llaman entre sí. Los detalles de cada interacción (timeouts, reintentos, idempotencia) están en ADR-005.

### Decisiones puntuales

**1. Ventas obtiene la identidad del comprador del token.**

Usuarios emite un JWT al iniciar sesión, con estos claims:

| Claim | Uso |
|---|---|
| `sub` | Id del usuario. "Mis compras" filtra por este valor (HU-14). |
| `email` | Ventas lo guarda en la venta para enviar los mails (HU-20). |
| `role` | `comprador` o `administrador`. El gateway controla el acceso a las rutas de administración (HU-19). |
| `iat`, `exp` | Emisión y vencimiento. El gateway rechaza tokens vencidos. |

El gateway valida la firma y el vencimiento sin llamar a Usuarios. Descarta cualquier encabezado de identidad que venga del cliente y reenvía a los servicios `X-User-Id`, `X-User-Email` y `X-User-Role`. Los servicios confían en esos encabezados solo dentro de la red interna.

Al crear la venta, Ventas guarda el `userId` y el email. Los mails posteriores (pago aprobado, despacho) salen cuando el comprador ya no tiene un token vigente, así que se envían con el email guardado, sin consultar a Usuarios. Para un invitado, el email viene en la compra (RN-11).

- **[propuesta]** Firma asimétrica RS256: solo Usuarios tiene la clave privada y puede emitir tokens; el gateway tiene la pública. Vencimiento de 1 hora, sin refresh token en v1.
- **HU-18.4 se descarta.** Las compras hechas como invitado no se asocian a una cuenta creada después con el mismo email. Sin verificación de email, cualquiera podría registrarse con un email ajeno y ver esos pedidos.

**2. El carrito vive en el frontend.**

Agregar al carrito no reserva stock (HU-10.1), así que el carrito no tiene reglas ni garantías: es una lista de variantes y cantidades. Se guarda en el navegador y se envía completo a Ventas al apretar "Comprar". Ventas vuelve a validar todo: que el producto esté publicado, el precio (consultándolo a Catálogo) y el stock (al reservar).

**3. La vista de stock del panel se compone en el frontend. Inventario no copia datos de Catálogo.**

El panel muestra el stock de cada variante dentro de la ficha del producto: el frontend pide las variantes a Catálogo y las cantidades a Inventario por ids, y las junta. El filtro *stock a corregir* (RN-25) funciona igual: Inventario devuelve los ids de las variantes con stock físico negativo y el frontend obtiene producto, talle y color de Catálogo en una sola consulta por lote.

Se descarta el filtro de poco stock con umbral por categoría (versión anterior de HU-09). Obligaba a Inventario a conocer la categoría, el nombre, el talle y el color de cada variante, con un evento de actualización para mantenerlos. Con el volumen actual, el cliente controla el stock desde la vista por producto.

**4. Los pagos son un adaptador dentro de Ventas.**

No hay reglas de pago propias: el estado del pago es parte del estado de la venta. Separarlos obligaría a coordinar dos servicios en la operación con más fallas parciales.

**5. Los mails son un adaptador dentro de Ventas.**

Los mails de FAV se originan en cambios de estado de una venta (HU-20, HU-21). Se envían fuera de la transacción de la venta (ADR-005). Como se envían a través de un puerto propio, el adaptador se puede extraer a un servicio si aparecen otros orígenes.

Con la opción B de la capacidad publicada aparece un segundo origen: los emails de otros grupos. Entran por un adaptador de entrada aparte y usan el mismo puerto de mail. No se separa un servicio de notificaciones por eso (ver Alternativas).

**6. La capacidad publicada vive en un servicio existente.**

- **Opción A (reserva de cupos):** en Inventario, que ya resuelve reservas con vencimiento sin sobreventa.
- **Opción B (envío de emails):** en Ventas, que ya envía mails con reintentos.

En las dos, el consumidor externo depende de un solo servicio y la capacidad es una entrada aparte: sus datos se guardan separados de los de FAV, por grupo consumidor, y al terminar la materia se puede quitar sin tocar la lógica de negocio. El diseño de los contratos y la elección están en ADR-008.

## Alternativas consideradas

| Alternativa | Por qué se descartó |
|---|---|
| **Monolito modular** | Resolvería el negocio con menos costo operativo, y para FAV fuera del TP sería razonable. La consigna exige microservicios. Los módulos de ese monolito serían los mismos servicios de este ADR. |
| **Catálogo e Inventario juntos** | Mezcla la carga de lectura más alta con las operaciones que necesitan consistencia fuerte: escalar o cachear el catálogo implicaría escalar las reservas. Con la opción A, la capacidad publicada quedaría acoplada al catálogo. |
| **Servicio de Pagos separado** | No tiene reglas propias. Obliga a coordinar dos servicios para confirmar una venta y suma un caso de falla parcial (pago registrado y venta sin actualizar). |
| **Servicio de Notificaciones separado** | Sin la opción B hay un único origen de mails. Con la opción B habría dos (FAV y otros grupos), pero un servicio nuevo suma despliegue, base y casos de falla para una capacidad que existe por la materia. Se acepta compartir Ventas, con tabla propia y límite de pedidos por grupo. El puerto de mail deja la puerta abierta para separarlo después. |
| **Servicio propio para la capacidad publicada** | Mismo costo que el anterior. A y B reutilizan mecanismos que FAV ya necesita (reservas en Inventario, mails en Ventas). |
| **Publicar la reserva de stock de FAV** | Era la primera propuesta. El grupo consumidor tiene otro dominio y no tiene prendas que reservar: la integración sería forzada. |
| **Servicio de Búsqueda separado** | El índice es un modelo de lectura derivado de Catálogo e Inventario, no el dueño de ningún dato. Dónde se mantiene se decide en ADR-006. |
| **Ventas online y del local en servicios distintos** | Comparten la entidad, el registro (RN-12) y las promociones por medio de pago. La diferencia es de canal y de flujo, no de dominio. |
| **Servicio de Carrito** | En v1 el carrito no reserva, así que no tiene reglas ni garantías propias. |
| **Precios y promociones en un servicio propio** | El precio vigente depende del producto y sus promociones, que se editan juntos en el panel. Separarlo agregaría una llamada más en el camino de la compra. |
| **Email: llamada síncrona de Ventas a Usuarios** | Agrega una dependencia a la compra y rompe la degradación prevista: con Usuarios caído, las sesiones ya iniciadas deben poder comprar. |
| **Email: copia en Ventas por un evento `UsuarioRegistrado`** | Evento, consumidor y tabla para resolver un solo campo que ya viaja en el token. |
| **Email: enviado por el frontend en el cuerpo de la compra** | Es un dato que controla el cliente: la venta quedaría asociada a un email no confiable. |
| **Copia de datos de variante en Inventario por eventos** | Solo era necesaria para el filtro de poco stock por categoría, que se descartó. Agregaba un evento de actualización y datos con consistencia eventual. |
| **Composición de vistas en el gateway** | El gateway se limita a autenticar, rutear y controlar el tráfico. Poner lógica de presentación lo acopla a cada pantalla. Si la composición crece, se evalúa una capa de agregación (bonus de la consigna). |

## Consecuencias

**Positivas**

- Las dos operaciones críticas (reserva y transferencia) se resuelven dentro de una sola base, con transacciones locales y sin coordinación distribuida (ADR-004).
- Catálogo se puede replicar y cachear sin tocar el stock (ADR-007, ADR-012).
- Comprar depende de Ventas, Catálogo e Inventario, pero no de Usuarios. Con Usuarios caído, los invitados y las sesiones ya iniciadas siguen comprando.
- La capacidad publicada depende de un solo servicio.

**Negativas aceptadas**

- **La compra pasa por tres servicios.** Ventas depende de forma síncrona de Catálogo e Inventario: si cualquiera de los dos cae, no se puede comprar online. Se acota con timeouts y errores claros (ADR-005, ADR-010).
- **Fallas parciales entre Ventas e Inventario.** La venta y su reserva están en bases distintas y puede quedar una sin la otra. Se trata con idempotencia, outbox y vencimiento de las reservas (ADR-004, ADR-005).
- **El email del token puede quedar desactualizado** hasta que vence. En v1 no se puede cambiar el email.
- **El token no se puede revocar** antes de su vencimiento. Con una sola cuenta de administrador, se acepta.
- **El contenido del token es legible** por quien lo tenga. Lleva solo id, email y rol, y no se escribe en los logs.
- **Las compras como invitado no aparecen al registrarse** (HU-18.4 descartada).
- **El carrito no se comparte entre dispositivos** y se pierde si se borran los datos del navegador.
- **La vista de stock del panel depende de dos servicios.** Si Catálogo cae, el panel no puede mostrar los nombres de las variantes.
- **No hay aviso de poco stock.** El cliente lo controla desde el panel.
- **Costo operativo** de cuatro servicios y cuatro bases para un volumen bajo, aceptado por la consigna.
- **La capacidad publicada comparte servicio con FAV.** Si cae Inventario (A) o Ventas (B), cae también la capacidad, y un pico del consumidor podría afectar a ese servicio. Se acota con un límite de pedidos por grupo.
- **Inventario (A) o Ventas (B) guarda datos de otros grupos,** separados de los de FAV por la clave de cada grupo.

## Pendientes

- **Qué capacidad se publica (A o B).** Se elige según el grupo consumidor que asigne la cátedra. Cambia qué servicio la expone, no los límites (ADR-008).
- **Firma y vencimiento del token, y dónde lo guarda el frontend:** se confirman al implementar el gateway y el frontend.

## Validación prevista (Entrega 2)

- Ningún servicio accede a la base de otro (RNF-08): se revisa en Docker Compose y en el código.
- Una compra se completa con Usuarios detenido, usando una sesión ya iniciada.
- Las trazas del flujo de compra muestran solo las dependencias de la tabla de relaciones.
