# ADR-006 · Búsqueda

|                             |                                                                                                                                                              |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Estado**                  | **Propuesta** · Entrega 2. **No hay nada implementado.** Se confirma o ajusta antes de construir el índice.                                                   |
| **Fecha**                   | 23/10/2026                                                                                                                                                   |
| **Decisión de la consigna** | D6 · Motor de búsqueda                                                                                                                                       |
| **Relacionadas**            | [ADR-001](ADR-001-limites-de-los-servicios.md) (límites), [ADR-003](ADR-003-persistencia.md) (Mongo), [ADR-005](ADR-005-comunicacion.md) (eventos), [ADR-007](ADR-007-cache.md) (caché) |

> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar · _previsto_: forma parte del diseño pero todavía no está implementado.

## Contexto

La consigna pide justificar si el sistema necesita un motor de búsqueda, qué se indexa, cómo se mantiene actualizado y qué pasa si el índice está desactualizado o cae. El apunte de motores de búsqueda plantea que el índice es un **modelo de lectura derivado**: ayuda a descubrir, pero **el servicio dueño del dato valida**.

El catálogo permite buscar por texto ("remera blanca"), filtrar por categoría, talle y color, y ordenar. Hoy la lectura sale de Mongo por categoría o por id. Una búsqueda de texto libre con tolerancia a errores de tipeo y orden por relevancia no se resuelve bien con las consultas por campo.

## Criterios

1. **Relevancia:** ordenar por qué tan bien coincide la consulta, no solo filtrar.
2. **Tolerancia** a errores de escritura y a acentos.
3. **Que el catálogo siga funcionando si el buscador cae.**
4. **Frescura acotada y medible** entre editar un producto y encontrarlo.
5. **Costo operativo bajo** para un equipo de dos personas.

## Decisión **[propuesta]**

### Motor

**Elasticsearch 8** (o su equivalente OpenSearch, si la licencia es un problema) en un contenedor de un solo nodo para el desarrollo. Se elige por ser el estándar para búsqueda por texto con relevancia (BM25), filtros y facetas, y por ser el que cubre el material de la cátedra.

### Qué se indexa

**Un documento por producto publicado**, con sus variantes aplanadas dentro:

| Campo                        | Uso                                                 |
| ---------------------------- | --------------------------------------------------- |
| `nombre`, `descripcion`      | Texto buscable (con analizador en español)          |
| `categoria`, `talles[]`, `colores[]` | Filtros (no puntúan)                        |
| `precioDesde`                | Orden y filtro por rango                            |
| `publicado`                  | Solo se indexan los publicados                      |
| `version`                    | Versión del producto en Catálogo, para indexar sin duplicar |

**No se indexa la disponibilidad.** Cambia con cada reserva y se desactualizaría al instante. La disponibilidad se consulta a Inventario al mostrar el resultado (ver Consecuencias).

### Cómo se mantiene

1. Catálogo publica un evento al crear, editar o despublicar un producto (ADR-005, con outbox).
2. Un **indexador** consume los eventos y escribe en el índice con **versionado externo**: solo aplica si la versión del evento es mayor a la guardada. Un evento repetido o fuera de orden no pisa un dato más nuevo (idempotente).
3. Despublicar quita el documento.

### Consultas

- **Texto + filtros:** la consulta de texto puntúa; los filtros de categoría, talle, color y precio recortan sin afectar la relevancia.
- **Paginación** con `search_after`, no con `from`/`size` profundo.
- **Autocompletado**: _previsto_, no necesario para la primera versión.

### Frescura **[propuesta]**

Objetivo inicial: que un cambio sea encontrable en **unos 10 segundos** (valor a medir con el indexador real, no una garantía hoy). Se mide la diferencia entre el momento de la edición y el momento en que aparece en la búsqueda.

### Si el índice falla o está atrasado

| Situación                          | Comportamiento                                                                                                          |
| ---------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| El buscador cae                    | La búsqueda por texto responde `503`; **la navegación por categoría sigue** porque lee de Mongo.                         |
| El índice está atrasado            | Se muestra un dato levemente viejo. El detalle del producto sale siempre de Catálogo y el pedido se valida contra Catálogo e Inventario. |
| El índice pierde datos             | **Reindexado completo:** se crea un índice nuevo, se carga desde Mongo y se cambia un alias; sin cortar la búsqueda.     |
| Un producto despublicado aparece   | Al abrirlo, Catálogo responde `404`; nunca se puede comprar por haberlo encontrado en el índice.                         |

## Alternativas consideradas

| Alternativa                                         | Por qué se descartó                                                                                                                                         |
| --------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Índice de texto de Mongo**                        | Cero piezas nuevas, pero la relevancia, la tolerancia a errores y las facetas son limitadas. **Es el plan B** si el motor resulta excesivo.                  |
| **`LIKE` en MySQL**                                 | No ordena por relevancia, no tolera errores y no usa índices con comodín al principio. Además, el catálogo no está en MySQL.                                 |
| **Meilisearch o Typesense**                         | Más simples de operar y buenos para este tamaño. Una opción válida; se prefiere Elasticsearch por el material de la cátedra. **[?]** Reevaluar si la operación pesa. |
| **Búsqueda vectorial o híbrida**                    | Sin una necesidad observable. Aumenta el costo y la complejidad.                                                                                            |
| **Indexar la disponibilidad**                       | Se desactualiza con cada reserva y obliga a un flujo de eventos de muy alto volumen.                                                                        |
| **Consultar a Mongo y a Inventario en cada búsqueda** | Una consulta a Inventario por resultado multiplica la carga y acopla la búsqueda a su disponibilidad.                                                    |

## Consecuencias

**Positivas**

- Búsqueda con relevancia y tolerancia a errores sin sobrecargar a Mongo.
- Catálogo sigue operando si el buscador cae.
- El índice se puede reconstruir siempre desde Mongo: no es una fuente de verdad.

**Negativas aceptadas**

- **Una pieza más que operar** (memoria, versiones, mapeo).
- **Frescura eventual:** hay una ventana en la que el índice no refleja la edición.
- **Hay que escribir el indexador** y mantener su idempotencia.
- **Los resultados no muestran disponibilidad exacta.** Se resuelve con un dato aproximado (un indicador "hay stock") **[propuesta]** que Inventario publicaría como evento; hoy no existe ese evento.

## Pendientes

- **Todo:** el indexador, el índice, el mapeo, la consulta y los eventos de Catálogo no existen todavía.
- **Evento de disponibilidad** de Inventario **[propuesta]**.
- **Decidir motor definitivo** después de armar una prueba con datos reales.
- **Medir la frescura** y fijar el objetivo.

## Validación

A hacer cuando se implemente:

1. Editar un producto y medir el tiempo hasta que la búsqueda lo refleja.
2. Reenviar el mismo evento dos veces y uno viejo: el documento no cambia.
3. Apagar el buscador: la navegación por categoría responde igual y la búsqueda devuelve `503`.
4. Reindexar desde cero con un alias y comprobar que no hay corte.
