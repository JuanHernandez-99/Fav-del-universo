# ADR-013 · Capacidad, escalado y costos

|                             |                                                                                                                                                         |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Estado**                  | **Aceptada con mediciones** (10/11/2026). Propuesta en la Entrega 2; ver "Cambios respecto de la propuesta". |
| **Fecha**                   | 23/10/2026, actualizada el 10/11/2026                                                                                                                   |
| **Decisión de la consigna** | D13 · Capacidad y costos                                                                                                                                |
| **Relacionadas**            | [ADR-004](ADR-004-consistencia-y-concurrencia.md) (contención), [ADR-006](ADR-006-busqueda.md) (búsqueda), [ADR-011](ADR-011-observabilidad.md) (objetivo de servicio), [ADR-012](ADR-012-balanceo.md) (réplicas), [ADR-020](ADR-020-logs-y-track-id-modelo-catedra.md) (observabilidad actual) |

> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar.

## Contexto

La consigna pide probar la carga, encontrar el principal límite, justificar cómo escalar y estimar el costo. El SPEC (RNF-06) pide un escenario de pico: cambio de temporada y diciembre.

El volumen real del cliente es chico: **entre 10 y 20 prendas por semana** (SPEC §3). La prueba no busca "aguantar diciembre", que cualquier configuración aguanta, sino saber dónde se rompe el sistema y qué cuesta operarlo.

## Criterios

1. **Medir antes de afirmar:** ninguna capacidad se declara sin una prueba.
2. **Encontrar el primer límite,** no solo cuánto aguanta.
3. **Escalar donde duele,** no todo por igual.
4. **Costos con supuestos explícitos y precios citables.**

## Escenario

**Pico de la tienda** (`loadtest/tienda.js`, k6): compradores que entran por el gateway, como en el navegador.

| Operación | Ruta | Peso **[propuesta]** |
|---|---|---|
| Ver una ficha | `GET /catalogo/v1/tienda/productos/{id}` (caché) | 40 % |
| Ver un listado por categoría | `GET /catalogo/v1/tienda/productos?categoria=…` (caché) | 25 % |
| Buscar | `GET /catalogo/v1/tienda/productos?q=…` (Elasticsearch) | 15 % |
| Ver unidades disponibles | `GET /inventario/v1/disponibilidad` | 15 % |
| Comprar como invitado, por transferencia | `POST /ventas/v1/compras` (Ventas → Catálogo → Inventario) | 5 % |

- La mitad de las compras va a la misma variante ("la más pedida"), para ver la contención sobre una fila de stock (ADR-004).
- Tasa fija por operación (`constant-arrival-rate`): si el sistema se pone lento, k6 no baja la carga.
- **Objetivo** (ADR-011): p95 menor a 300 ms y menos de 1 % de errores.
- Escalones de 60 s con `loadtest/pico_tienda.sh`, que además toma la CPU y la memoria de cada contenedor a mitad del escalón y las esperas por bloqueo de filas en el MySQL de Inventario.

**Dónde se midió:** una notebook (Intel i7-13620H) con Docker Desktop: 16 vCPU y 7,6 GB para todos los contenedores, **incluido k6**, que a tasas altas usa entre 1 y 2,5 núcleos. Los números valen para comparar configuraciones entre sí, no como capacidad de la VM de Azure.

## Resultados (10/11/2026)

**Se midió antes de [ADR-020](ADR-020-logs-y-track-id-modelo-catedra.md),** con la observabilidad anterior, que trazaba cada pedido. En las tablas, "sin trazas" es lo más parecido al sistema actual. Cómo afectaban las trazas y por qué se reemplazaron está en ADR-020.

### Escalones con dos réplicas de Catálogo y de Inventario

| Pedidos/s (objetivo) | Logrados | Errores | p95 total | p95 compra | p95 búsqueda | Esperas por bloqueo | Corrida |
|---|---|---|---|---|---|---|---|
| 50 | 52 | 0 % | 24 ms | 45 ms | 34 ms | — | 1 |
| 100 | 100 | 0 % | 23 ms | 48 ms | 22 ms | — | 1 |
| 200 | 199 | 0 % | 22 ms | 47 ms | 17 ms | — | 1 |
| 400 | 388 | 0 % | 35 ms | 250 ms | 39 ms | — | 1 |
| 600 | 596 | 0 % | 33 ms | 171 ms | 28 ms | 91 | con trazas, recién vaciadas |
| 800 | 793 | 0 % | 65 ms | 262 ms | 77 ms | 316 | con trazas, recién vaciadas |
| 1200 | 1020 | **13 %** | **2,9 s** | 5,0 s | 2,8 s | 2233 | sin trazas |
| 1600 | 1485 | 0,8 % | **1,3 s** | 4,6 s | 0,9 s | no se pudo medir | sin trazas |
| 2000 | 1115 | **7,3 %** | **4,2 s** | 5,3 s | 3,1 s | 2053 | sin trazas |

- **El objetivo se cumple hasta 800 pedidos por segundo** y se rompe antes de 1200.
- Entre 1200 y 2000, los errores fueron `503` del gateway (ninguna réplica respondió a tiempo) y pedidos que k6 abandonó (`499` en el log del gateway). A 1200, la búsqueda fue la que más falló: 72 % de error, con Elasticsearch usando 2,7 núcleos.
- A 1600 y 2000 el resultado no es parejo (menos errores a 1600 que a 1200). Pasado el límite, la máquina entera está saturada, k6 incluido, y los números ya no comparan bien.
- La **compra** es la operación que primero se acerca al objetivo: 262 ms de p95 a 800 pedidos/s. Pasa por tres servicios, dos veces por el gateway y dos transacciones.

### Una réplica o dos (sin trazas)

| Réplicas de Catálogo e Inventario | 600 pedidos/s: p95 · compra | 800 pedidos/s: p95 · compra |
|---|---|---|
| 2 | 55 ms · 307 ms | 49 ms · 263 ms |
| 1 | 27 ms · 119 ms | 40 ms · 189 ms |

**En una sola máquina, una segunda réplica no suma capacidad:** las dos comparten los mismos núcleos, y un proceso Go ya usa varios (Catálogo llegó a 1,5 núcleos con una réplica). Las réplicas dan disponibilidad ante la caída de una (ADR-012), no rendimiento.

### Contención sobre la variante más pedida

| | 600 pedidos/s | 800 pedidos/s |
|---|---|---|
| p95 de la compra: variante popular / otras (dos réplicas, sin trazas) | 318 / 291 ms | 305 / 221 ms |
| p95 de la compra: variante popular / otras (una réplica, sin trazas) | 128 / 105 ms | 208 / 172 ms |
| Esperas por bloqueo en MySQL de Inventario (con trazas, recién vaciadas) | 91 | 316 |

Comprar la variante popular es entre 10 y 40 % más lento, y las esperas por bloqueo crecen más rápido que la tasa. A 1200 pedidos/s fueron más de 2000 en un minuto.

### Hipótesis de la propuesta

| # | Hipótesis | Resultado |
|---|---|---|
| 1 | Contención sobre una fila de stock | **Confirmada, pero no es el primer límite.** Crece con la tasa y se nota en la variante popular; hasta 800 pedidos/s la compra sigue dentro del objetivo. |
| 2 | Pool de conexiones de Inventario | **Sin evidencia:** no hay una métrica de espera del pool. MySQL de Inventario no pasó de medio núcleo. |
| 3 | Una sola base MySQL | **Descartada en este rango:** MySQL de Inventario usó como mucho medio núcleo hasta 800 pedidos/s. |
| 4 | Límite por grupo del gateway | **No aplica:** las rutas de la tienda no tienen límite por grupo. |
| — | Memoria de la observabilidad anterior (no estaba prevista) | **Confirmada:** guardaba las trazas en memoria sin límite y fue lo primero que degradó el sistema con carga sostenida. ADR-020 la reemplazó. |

## Principal límite

**CPU de la máquina, entre 800 y 1200 pedidos/s.** Todo comparte los mismos núcleos (los contenedores llegaron a 12 de 16, más k6). Elasticsearch es el que más CPU pide al saturarse, y la búsqueda es la primera en fallar. La compra es la operación más cercana al objetivo.

Antes de la CPU apareció la memoria de la observabilidad anterior (última fila de la tabla de hipótesis). Ya no aplica: ADR-020 la reemplazó.

Para FAV, el límite de cómputo no es el problema: 800 pedidos/s con 5 % de compras son 40 compras por segundo, contra 10 a 20 prendas por semana. Lo que hoy crece sin límite con el uso es la tabla de logs, en disco (ADR-020).

## Cómo escalar

| Opción | Cuándo | Evidencia |
|---|---|---|
| **VM más grande** (escalado vertical) | Si la CPU fuera el límite con tráfico real | En una máquina, más réplicas no suman (tabla "Una réplica o dos") |
| **Elasticsearch y las bases en otra VM o como servicio administrado** | Si una VM grande no alcanzara | Elasticsearch es el primero en saturarse |
| **Más réplicas en otras máquinas** | Recién con más de una VM | — |
| **Repartir el stock de una variante muy pedida** **[?]** | Solo si la contención pasara a ser el límite | Hoy no lo es (hipótesis 1) |
| Cachear la disponibilidad | **No:** el stock tiene que ser exacto (ADR-007) | — |

## Costos

**Supuestos:** una sola VM Linux con todo el Docker Compose, como el despliegue actual (`desplegar/README.md`), en la región **Chile Central** de Azure, con precio por uso (pay-as-you-go) y 730 horas por mes. Los precios salen de la [API pública de precios de Azure](https://prices.azure.com/api/retail/prices), consultada el 10/11/2026, en dólares y sin impuestos.

**Tamaño mínimo por memoria:** sin carga, el sistema usa **3,7 GB** (medido el 11/11/2026 con `docker stats`, tres muestras, con todo levantado hacía 47 minutos y sin tráfico: Elasticsearch 1,1 GB, los cuatro MySQL 1,7 GB, MongoDB 0,2 GB, el resto 0,7 GB). Una VM de 4 GB queda sin margen; la de 8 GB es la mínima razonable.

| Componente | Precio | Costo mensual |
|---|---|---|
| VM **B2as v2** (2 vCPU, 8 GB), la que usa el despliegue | USD 0,105 por hora | USD 76,65 (0,105 × 730) |
| Disco del sistema Standard SSD E4 (32 GB) | USD 3,36 por mes | USD 3,36 |
| IP pública estática estándar | USD 0,005 por hora | USD 3,65 |
| Transferencia de salida | Primeros 100 GB del mes sin cargo | USD 0 con el volumen de FAV **[supuesto]** |
| **Total** | 76,65 + 3,36 + 3,65 | **USD 83,66 por mes** |

| Otros tamaños | Precio por hora | VM por mes | Por qué no |
|---|---|---|---|
| B2s (2 vCPU, 4 GB) | USD 0,0582 | USD 42,49 | Menos memoria que la que usa el sistema en reposo |
| B2ms (2 vCPU, 8 GB) | USD 0,116 | USD 84,68 | Misma memoria que la B2as v2 y 10 % más cara. Con el disco y la IP, USD 91,69 por mes |
| D2s v5 (2 vCPU, 8 GB) | USD 0,134 | USD 97,82 | Misma memoria que la B2as v2, más cara. Conviene solo con CPU alta todo el tiempo |
| B4ms (4 vCPU, 16 GB) | USD 0,233 | USD 170,09 | El doble, sin una carga que lo justifique |

- **Uso bajo y pico cuestan lo mismo:** con 10 a 20 prendas por semana, diciembre no obliga a cambiar de VM.
- **Desasignar la VM** cuando no se usa deja de cobrar el cómputo; el disco y la IP siguen cobrando (`desplegar/README.md`).
- La VM del grupo es una B2as v2, con la memoria que pide el sistema en reposo (3,7 GB de 8 GB).
- **No medido:** la capacidad en la VM de 2 vCPU. Va a ser bastante menor que la de la notebook, pero con el volumen del cliente el margen sigue siendo muy grande.

## Cambios respecto de la propuesta

| Propuesta (23/10) | Ahora | Por qué |
|---|---|---|
| Solo Inventario: 70 % consultar stock, 30 % reservar | Pico de la tienda con los cuatro servicios | En la Entrega 2 Inventario era el único servicio real; ahora se puede probar el flujo del comprador (RNF-06) |
| Contención como primer cuello candidato | CPU (antes, la memoria de la observabilidad anterior, reemplazada por ADR-020) | Lo midió la prueba |
| Costos sin proveedor | Azure, Chile Central | Es donde está el despliegue |

## Alternativas consideradas

| Alternativa | Por qué se descartó |
|---|---|
| **Estimar capacidad por cálculo, sin prueba** | Sin medición no se puede defender; la consigna pide la prueba. |
| **Probar contra la VM de Azure** | Le carga tráfico al despliegue que usan los otros grupos y puede generar costos. Se prefirió repetir en local. |
| **Generar la carga con un script propio** | k6 mantiene fija la tasa aunque el sistema se ponga lento. Un bucle propio esperaría cada respuesta y bajaría la carga justo al saturarse. |
| **Escalar todo por igual** | Gasta donde no hace falta: la prueba mostró que más réplicas en la misma máquina no suman. |

## Consecuencias

**Positivas**

- Hay una capacidad medida y un límite identificado, con el método para repetirlos (`loadtest/pico_tienda.sh`).
- La prueba encontró un riesgo real de producción antes de que pase en la nube: la memoria de la observabilidad anterior, que llevó a ADR-020.

**Negativas aceptadas**

- **Los números son de una notebook,** con k6 en la misma máquina. Sirven para comparar, no para prometer capacidad en Azure.
- **La mezcla de operaciones es supuesta:** el cliente no tiene datos de navegación.
- **Cada corrida deja datos:** compras de prueba en Ventas y un millón de unidades por variante en Inventario. En local se borran con `docker compose down -v`.

## Pendientes

- **Repetir la corrida de 800 pedidos/s con la observabilidad actual** (ADR-020). Por ahora solo se repitió la de 600 pedidos/s: p95 de 18 ms con los logs en MySQL.
- Medir la espera del pool de conexiones, si la hipótesis 2 hiciera falta.

## Cómo repetirlo

```bash
docker compose up -d --build
bash loadtest/pico_tienda.sh                      # 50 a 1200 pedidos por segundo, unos 9 minutos
TASAS="600 800" bash loadtest/pico_tienda.sh      # solo algunas tasas
```

Para comparar con los logs en MySQL apagados, `LOGS_MYSQL_ACTIVO=false docker compose up -d catalogo inventario ventas usuarios` (ADR-020). Para una réplica, `docker compose up -d --no-recreate --scale catalogo=1 --scale inventario=1 catalogo inventario`.

---

**Si la decisión cambia:** no se borra este archivo. Se marca como *Reemplazada por ADR-YYY* y el nuevo ADR indica a cuál reemplaza y por qué hubo que revisarla.
