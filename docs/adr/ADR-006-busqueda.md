# ADR-006 · Búsqueda

|                             |                                                                                                                                                              |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Estado**                  | **Aceptada e implementada** (08/11/2026). Propuesta en la Entrega 2; ver "Cambios respecto de la propuesta".                                                  |
| **Fecha**                   | 23/10/2026, actualizada el 08/11/2026                                                                                                                         |
| **Decisión de la consigna** | D6 · Motor de búsqueda                                                                                                                                       |
| **Relacionadas**            | [ADR-001](ADR-001-limites-de-los-servicios.md) (límites), [ADR-003](ADR-003-persistencia.md) (Mongo), [ADR-005](ADR-005-comunicacion.md) (eventos), [ADR-007](ADR-007-cache.md) (caché) |

> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar.

## Contexto

La consigna pide una búsqueda mediante un motor, con paginación, filtros y orden, un índice sincronizado y un retraso máximo definido. También pide justificar qué se indexa y cómo se actualiza o reconstruye. El apunte de motores de búsqueda plantea que el índice es un **modelo de lectura derivado**: ayuda a descubrir, pero **el servicio dueño del dato valida**.

El catálogo permite buscar por texto ("remera blanca"), filtrar por categoría, talle, color y precio, y ordenar. Una búsqueda con tolerancia a errores de tipeo y orden por relevancia no se resuelve bien con las consultas por campo de Mongo.

## Criterios

1. **Relevancia:** ordenar por qué tan bien coincide la consulta, no solo filtrar.
2. **Tolerancia** a errores de escritura y a acentos.
3. **Que el catálogo siga funcionando si el buscador cae.**
4. **Frescura acotada y medible** entre editar un producto y encontrarlo.
5. **Costo operativo bajo** para un equipo de dos personas.

## Decisión

### Motor

**Elasticsearch 8** en un contenedor de un solo nodo. Da búsqueda por texto con relevancia (BM25) y filtros, y se usa por HTTP con JSON, sin un cliente específico. La consigna pide un motor sin fijar cuál; ver la comparación con Solr en "Alternativas consideradas".

### Qué se indexa

**Un documento por producto publicado.** Catálogo habla con el motor solo desde `internal/busqueda`.

| Campo                        | Uso                                                 |
| ---------------------------- | --------------------------------------------------- |
| `nombre`, `descripcion`, `colores` | Texto buscable, con analizador en español: sin acentos ni mayúsculas, y por raíz ("remeras" encuentra "remera") |
| `categoria`, `variantes` (`talle`, `color`) | Filtros, que no puntúan. `variantes` es `nested`: talle y color tienen que coincidir en la misma variante |
| `precio`                     | Precio base en centavos, para orden y filtro por rango. La promoción se aplica al mostrar |
| `creadoEn`, `nombre.orden`, `productoId` | Orden y desempate |

**No se indexa la disponibilidad.** Cambia con cada reserva y se desactualizaría al instante. La tienda la consulta a Inventario al mostrar el resultado (ver Consecuencias).

**El índice dice qué productos y en qué orden; los datos salen de Mongo.** Catálogo pide los ids al motor y lee esos productos de su base. Así, el precio vigente siempre es el real y un producto despublicado que siga en el índice no se muestra.

### Cómo se mantiene

```
Admin edita → Mongo: version + 1 → publicador → RabbitMQ: ProductoCambiado → indexador → lee Mongo → Elasticsearch
```

1. **Outbox dentro del documento.** Cada producto guarda `version` y `publicadoVersion`. Un producto está pendiente de avisar si `version` es mayor. Como escribir un documento es atómico en Mongo, no hace falta una transacción ni un replica set.
2. **Publicador.** Cada 1 s busca los pendientes y publica `ProductoCambiado` en el exchange `catalogo.eventos`, con confirmación del broker. Recién entonces marca la versión. Si se cae en el medio, el evento se publica de nuevo (al menos una vez).
3. **Indexador.** Consume de `catalogo.indexador`, **lee el estado actual del producto en Mongo** y escribe con **versionado externo**: solo aplica si la versión es mayor a la guardada. Un evento repetido o fuera de orden no pisa un dato más nuevo (idempotente).
4. **Despublicar** quita el documento.
5. **Dos réplicas:** las dos publican y consumen. Si hacen lo mismo no hay daño, por el punto 3.

### Consultas

Solo la búsqueda **con texto** usa el motor (`GET /v1/tienda/productos?q=...`). Navegar sin texto, y el listado del panel, salen de Mongo.

- **Texto:** todas las palabras tienen que aparecer, entre el nombre, la descripción y los colores, con tolerancia a errores de tipeo (`fuzziness: AUTO`). Una coincidencia exacta en el nombre puntúa más.
- **Filtros:** categoría, talle, color y rango de precio.
- **Orden:** relevancia (por defecto), novedad, precio y nombre. Siempre se desempata por `productoId`, para que un resultado no se repita entre páginas.
- **Paginación:** por número de página (`from` y `size`), hasta el resultado 10.000. Más allá responde `400`.

### Retraso máximo tolerable **[propuesta]**

**10 segundos** entre el cambio y su aparición en la búsqueda, con todo funcionando. Medido: hasta 1,9 s (ver Validación). Con una dependencia caída no aplica: el cambio queda pendiente y aparece cuando vuelve.

### Si el índice falla o está atrasado

| Situación                          | Comportamiento                                                                                                          |
| ---------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| Elasticsearch cae                  | La búsqueda por texto responde `503 BUSQUEDA_NO_DISPONIBLE`; **navegar, la ficha y comprar siguen**, porque leen de Mongo. Los eventos esperan en la cola de reintentos, sin ir a la de fallidos |
| RabbitMQ cae                       | Editar sigue funcionando. Los cambios quedan pendientes en cada producto y se avisan cuando vuelve. La búsqueda responde con el dato anterior |
| El índice está atrasado            | Se muestra un dato levemente viejo. El detalle sale siempre de Catálogo y el pedido se valida contra Catálogo e Inventario. |
| El índice pierde datos             | **Reconstrucción:** `POST /v1/admin/busqueda/reconstruir` crea un índice nuevo, lo carga desde Mongo y cambia un alias, sin cortar la búsqueda. Si el índice no existe al arrancar, se hace sola. |
| Un producto despublicado aparece   | No se muestra: lo filtra la lectura en Mongo. Al abrirlo, Catálogo responde `404`.                                       |
| Un mensaje no se puede procesar    | Tras 3 reintentos, a `catalogo.indexador.dlq`. Uno inválido va directo.                                                  |

**Sin `ELASTICSEARCH_URL`,** la búsqueda usa el índice de texto de Mongo (plan B). Sirve para un entorno sin el motor, como el despliegue de la capacidad, que no lo usa.

## Cambios respecto de la propuesta

| Propuesta | Implementado | Por qué |
|---|---|---|
| Outbox, sin decir dónde | Dentro del documento del producto | En una colección aparte haría falta una transacción y un replica set. Un solo documento ya es atómico |
| El evento lleva los datos | El evento solo avisa; se lee Mongo | Un evento viejo no puede revivir un producto despublicado |
| `search_after` | Número de página | La tienda muestra páginas numeradas, y `search_after` no permite saltar a una. El catálogo no llega al límite de 10.000 |
| `talles[]`, `colores[]` | `variantes` como `nested` | Con listas separadas, una prenda M blanca y L negra coincidiría con "M negro" |
| Decidir el motor tras una prueba | Elasticsearch | Cumple los criterios en las pruebas, y corre en las máquinas del equipo |

## Alternativas consideradas

| Alternativa                                         | Por qué se descartó                                                                                                                                         |
| --------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Índice de texto de Mongo**                        | Cero piezas nuevas, pero la relevancia y la tolerancia a errores son limitadas, y la consigna pide un motor. Queda como plan B sin `ELASTICSEARCH_URL`.     |
| **Comparar versiones sin RabbitMQ** (el indexador lee Mongo directo) | Más simple, y también cumple la consigna. Se prefirió respetar el diseño con eventos: desacopla la escritura del indexador y da reintentos y cola de fallidos. |
| **Outbox en otra colección, o change streams**      | Requieren un replica set, también en la nube. Sin ganancia para este caso.                                                                                   |
| **`LIKE` en MySQL**                                 | No ordena por relevancia, no tolera errores y no usa índices con comodín al principio. Además, el catálogo no está en MySQL.                                 |
| **Apache Solr** (el que se usó en clase)            | Ver "Por qué no Solr". |
| **Meilisearch o Typesense**                         | Más simples de operar y buenos para este tamaño. No se compararon con una prueba.                                                                            |
| **Búsqueda vectorial o híbrida**                    | Sin una necesidad observable. Aumenta el costo y la complejidad.                                                                                            |
| **Indexar la disponibilidad**                       | Se desactualiza con cada reserva y obliga a un flujo de eventos de muy alto volumen.                                                                        |
| **Consultar a Inventario por cada resultado**       | Multiplica la carga y acopla la búsqueda a su disponibilidad.                                                                                               |

### Por qué no Solr

Solr fue el motor usado en clase. Elasticsearch se definió en la Entrega 2 sin compararlo, así que se comparó el 09/11/2026 con `services/catalogo/pruebas/comparar_solr.mjs`: mismo catálogo (20 productos), mismo análisis de texto y mismas consultas.

**Los dos cumplen.** Pasaron los 16 casos: plural, acentos, errores de tipeo, todas las palabras, talle y color en la misma variante, orden, paginación, relevancia, reconstrucción sin corte, evento viejo y edición de variantes.

| | Elasticsearch | Solr |
|---|---|---|
| Error de tipeo | `fuzziness: AUTO`, incluido en la consulta | **Se arma aparte:** calcular la distancia en el cliente y agregar un campo sin raíz, porque un término difuso no pasa por el análisis |
| Evento viejo | `version_type=external` en cada pedido | Un procesador de actualización que hay que configurar |
| Reconstrucción sin corte | Cambio de alias | Intercambio de núcleos (`SWAP`) |
| Memoria en reposo, heap de 512 MB | 1 GiB | **745 MiB** |
| p95, 700 consultas seguidas | 10,2 ms | **8,1 ms** |

**Solr fue algo más liviano y rápido, y Elasticsearch, más directo de usar.** Con 20 productos y una sola computadora, esas diferencias de rendimiento no alcanzan para decidir.

**Por qué se mantiene Elasticsearch: resuelve de forma más directa los errores de tipeo, que son el criterio 2 de esta decisión desde la Entrega 2.** Un comprador que escribe "remra" tiene que encontrar la remera.

- **En Elasticsearch** alcanza con `fuzziness: AUTO` en la consulta. El término pasa por el mismo análisis que el índice, y la regla de distancia la aplica el motor.
- **En Solr** un término difuso no pasa por ese análisis, así que no coincide con la raíz guardada. En la comparación, la consulta difusa directa dio **0 resultados con "remra"**, y Elasticsearch, 5. Para igualarlo hizo falta calcular la distancia de cada palabra en el cliente y agregar un campo sin raíz al índice: más código y más datos que mantener alineados.

Además, Solr no agrega nada a lo que pide la consigna, y migrar obligaría a reescribir `internal/busqueda` y a repetir las pruebas de lo que ya está comprobado sobre este motor:

| Lo que pide la consigna | Cómo se cumple | Evidencia |
|---|---|---|
| Índice sincronizado, sin que un evento repetido o viejo pise un dato nuevo | Versionado externo (`version_type=external`) | `TestVersionadoExterno` y prueba por el gateway |
| Mecanismo de reconstrucción | Índice nuevo y cambio de alias, sin cortar la búsqueda | 87 consultas durante el cambio, ninguna falló |
| Filtros relevantes | `nested`: talle y color tienen que coincidir en la misma variante | `TestFiltros` |
| Retraso máximo definido | 10 s **[propuesta]** | Peor caso medido: 1,9 s |

**Si la cátedra lo exigiera, el cambio es acotado:** el servicio usa la interfaz `Indice`, y solo Catálogo depende del motor, en `internal/busqueda`.

**Costo aceptado:** unos 260 MiB más de memoria que Solr, que pesan en el despliegue (ADR-013).

**Sin probar con Solr:** carga real, retraso de indexación, y el flujo completo con RabbitMQ y el gateway.

## Consecuencias

**Positivas**

- Búsqueda con relevancia y tolerancia a errores sin sobrecargar a Mongo.
- Catálogo sigue operando si el buscador o RabbitMQ caen.
- El índice se puede reconstruir siempre desde Mongo: no es una fuente de verdad.

**Negativas aceptadas**

- **Una pieza más que operar:** unos 980 MiB de memoria en reposo.
- **Frescura eventual:** hay una ventana en la que el índice no refleja la edición.
- **Buscar lee también Mongo** o la caché (ADR-007). Si Mongo cae, lo que no esté en la caché no responde.
- **El total puede incluir uno de más** si un producto se despublicó y todavía no salió del índice.
- **Varios cambios seguidos pueden dar un solo evento,** con la última versión. Alcanza, porque el índice solo necesita el estado final.
- **Buscar pendientes recorre la colección** cada segundo y por réplica: compara dos campos del mismo documento, sin índice. Es aceptable para cientos o pocos miles de productos.
- **El filtro y el orden por precio usan el precio base,** no el que tiene la promoción aplicada (igual que el listado de Mongo).
- **Los resultados no traen la disponibilidad.** La tienda la pide aparte a Inventario, por lote (`GET /v1/disponibilidad`, hasta 200 ids): un pedido por página, no uno por resultado. No se puede filtrar "solo con stock" y, sin Inventario, se ven los productos sin saber si hay.
- **Recargar los datos de ejemplo** (`CATALOGO_RECARGAR=true`) puede bajar versiones: el índice queda desalineado hasta reconstruirlo.

## Pendientes

- **Métricas y alerta** de la cola de fallidos y del retraso (ADR-011). Hoy el retraso queda en el log `retraso_ms`.
- **Confirmar el retraso máximo** de 10 s con el cliente, si hace falta. **[propuesta]**

## Validación

Probado el 08/11/2026 en local, con `services/catalogo/pruebas/busqueda.sh` por el gateway y tests contra servicios reales. **Sin carga:** el retraso se midió con un solo administrador editando.

- **Retraso:** 22 mediciones entre publicar o editar y encontrar el producto. Peor caso: **1,9 s**.
- **Evento repetido y uno viejo:** el resultado no cambia. Lo cubre también `internal/busqueda/elastic_test.go`.
- **Mensaje inválido:** llegó a `catalogo.indexador.dlq`.
- **Elasticsearch apagado:** buscar responde `503`; navegar, la ficha y editar responden bien. El cambio hecho durante la caída apareció al volver y no fue a la cola de fallidos.
- **RabbitMQ apagado:** editar responde bien y la búsqueda sigue con el dato anterior. Al volver, el cambio apareció a los 13 s.
- **Reconstrucción:** 87 consultas durante el cambio de alias, ninguna falló. **La primera corrida falló en 13 de 102:** el alias cambiaba antes de que el índice nuevo fuera visible. Se corrigió con un `refresh` previo.
- **Despublicar:** salió de la búsqueda en menos de 2 s.
- **Tests contra servicios reales:** `internal/busqueda/elastic_test.go` (`ELASTICSEARCH_TEST_URL`) e `internal/repository/mongo_test.go` (`CATALOGO_TEST_MONGO_URI`).
