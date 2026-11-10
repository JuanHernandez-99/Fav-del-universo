# ADR-013 · Capacidad, escalado y costos

|                             |                                                                                                                                                         |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Estado**                  | **Aceptada con mediciones** (10/11/2026). Propuesta en la Entrega 2; ver "Cambios respecto de la propuesta". Falta confirmar el tamaño de la VM de Azure para cerrar los costos. |
| **Fecha**                   | 23/10/2026, actualizada el 10/11/2026                                                                                                                   |
| **Decisión de la consigna** | D13 · Capacidad y costos                                                                                                                                |
| **Relacionadas**            | [ADR-004](ADR-004-consistencia-y-concurrencia.md) (contención), [ADR-006](ADR-006-busqueda.md) (búsqueda), [ADR-011](ADR-011-observabilidad.md) (objetivo de servicio, Jaeger), [ADR-012](ADR-012-balanceo.md) (réplicas) |

> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar · **[completar]** dato que falta.

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

### Escalones con dos réplicas de Catálogo y de Inventario

| Pedidos/s pedidos | Logrados | Errores | p95 total | p95 compra | p95 búsqueda | Esperas por bloqueo | Corrida |
|---|---|---|---|---|---|---|---|
| 50 | 52 | 0 % | 24 ms | 45 ms | 34 ms | — | 1 |
| 100 | 100 | 0 % | 23 ms | 48 ms | 22 ms | — | 1 |
| 200 | 199 | 0 % | 22 ms | 47 ms | 17 ms | — | 1 |
| 400 | 388 | 0 % | 35 ms | 250 ms | 39 ms | — | 1 |
| 600 | 596 | 0 % | 33 ms | 171 ms | 28 ms | 91 | Jaeger recién reiniciado |
| 800 | 793 | 0 % | 65 ms | 262 ms | 77 ms | 316 | Jaeger recién reiniciado |
| 1200 | 1020 | **13 %** | **2,9 s** | 5,0 s | 2,8 s | 2233 | sin Jaeger |
| 1600 | 1485 | 0,8 % | **1,3 s** | 4,6 s | 0,9 s | no se pudo medir | sin Jaeger |
| 2000 | 1115 | **7,3 %** | **4,2 s** | 5,3 s | 3,1 s | 2053 | sin Jaeger |

- **El objetivo se cumple hasta 800 pedidos por segundo** y se rompe antes de 1200.
- Entre 1200 y 2000, los errores fueron `503` del gateway (ninguna réplica respondió a tiempo) y pedidos que k6 abandonó (`499` en el log del gateway). A 1200, la búsqueda fue la que más falló: 72 % de error, con Elasticsearch usando 2,7 núcleos.
- A 1600 y 2000 el resultado no es parejo (menos errores a 1600 que a 1200). Pasado el límite, la máquina entera está saturada, k6 incluido, y los números ya no comparan bien.
- La **compra** es la operación que primero se acerca al objetivo: 262 ms de p95 a 800 pedidos/s. Pasa por tres servicios, dos veces por el gateway y dos transacciones.

### Efecto de Jaeger (dos réplicas)

| Jaeger | 600 pedidos/s: p95 · compra · errores | 800 pedidos/s: p95 · compra · logrados | Memoria de Jaeger |
|---|---|---|---|
| Con trazas acumuladas de las corridas anteriores | **527 ms** · 2,4 s · 0,15 % | **473 ms** · 2,0 s · **289 de 800** | 2,7 → 3,7 → **4,3 GB** |
| Recién reiniciado | 33 ms · 171 ms · 0 % | 65 ms · 262 ms · 793 | 0,4 → 1,2 GB |
| Apagado | 55 ms · 307 ms · 0 % | 49 ms · 263 ms · 793 | — |

Trazar cuesta poco: con Jaeger vacío, los resultados son parecidos a tenerlo apagado. **Lo que rompe el sistema es la memoria:** Jaeger all-in-one guarda las trazas en memoria y sin límite (`--memory.max-traces` no está configurado), a razón de unos **18 KB por pedido**. Con 4,3 GB de los 7,6 GB de Docker, al resto le queda poca memoria (Elasticsearch bajó de 1,1 a 0,7 GB) y todo el sistema se pone lento.

### Una réplica o dos (sin Jaeger)

| Réplicas de Catálogo e Inventario | 600 pedidos/s: p95 · compra | 800 pedidos/s: p95 · compra |
|---|---|---|
| 2 | 55 ms · 307 ms | 49 ms · 263 ms |
| 1 | 27 ms · 119 ms | 40 ms · 189 ms |

**En una sola máquina, una segunda réplica no suma capacidad:** las dos comparten los mismos núcleos, y un proceso Go ya usa varios (Catálogo llegó a 1,5 núcleos con una réplica). Las réplicas dan disponibilidad ante la caída de una (ADR-012), no rendimiento.

### Contención sobre la variante más pedida

| | 600 pedidos/s | 800 pedidos/s |
|---|---|---|
| p95 de la compra: variante popular / otras (dos réplicas, sin Jaeger) | 318 / 291 ms | 305 / 221 ms |
| p95 de la compra: variante popular / otras (una réplica, sin Jaeger) | 128 / 105 ms | 208 / 172 ms |
| Esperas por bloqueo en MySQL de Inventario (Jaeger recién reiniciado) | 91 | 316 |

Comprar la variante popular es entre 10 y 40 % más lento, y las esperas por bloqueo crecen más rápido que la tasa. A 1200 pedidos/s fueron más de 2000 en un minuto.

### Hipótesis de la propuesta

| # | Hipótesis | Resultado |
|---|---|---|
| 1 | Contención sobre una fila de stock | **Confirmada, pero no es el primer límite.** Crece con la tasa y se nota en la variante popular; hasta 800 pedidos/s la compra sigue dentro del objetivo. |
| 2 | Pool de conexiones de Inventario | **Sin evidencia:** no hay una métrica de espera del pool. MySQL de Inventario no pasó de medio núcleo. |
| 3 | Una sola base MySQL | **Descartada en este rango:** MySQL de Inventario usó como mucho medio núcleo hasta 800 pedidos/s. |
| 4 | Límite por grupo del gateway | **No aplica:** las rutas de la tienda no tienen límite por grupo. |
| — | Memoria de Jaeger (no estaba prevista) | **Confirmada:** es lo primero que degrada el sistema con carga sostenida. |

## Principal límite

1. **Memoria de Jaeger, con la configuración actual.** Crece sin techo con cada pedido trazado. A 800 pedidos/s suma 1 GB cada pocos minutos; con tráfico real crece más lento, pero no para. A un pedido por segundo serían unos 1,5 GB por día. **Pasa lo mismo en la nube:** `docker-compose.cloud.yml` usa la misma configuración de Jaeger.
2. **CPU de la máquina, entre 800 y 1200 pedidos/s.** Todo comparte los mismos núcleos (los contenedores llegaron a 12 de 16, más k6). Elasticsearch es el que más CPU pide al saturarse, y la búsqueda es la primera en fallar. La compra es la operación más cercana al objetivo.

Para FAV, el límite de cómputo no es el problema: 800 pedidos/s con 5 % de compras son 40 compras por segundo, contra 10 a 20 prendas por semana. **El límite que importa en producción es la memoria.**

## Cómo escalar

| Opción | Cuándo | Evidencia |
|---|---|---|
| **Limitar las trazas guardadas** (`--memory.max-traces`) o muestrear | Ya: es el primer límite | Tabla "Efecto de Jaeger" |
| **VM más grande** (escalado vertical) | Si la CPU fuera el límite con tráfico real | En una máquina, más réplicas no suman (tabla "Una réplica o dos") |
| **Elasticsearch y las bases en otra VM o como servicio administrado** | Si una VM grande no alcanzara | Elasticsearch es el primero en saturarse |
| **Más réplicas en otras máquinas** | Recién con más de una VM | — |
| **Repartir el stock de una variante muy pedida** **[?]** | Solo si la contención pasara a ser el límite | Hoy no lo es (hipótesis 1) |
| Cachear la disponibilidad | **No:** el stock tiene que ser exacto (ADR-007) | — |

## Costos

**Supuestos:** una sola VM Linux con todo el Docker Compose, como el despliegue actual (`desplegar/README.md`), en la región **Chile Central** de Azure, con precio por uso (pay-as-you-go) y 730 horas por mes. Los precios salen de la [API pública de precios de Azure](https://prices.azure.com/api/retail/prices), consultada el 10/11/2026, en dólares y sin impuestos.

**Tamaño mínimo por memoria:** sin carga, el sistema usa **3,5 GB** (medido con `docker stats`: Elasticsearch 0,95 GB, Jaeger 0,9 GB, los tres MySQL 0,7 GB, MongoDB 0,2 GB, el resto 0,7 GB). Una VM de 4 GB queda sin margen; la de 8 GB es la mínima razonable.

| Componente | Precio | Costo mensual |
|---|---|---|
| VM **B2ms** (2 vCPU, 8 GB) | USD 0,116 por hora | USD 84,68 |
| Disco del sistema Standard SSD E4 (32 GB) | USD 3,36 por mes | USD 3,36 |
| IP pública estática estándar | USD 0,005 por hora | USD 3,65 |
| Transferencia de salida | Primeros 100 GB del mes sin cargo | USD 0 con el volumen de FAV **[supuesto]** |
| **Total** | | **USD 91,69 por mes** |

| Otros tamaños | Precio por hora | VM por mes | Por qué no |
|---|---|---|---|
| B2s (2 vCPU, 4 GB) | USD 0,0582 | USD 42,49 | Menos memoria que la que usa el sistema en reposo |
| D2s v5 (2 vCPU, 8 GB) | USD 0,134 | USD 97,82 | Misma memoria que B2ms, más cara. Conviene solo con CPU alta todo el tiempo |
| B4ms (4 vCPU, 16 GB) | USD 0,233 | USD 170,09 | El doble, sin una carga que lo justifique |

- **Uso bajo y pico cuestan lo mismo:** con 10 a 20 prendas por semana, diciembre no obliga a cambiar de VM.
- **Desasignar la VM** cuando no se usa deja de cobrar el cómputo; el disco y la IP siguen cobrando (`desplegar/README.md`).
- **[completar]** El tamaño real de la VM que creó el grupo. Si fuera una B2s, corre riesgo de quedarse sin memoria, más con Jaeger sin límite.
- **No medido:** la capacidad en la VM de 2 vCPU. Va a ser bastante menor que la de la notebook, pero con el volumen del cliente el margen sigue siendo muy grande.

## Cambios respecto de la propuesta

| Propuesta (23/10) | Ahora | Por qué |
|---|---|---|
| Solo Inventario: 70 % consultar stock, 30 % reservar | Pico de la tienda con los cuatro servicios | En la Entrega 2 Inventario era el único servicio real; ahora se puede probar el flujo del comprador (RNF-06) |
| Contención como primer cuello candidato | Memoria de Jaeger, después CPU | Lo midió la prueba |
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
- La prueba encontró un riesgo real de producción (la memoria de Jaeger) antes de que pase en la nube.

**Negativas aceptadas**

- **Los números son de una notebook,** con k6 en la misma máquina. Sirven para comparar, no para prometer capacidad en Azure.
- **La mezcla de operaciones es supuesta:** el cliente no tiene datos de navegación.
- **Cada corrida deja datos:** compras de prueba en Ventas y un millón de unidades por variante en Inventario. En local se borran con `docker compose down -v`.

## Pendientes

- **Decidir cómo limitar Jaeger** (local y nube) y repetir la corrida de 800 pedidos/s con el límite.
- **[completar]** Confirmar el tamaño de la VM de Azure y ajustar la tabla de costos.
- Medir la espera del pool de conexiones, si la hipótesis 2 hiciera falta.

## Cómo repetirlo

```bash
docker compose up -d --build
bash loadtest/pico_tienda.sh                      # 50 a 1200 pedidos por segundo, unos 9 minutos
TASAS="600 800" bash loadtest/pico_tienda.sh      # solo algunas tasas
```

Para comparar sin Jaeger, `docker compose stop jaeger` antes de correr. Para una réplica, `docker compose up -d --no-recreate --scale catalogo=1 --scale inventario=1 catalogo inventario`.

---

**Si la decisión cambia:** no se borra este archivo. Se marca como *Reemplazada por ADR-YYY* y el nuevo ADR indica a cuál reemplaza y por qué hubo que revisarla.
