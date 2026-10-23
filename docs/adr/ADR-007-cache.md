# ADR-007 · Caché

|                             |                                                                                                                                                                                  |
| --------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Estado**                  | **Propuesta** · Entrega 2. **No hay nada implementado.** Se aplica solo si la medición base lo justifica.                                                                         |
| **Fecha**                   | 23/10/2026                                                                                                                                                                       |
| **Decisión de la consigna** | D7 · Caché                                                                                                                                                                       |
| **Relacionadas**            | [ADR-003](ADR-003-persistencia.md) (Mongo), [ADR-006](ADR-006-busqueda.md) (búsqueda), [ADR-011](ADR-011-observabilidad.md) (métricas), [ADR-013](ADR-013-capacidad-y-costos.md) (medición) |

> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar · _previsto_: forma parte del diseño pero todavía no está implementado.

## Contexto

La consigna pide justificar qué se cachea, dónde, con qué política de invalidación y qué pasa si el caché falla o está desactualizado. El apunte de caché propone preguntas previas: cuánta lectura hay, qué tan costosa es, cuánta desactualización tolera el negocio, qué pasa si falla.

Las lecturas más repetidas son las del catálogo: **ver un producto** y **listar por categoría**. Cambian poco y se leen mucho. En cambio, **la disponibilidad** cambia con cada reserva y **el precio con el que se cobra** debe ser el vigente en el momento del pedido.

## Criterios

1. **Justificarlo con una medición,** no por costumbre.
2. **No servir datos que el negocio no tolera viejos** (stock y precio de cobro).
3. **Si el caché cae, el servicio sigue** (más lento, pero correcto).
4. **Invalidación simple y explícita.**
5. **Verse en métricas:** aciertos, fallos, evicciones.

## Decisión **[propuesta]**

### Qué se cachea y qué no

| Dato                                        | ¿Se cachea? | Motivo                                                                                           |
| ------------------------------------------- | ----------- | ------------------------------------------------------------------------------------------------ |
| Detalle público de un producto              | Sí          | Muy leído, cambia poco                                                                           |
| Listado por categoría                       | Sí          | Muy leído, tolera segundos de atraso                                                             |
| **Disponibilidad (stock)**                  | **No**      | Cambia con cada reserva; un dato viejo vende de más                                              |
| **Precio usado para cobrar**                | **No**      | Se calcula al recibir el pedido contra Catálogo (el cache solo sirve para mostrar)               |
| Respuestas de escritura                     | No          | —                                                                                                |

### Dónde: Redis, patrón cache-aside

Catálogo lee del caché; si no está, lee de Mongo y lo guarda. Se usa **Redis** (compartido entre réplicas) y no un caché en memoria de cada proceso, porque con varias réplicas el caché local daría respuestas distintas según la réplica y se invalidaría mal.

### Claves, vencimiento e invalidación

- **Producto:** `catalogo:producto:{id}:publico:v1`, vencimiento de unos **5 minutos con variación aleatoria** (jitter) para que no venzan todas juntas.
- **Listado:** `catalogo:categoria:{id}:v{n}:pagina:{p}`, donde `n` es un **contador de versión por categoría**. Cuando un producto de esa categoría cambia, se incrementa el contador y las páginas viejas dejan de usarse (vencen solas).
- **Invalidación:** al editar o despublicar un producto, Catálogo **borra la clave después de escribir en Mongo**. El vencimiento es la red de seguridad si el borrado falla.
- **Precio vigente:** el precio de una promoción se calcula al leer, a partir de las fechas; no se guarda un precio resuelto en el caché más allá de su vencimiento.
- El prefijo `v1` en la clave permite cambiar el formato sin vaciar todo.

### Si el caché falla

- Timeout corto a Redis (unos **100 ms**).
- Ante error o timeout: se lee de Mongo y se sigue, sin devolver error al cliente.
- **Una sola lectura a Mongo por clave** cuando muchos pedidos fallan a la vez (colapsar pedidos iguales), para evitar una avalancha sobre la base.
- Política de expulsión `allkeys-lru` y memoria máxima acotada.

### Medición **[propuesta]**

Primero se mide **sin** caché (línea base). Se activa con un interruptor (`CACHE_ENABLED`) y se compara latencia y carga de Mongo. Si la diferencia no justifica la pieza, **no se incorpora**.

Métricas: aciertos, fallos, errores de Redis, evicciones y latencia con y sin caché.

## Alternativas consideradas

| Alternativa                                  | Por qué se descartó                                                                                                                                  |
| -------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Caché local en memoria del proceso**       | Más simple y sin red, pero cada réplica tendría contenido distinto y la invalidación no llegaría a todas. Podría servir como segundo nivel.          |
| **Caché de nginx (`proxy_cache`)**           | Fácil, pero invalidar por producto es difícil y no distingue datos públicos de privados con claridad.                                                 |
| **Write-through / write-behind**             | Complican la escritura y arriesgan perder datos (write-behind). El catálogo se escribe poco: no hay ganancia.                                         |
| **Cachear la disponibilidad con TTL corto**  | Aun con segundos de atraso, puede vender de más. El stock lo valida Inventario con una actualización condicional (ADR-004).                           |
| **No cachear nada**                          | Es válido si la medición no muestra necesidad. Por eso la propuesta está condicionada a la línea base.                                                |

## Consecuencias

**Positivas**

- Menos lecturas a Mongo para lo más leído.
- Si Redis cae, el servicio sigue.
- Lo que el negocio no tolera viejo (stock, precio de cobro) nunca pasa por el caché.

**Negativas aceptadas**

- **Una pieza más** (Redis) y un nuevo modo de falla.
- **Datos levemente viejos** hasta el vencimiento si falla el borrado.
- **Arranque en frío:** con el caché vacío, todas las lecturas van a Mongo.
- **El precio mostrado puede diferir del cobrado** si cambió en esos minutos; por eso el cobro siempre recalcula.

## Pendientes

- **Todo:** no existe código de caché.
- **Línea base de carga** para decidir si hace falta (ADR-013).
- **Dimensionar memoria y vencimiento** con datos reales.

## Validación

A hacer cuando se implemente:

1. Medir latencia y consultas a Mongo con y sin `CACHE_ENABLED`.
2. Editar un producto: la siguiente lectura trae el dato nuevo.
3. Apagar Redis: el catálogo responde igual (más lento) y sin errores al cliente.
4. Vaciar el caché y lanzar lecturas concurrentes de una misma clave: Mongo recibe una sola lectura.
