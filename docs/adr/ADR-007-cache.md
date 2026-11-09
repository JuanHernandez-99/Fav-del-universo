# ADR-007 · Caché

|                             |                                                                                                                                                                                  |
| --------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Estado**                  | **Aceptada e implementada** (08/11/2026). Propuesta en la Entrega 2; ver "Cambios respecto de la propuesta".                                                                      |
| **Fecha**                   | 23/10/2026, actualizada el 08/11/2026                                                                                                                                            |
| **Decisión de la consigna** | D7 · Caché                                                                                                                                                                       |
| **Relacionadas**            | [ADR-003](ADR-003-persistencia.md) (Mongo), [ADR-006](ADR-006-busqueda.md) (búsqueda), [ADR-011](ADR-011-observabilidad.md) (métricas), [ADR-013](ADR-013-capacidad-y-costos.md) (medición) |

> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar.

## Contexto

La consigna pide caché en un flujo de lectura relevante, elegida por una **necesidad observable** y con **medición comparativa antes y después**. También pide justificar qué se guarda, cómo se invalida, cuánto dura, qué pasa si falla y con qué se mide.

Las lecturas más repetidas son las de la tienda: **ver un producto** y **listar por categoría**. Cambian poco y se leen mucho. En cambio, **la disponibilidad** cambia con cada reserva y **el precio con el que se cobra** debe ser el vigente en el momento del pedido.

**La necesidad medida:** sin caché, cada lectura de la tienda hace 2 consultas a MongoDB, una del producto o el listado y otra de las promociones. A 1.000 pedidos por segundo son unas 2.000 consultas por segundo sobre la base de la que dependen también la gestión del catálogo, los pedidos y los cobros.

## Criterios

1. **Justificarlo con una medición,** no por costumbre.
2. **No servir datos que el negocio no tolera viejos** (stock y precio de cobro).
3. **Si el caché cae, el servicio sigue.**
4. **Invalidación simple y explícita.**
5. **Verse en métricas:** aciertos, fallos, errores, evicciones.

## Decisión

### Qué se cachea y qué no

| Dato                                        | ¿Se cachea? | Motivo                                                                                           |
| ------------------------------------------- | ----------- | ------------------------------------------------------------------------------------------------ |
| Ficha de un producto publicado              | Sí          | Muy leída, cambia poco                                                                           |
| Listados de la tienda sin texto             | Sí          | Muy leídos                                                                                       |
| Promociones, **para mostrar**               | Sí          | Se leen en cada pedido. La vigencia se evalúa al leer                                            |
| Búsqueda por texto                          | No          | Los ids salen de Elasticsearch (ADR-006)                                                         |
| Producto no publicado o inexistente         | No          | Publicarlo tiene que verse enseguida                                                             |
| **Disponibilidad (stock)**                  | **No**      | Cambia con cada reserva; un dato viejo vende de más                                              |
| **Precio usado para cobrar**                | **No**      | `GET /v1/variantes` y la capacidad publicada leen de MongoDB. Lo cubre un test                   |
| Panel de administración                     | No          | Necesita la versión real para editar                                                             |

**El precio no se guarda resuelto.** Se guardan el producto y las promociones, y el precio vigente se calcula en cada lectura. Así, una promoción empieza y termina en su hora exacta aunque esté en la caché.

### Dónde: Redis, patrón cache-aside

Catálogo lee del caché; si no está, lee de Mongo y lo guarda. Se usa **Redis**, compartido entre las réplicas, porque con caché en memoria cada réplica podría responder distinto y la invalidación no llegaría a todas. Solo `internal/cache` habla con Redis.

Redis corre sin persistencia, con `maxmemory 64mb` y `allkeys-lru`: si se pierde, los datos están en Mongo. Con el catálogo de prueba usó 1,15 MB.

### Claves, vencimiento e invalidación

- **Clave:** `catalogo:v1:g{generación}:{producto:id | listado:huella | promociones}`. La huella identifica todos los filtros de la página.
- **Vencimiento:** 5 minutos más hasta un 20 % de variación, para que no venzan todas juntas.
- **Invalidación por generación:** al editar, publicar, despublicar, agregar una variante o cambiar una promoción, Catálogo sube un contador **después de escribir en Mongo**. Las claves anteriores dejan de leerse y vencen solas.

**Por qué generación y no borrar la clave.** Con el borrado hay una carrera: alguien lee el dato viejo, otro escribe y borra, y el primero guarda lo viejo, que queda hasta el vencimiento. Con la generación, el primero lo guarda en una clave que ya nadie lee. Además, un cambio de producto afecta a muchos listados, y así no hace falta saber cuáles. El costo es que cualquier cambio vacía toda la caché; es aceptable porque hay un solo administrador y escribe poco.

### Si el caché falla

| Situación | Comportamiento |
|---|---|
| Redis no responde | Espera hasta 100 ms, sin reintentos, y lee de Mongo. **El comprador no ve un error** |
| Sigue caído | **Pausa de 5 s** tras cada error: esa réplica va directo a Mongo, sin intentar. Sin esto, cada lectura pagaría el tiempo de espera |
| No se puede invalidar | La escritura no falla. El dato puede quedar viejo hasta 6 minutos. Queda en el log y en la métrica |
| Muchos pedidos iguales con la caché vacía | **`singleflight`:** una sola lectura a Mongo por clave y por réplica |
| Entrada dañada | Cuenta como fallo y se reemplaza |

### Medición

Se activa con `CACHE_ENABLED` (por defecto, activa). `loadtest/comparar_cache.sh` corre lo mismo sobre el mismo código, alternando el orden. Métricas en Prometheus, también en el job nuevo `catalogo`:

| Métrica | Para qué |
|---|---|
| `cache_operaciones_total{recurso,resultado}` | Aciertos, fallos, errores y omitidas por la pausa |
| `cache_lecturas_fuente_total{recurso}` | Lecturas a Mongo para completar la caché |
| `cache_invalidaciones_total{resultado}`, `cache_evicciones` | Invalidaciones que fallaron y claves expulsadas |
| `http_duracion_segundos`, `http_pedidos_total` | Latencia de Catálogo por ruta |

Tasa de aciertos: `sum(rate(cache_operaciones_total{resultado="acierto"}[1m])) / sum(rate(cache_operaciones_total[1m]))`

## Resultados

Medido el 08/11/2026 con k6, 70 % fichas y 30 % listados, durante 30 s, 2 réplicas de Catálogo. **Condiciones:** una sola computadora, con k6 y todo el sistema juntos, y 19 productos publicados. Sirve para comparar, no como capacidad de producción (ADR-013).

**Carga sobre MongoDB: es el resultado más firme.**

| Pedidos por segundo | Consultas por segundo, sin caché | Con caché |
|---|---|---|
| 300 | 602 | 2 |
| 600 | 1.202 | 2 |
| 1.000 | 1.969 y 1.997 | 2 |

Baja más de un 99 %. Las 2 que quedan son la búsqueda de cambios sin avisar de las dos réplicas (ADR-006). En las corridas con caché, los contadores de una réplica dieron más de 99,9 % de aciertos.

**Latencia con MongoDB sin límite: no concluyente.** p95, dos corridas por caso:

| Pedidos por segundo | Sin caché | Con caché |
|---|---|---|
| 300 | 8,5 y 6,3 ms | 7,0 y 5,4 ms |
| 600 | 4,2 y 8,0 ms | 16,9 y 6,0 ms |
| 1.000 | 226 y 21 ms | 8 y 115 ms |

Los picos aparecen de los dos lados y cambian entre repeticiones. Con este volumen, MongoDB en esta máquina no es el límite, y el ruido pesa más que la diferencia. **No se afirma que la caché mejore la latencia en estas condiciones.**

**Con MongoDB limitado a 0,25 CPU:** simula una base en una máquina chica. 600 pedidos por segundo:

| | Sin caché (4 corridas) | Con caché (7 corridas) |
|---|---|---|
| Pedidos fallidos | **41 % a 60 %** | **0 % en 6 corridas**, 7,5 % en 1 |
| Atendidos por segundo | 242 a 303 | 600 en esas 6, 434 en la otra |
| p95 | 4 s (el límite del pedido) | **6,9 a 20,9 ms** en esas 6, 4 s en la otra |

La corrida con errores coincidió con ediciones en la misma máquina, pero **la causa no se confirmó**. Las tres últimas corridas se hicieron sin otra actividad y dieron 0 errores.

**Conclusión:** la caché se justifica por la **capacidad**: saca casi toda la lectura de la tienda de la base, y es lo que permite atender este pico con una base chica.

## Cambios respecto de la propuesta

| Propuesta | Implementado | Por qué |
|---|---|---|
| Borrar la clave al escribir, y un contador por categoría | Una sola generación | Evita la carrera de la lectura lenta y es más simple. Escribir es poco frecuente |
| Promociones sin cachear | Promociones cacheadas para mostrar, con la vigencia evaluada al leer | Si no, cada lectura seguiría consultando a Mongo |
| Sin pausa ante errores | Pausa de 5 s | Para no pagar el tiempo de espera en cada lectura |

## Alternativas consideradas

| Alternativa                                  | Por qué se descartó                                                                                                                                  |
| -------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Caché local en memoria del proceso**       | Más simple y sin red, pero cada réplica tendría contenido distinto y la invalidación no llegaría a todas.                                            |
| **Caché de nginx (`proxy_cache`)**           | Fácil, pero invalidar por producto es difícil y no distingue datos públicos de privados con claridad.                                                 |
| **Write-through / write-behind**             | Complican la escritura y arriesgan perder datos (write-behind). El catálogo se escribe poco: no hay ganancia.                                         |
| **Guardar el precio ya calculado**           | Una promoción seguiría aplicándose después de vencer, hasta el vencimiento de la clave.                                                               |
| **Cachear la disponibilidad con TTL corto**  | Aun con segundos de atraso, puede vender de más. El stock lo valida Inventario con una actualización condicional (ADR-004).                           |
| **No cachear nada**                          | Con esta base y este volumen, la latencia sin límite de CPU casi no cambia. Pero la consigna la pide, y con una base chica la diferencia es enorme.   |

## Consecuencias

**Positivas**

- Más del 99 % menos de consultas a Mongo para lo más leído.
- Si Redis cae, el servicio sigue sin errores.
- Lo que el negocio no tolera viejo (stock, precio de cobro) nunca pasa por el caché.

**Negativas aceptadas**

- **Una pieza más** (Redis) y un nuevo modo de falla.
- **Cualquier cambio vacía toda la caché.** Con muchas escrituras dejaría de servir.
- **Datos levemente viejos,** hasta 6 minutos, si falla una invalidación.
- **Arranque en frío:** con el caché vacío, las lecturas van a Mongo. Esa ventana no se midió con la base limitada.
- **`singleflight` y la pausa son por réplica:** con N réplicas, hasta N lecturas por clave.
- **El precio mostrado puede diferir del cobrado** si la promoción cambió y falló la invalidación; el cobro siempre recalcula.
- **Si Redis expulsara la clave de la generación,** volvería a 0 y podrían reaparecer entradas viejas de esa generación. Es muy improbable: se lee en cada pedido, y esas entradas vencen en minutos.

## Pendientes

- **Repetir la medición con el catálogo completo y en la nube** (ADR-013).

## Validación

Probado el 08/11/2026 con `services/catalogo/pruebas/cache.sh`, por el gateway, y tests.

- **Invalidación:** la lectura siguiente a una edición trae el nombre y el precio nuevos, también en el listado. Una promoción nueva se aplica en la lectura siguiente.
- **Avalancha:** con la caché vacía, 100 pedidos iguales hicieron 1 lectura a Mongo.
- **Redis apagado, 300 pedidos por segundo durante 30 s:** 0 errores, p95 de 9,4 ms y p99 de 105 ms. Mongo pasó a recibir 596 consultas por segundo. Al volver, 0 errores, p95 de 6,1 ms y la caché se llenó sola.
- **La pausa:** un test comprueba que, tras un error, no se vuelve a consultar a Redis durante la pausa. **Su efecto en la latencia no se midió contra una versión sin pausa.**
- **Tests:** `internal/service/cache_test.go`, también con el detector de carreras, incluye la lectura lenta y que Ventas no usa la caché. `internal/cache/redis_test.go` corre contra Redis real (`CATALOGO_TEST_REDIS`).
- **Sin probar:** la tienda en el navegador.
