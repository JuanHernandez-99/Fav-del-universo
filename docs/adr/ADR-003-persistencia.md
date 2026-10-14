# ADR-003 · Persistencia

|                             |                                                                                                                                                                                                                     |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Estado**                  | Aceptada (versión inicial) · Entrega 1. Se valida en la Entrega 2.                                                                                                                                                  |
| **Fecha**                   | 09/10/2026                                                                                                                                                                                                          |
| **Decisión de la consigna** | D3 · Persistencia                                                                                                                                                                                                   |
| **Relacionadas**            | [ADR-001](ADR-001-limites-de-los-servicios.md) (límites y dueños de los datos), [ADR-005](ADR-005-comunicacion.md) (outbox e idempotencia), [ADR-008](ADR-008-contrato-propio.md) (datos de la capacidad publicada) |

> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar.

> **Actualización 14/10/2026:** la capacidad publicada quedó definida en el [ADR-008](ADR-008-contrato-propio.md): **catálogo y pedidos**, servida por Catálogo y Ventas. Además, Inventario consume la capacidad de un gimnasio (ADR-009, en preparación). Las menciones a las opciones A y B son de la Entrega 1 y se revisan al validar este ADR en la Entrega 2.

## Contexto

ADR-001 define cuatro servicios, cada uno dueño de sus datos. La consigna pide (requisito 2.8):

- que cada servicio elija el almacenamiento que mejor responda a sus patrones de acceso;
- al menos dos tipos de almacenamiento, uno relacional y otro no relacional;
- al menos una operación que no pueda duplicarse ni perder información.

Además, la cátedra indicó que tres microservicios implican tres bases: cada servicio tiene la suya y ningún servicio accede a la base de otro (RNF-08).

Las operaciones que no pueden duplicarse ni perder información son la **reserva de stock** y la **transferencia entre stocks**, las dos en Inventario (ARCHITECTURE §7). Ventas también necesita garantías: una venta no se confirma ni se cobra dos veces (HU-11.6, HU-15.3).

## Criterios

1. **Garantías que necesita cada operación:** transacciones y actualizaciones condicionales donde hay invariantes (stock, estados de venta, email único).
2. **Forma de los datos y de las consultas:** cómo se escriben y cómo se leen.
3. **Cumplir la consigna:** un tipo relacional y uno no relacional, una base por servicio.
4. **Simplicidad operativa:** pocos motores distintos para un equipo de dos personas.

## Decisión

Cada servicio tiene **su propia instancia** de base de datos, en su propio contenedor.

| Servicio       | Almacenamiento                          | Qué guarda                                                                                                                                                                                                                              | Patrones de acceso                                                                                                                                      | Por qué                                                                                                                                                                                          |
| -------------- | --------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Catálogo**   | **MongoDB** (documental, no relacional) | Un documento por producto, con sus variantes, sus fotos (referencias) y su categoría. Promociones en una colección aparte.                                                                                                              | Muchas lecturas del producto completo (ficha, listados). Pocas escrituras, hechas por un solo administrador. Atributos que pueden variar por categoría. | El producto se lee y se edita como una unidad: un documento lo devuelve completo sin joins. Las escrituras tocan un solo producto, así que alcanzan las operaciones atómicas sobre un documento. |
| **Inventario** | **MySQL** (relacional)                  | Stock por variante y canal, reservas y sus ítems, movimientos, transferencias, claves de idempotencia, outbox y mensajes procesados. Con la opción A de la capacidad, los cupos y reservas de cada grupo consumidor, en tablas propias. | Escrituras concurrentes sobre las mismas filas (las últimas unidades). Una reserva toca varias variantes a la vez. Consultas por id de variante.        | Necesita transacciones que abarquen varias filas (todo o nada), actualizaciones condicionales (`stock − reservado ≥ cantidad`), bloqueo de filas y restricciones únicas para la idempotencia.    |
| **Ventas**     | **MySQL** (relacional)                  | Ventas, ítems (con copia de nombre, talle, color y precio **[propuesta]**), pagos, registro de mails, claves de idempotencia, outbox y mensajes procesados. Con la opción B, los emails de cada grupo consumidor, en una tabla propia.  | Transiciones de estado que no pueden aplicarse dos veces. Consultas por estado, por comprador y por fecha (HU-14, HU-16).                               | Relaciones claras (venta, ítems, pagos), transiciones condicionales (`UPDATE … WHERE estado = …`) y restricciones únicas para no duplicar pagos ni avisos.                                       |
| **Usuarios**   | **MySQL** (relacional)                  | Usuarios, hash de contraseña y rol.                                                                                                                                                                                                     | Búsqueda por email al iniciar sesión. Pocas escrituras.                                                                                                 | El email único se garantiza con una restricción de la base (HU-18.1).                                                                                                                            |

**Almacenamiento derivado** (no es fuente de verdad; se puede perder y reconstruir):

| Componente        | Qué guarda                                                  | Se decide en |
| ----------------- | ----------------------------------------------------------- | ------------ |
| Redis (caché)     | Lecturas del catálogo                                       | ADR-007 (D7) |
| Motor de búsqueda | Índice de productos publicados con su disponibilidad online | ADR-006 (D6) |

**Cómo se cumplen las garantías:**

- **No vender de más:** la reserva descuenta con una actualización condicional dentro de una transacción. Si alguna variante no alcanza, se deshace todo (detalle en ADR-004).
- **No duplicar:** cada operación que se puede reintentar guarda su clave de idempotencia con una restricción única. Un reintento encuentra la clave y devuelve el resultado anterior.
- **No perder eventos:** el evento se guarda en la tabla outbox **en la misma transacción** que el cambio de datos, y se publica después (ADR-005).
- **No procesar dos veces un evento:** el consumidor registra el id del mensaje en una tabla de procesados, en la misma transacción que su efecto (ADR-005).

**MySQL para los tres servicios relacionales:** usar un solo motor relacional reduce lo que hay que aprender, configurar y observar. Además el equipo tiene experiencia previa en MySQL.

## Alternativas consideradas

| Alternativa                                                           | Por qué se descartó                                                                                                                                                                                                                   |
| --------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Todo en MySQL**                                                     | No cumple el requisito de dos tipos de almacenamiento. Además, el producto con variantes y atributos variables se modela mejor como documento.                                                                                        |
| **Inventario en MongoDB**                                             | Una reserva toca varias variantes a la vez. MongoDB lo permite con transacciones multi-documento, pero la actualización condicional con transacción y bloqueo de filas en SQL es más directa y más fácil de probar ante concurrencia. |
| **Ventas en MongoDB**                                                 | La venta tiene relaciones (ítems, pagos) y se consulta por estado y fecha. Las transiciones condicionales y las restricciones únicas son más simples en SQL.                                                                          |
| **Una base compartida, con esquemas separados**                       | La prohíbe la cátedra (RNF-08). Además, la caída o la carga de la base afectaría a todos los servicios.                                                                                                                               |
| **Una sola instancia de MySQL con tres bases lógicas**                | Ahorra recursos, pero la caída de la instancia tumba a tres servicios a la vez y rompe el aislamiento de fallas previsto (ARCHITECTURE §8).                                                                                           |
| **PostgreSQL en lugar de MySQL**                                      | Equivalente en garantías para este uso. Ver **[?]** en la decisión.                                                                                                                                                                   |
| **Redis o el motor de búsqueda como el almacenamiento no relacional** | Son copias derivadas, no la fuente de verdad de ningún servicio. **[?]** Consultado a la cátedra.                                                                                                                                     |

## Consecuencias

**Positivas**

- Las operaciones críticas se resuelven con transacciones locales en una sola base, sin coordinación distribuida.
- La caída de una base afecta solo a su servicio.
- El catálogo se lee como documento completo, que también es lo que se guarda en la caché y se indexa en el buscador.

**Negativas aceptadas**

- **Sin joins entre servicios.** Las vistas que cruzan datos (stock con nombres de variante) se componen en el frontend (ADR-001, decisión 3).
- **Consistencia eventual entre Catálogo e Inventario:** una variante nueva aparece en Inventario cuando llega `VarianteCreada`.
- **Copia de datos en Ventas:** cada venta guarda nombre, talle, color y precio del momento de la compra **[propuesta]**. Es intencional, para que la venta conserve su precio (HU-05.1).
- **Dos motores que operar** (MongoDB y MySQL) y cuatro contenedores de base para un volumen bajo.
- **MongoDB no impone un esquema:** hay que validar la forma de los documentos en el servicio **[propuesta]**, o con la validación de esquemas de MongoDB.
- **Datos de otros grupos** en Inventario (opción A) o en Ventas (opción B), en tablas propias separadas por grupo consumidor (ADR-008).

## Pendientes

- **Dónde se guardan las fotos de los productos.** **[propuesta]** Almacenamiento de objetos compatible con S3, con una instancia local en Docker; MongoDB guarda solo la referencia. **[?]** Herramienta a verificar.
- **Cuánto tiempo se conservan los emails de otros grupos** (opción B, ADR-008).
- **Respaldo de las bases** en el entorno desplegado. **[?]** Depende del proveedor de nube.
- **Índices concretos** de cada base: se definen al implementar, según las consultas reales.

## Validación prevista (Entrega 2)

- Cada servicio arranca con su propia base en Docker Compose y ningún servicio tiene credenciales de otra base (RNF-08).
- Test de integración por servicio contra su base real (requisito 2.11).
- Test de concurrencia: dos reservas simultáneas sobre la última unidad; solo una se confirma y el stock nunca queda negativo.
