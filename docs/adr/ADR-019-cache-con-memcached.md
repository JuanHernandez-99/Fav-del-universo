# ADR-019 · Caché con Memcached

| | |
|---|---|
| **Estado** | Aceptada · Reemplaza a [ADR-007](ADR-007-cache.md) solo en el motor: Memcached en lugar de Redis |
| **Fecha** | 09/11/2026 |
| **Decisión de la consigna** | D7 · Caché |
| **Relacionadas** | [ADR-007](ADR-007-cache.md) (qué se cachea e invalidación, siguen vigentes), [ADR-011](ADR-011-observabilidad.md) (alertas) |

## Contexto

ADR-007 eligió Redis sin compararlo con las opciones vistas en clase: ccache y Memcached. La cátedra admite otras tecnologías si hay una razón significativa, y al revisarla no había una: el diseño usa Redis solo para guardar valores con vencimiento y subir un contador. Memcached también lo hace.

## Decisión

**Se usa Memcached, compartido entre las réplicas, con 64 MB y sin persistencia.** Solo cambia `internal/cache`, porque el resto de Catálogo usa la interfaz `Cache`.

**No cambia** lo que define ADR-007: qué se cachea, cache-aside, invalidación por generación, vencimiento de 5 minutos con variación, pausa de 5 s tras un error, `singleflight` y las métricas.

### Diferencias con Redis que hubo que resolver

| Diferencia | Sin resolverla | Solución |
|---|---|---|
| No acepta claves de más de 250 bytes, con espacios o caracteres de control | Un id raro en la URL daría un error y pausaría la caché de la réplica | Esas claves se resumen con SHA-256 |
| No acepta valores de más de 1 MB | Igual que la anterior | Esos valores no se guardan |
| `incr` falla si la clave no existe | No se podría invalidar por primera vez | Se crea con `add`. Si otra réplica ganó, se incrementa |
| Vence por segundos, y 0 significa que no vence | Un vencimiento muy corto quedaría para siempre | Se redondea hacia arriba, con un mínimo de 1 s |
| `memcache.New` resuelve la dirección una sola vez | Catálogo no encontraría a Memcached si cambia de IP o no está al arrancar | Un selector propio, para que se resuelva en cada conexión |
| La biblioteca reutiliza conexiones muertas tras un reinicio | Ver abajo | Se vacían las conexiones en espera ante un error |
| La biblioteca no expone estadísticas ni usa `context` | Sin métrica de evicciones | Se lee `stats` por TCP. Los tiempos de espera son los del cliente: 100 ms |

## Resultados

Medido el 09/11/2026 con k6, 70 % fichas y 30 % listados, durante 30 s, 2 réplicas de Catálogo. **Condiciones:** una sola computadora, con k6 y todo el sistema juntos, y 19 productos publicados. Sirve para comparar, no como capacidad de producción (ADR-013).

**Carga sobre MongoDB: baja más de un 99 %, igual que con Redis.**

| Pedidos por segundo | Consultas por segundo, sin caché | Con caché |
|---|---|---|
| 300 | 602 | 2 |
| 600 | 1.202 | 2 |
| 1.000 | 2.002 | 2 |

Las 2 que quedan son la búsqueda de cambios sin avisar de las dos réplicas (ADR-006). En una corrida a 600 pedidos por segundo, los contadores de una réplica dieron más de 99,9 % de aciertos.

**Latencia con MongoDB sin límite: no concluyente.** p95, dos corridas por caso:

| Pedidos por segundo | Sin caché | Con caché |
|---|---|---|
| 300 | 3,4 y 3,2 ms | 3,5 y 2,1 ms |
| 600 | 4,2 y 2,9 ms | 2,2 y 2,2 ms |
| 1.000 | 2,8 y 3,8 ms | 2,3 y 2,5 ms |

**Con MongoDB limitado a 0,25 CPU:** simula una base en una máquina chica. 600 pedidos por segundo, 4 corridas por caso:

| | Sin caché | Con caché |
|---|---|---|
| Pedidos fallidos | **19 % a 40 %** | **0 %** |
| Atendidos por segundo | 362 a 384 | 600 |
| p95 | 4 s (el límite del pedido) | **1,9 a 3,5 ms** |

**Conclusión:** como en ADR-007, la caché se justifica por la **capacidad**, no por la latencia en condiciones normales.

### Reinicio de Memcached con carga

La prueba encontró un problema que los tests no mostraban. A 1.000 pedidos por segundo, cada conexión vieja daba un error, y cada error, otros 5 s de pausa:

| | Antes de corregir | Después |
|---|---|---|
| Una réplica seguía sin usar la caché | Más de 70 s | Unos 10 s, tras el reinicio |
| Pausas entre las dos réplicas | Más de 20 | 3 |
| Errores para el comprador | 0 | 0 |

`TestTrasUnReinicioNoQuedanConexionesMuertas` lo cubre. Sin el arreglo, fallan 10 de 10 lecturas; con el arreglo, como máximo 1.

## Alternativas consideradas

| Alternativa | Por qué se descartó |
|---|---|
| **Seguir con Redis** | Funcionaba, y sus mediciones eran equivalentes. Pero el proyecto no usa nada que Memcached no tenga, y no había una razón significativa para apartarse de lo visto en clase. |
| **ccache** (caché en memoria de cada réplica) | Vista en clase. Con dos réplicas, cada una podría responder distinto, y la invalidación por generación no llegaría a la otra. Es lo mismo que ADR-007 descartó para la caché local. |

## Consecuencias

**Positivas**

- La caché coincide con lo visto en clase, con las mismas garantías y resultados.
- Una réplica se recupera sola tras un reinicio de Memcached, sin errores para el comprador.

**Negativas aceptadas**

- **Vaciar las conexiones ante un error cierra también las sanas.** Se vuelven a abrir al usarlas, y la pausa de 5 s limita cuántas veces puede pasar.
- **Un valor de más de 1 MB nunca se cachea,** sin aviso. Con este catálogo no ocurre.
- **Sin expiración por generación:** si Memcached expulsara la clave de la generación, volvería a 0, como en ADR-007.

## Validación

Probado el 09/11/2026:

- **Tests:** `internal/cache` contra Memcached real (`CATALOGO_TEST_MEMCACHED`), incluida la creación simultánea de la generación. `internal/service`, también con el detector de carreras.
- **`services/catalogo/pruebas/cache.sh`, por el gateway:** invalidación por edición y por promoción, avalancha de 100 pedidos iguales con 1 lectura a MongoDB, y lecturas y edición con Memcached apagado, sin errores.
- **Con Memcached apagado:** cada lectura respondió entre 5 y 14 ms.
- **Alertas:** `CacheNoDisponible` e `InvalidacionDeCacheFallida` completaron el ciclo (`observabilidad/probar_alertas.sh`).

**Sin probar:** la tienda en el navegador, Memcached en la nube, una expulsión real por falta de memoria y un valor de más de 1 MB fuera de los tests.
