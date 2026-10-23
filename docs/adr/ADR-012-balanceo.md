# ADR-012 · Balanceo de carga

| | |
|---|---|
| **Estado** | Aceptada (versión inicial) · Entrega 2 |
| **Fecha** | 23/10/2026 |
| **Decisión de la consigna** | D12 · Balanceo de carga |
| **Relacionadas** | [ADR-005](ADR-005-comunicacion.md) (timeouts y reintentos), ADR-010 (resiliencia), [ADR-011](ADR-011-observabilidad.md) (métricas por réplica), ADR-013 (pruebas de carga) |

> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar.

## Contexto

La consigna pide al menos un servicio con más de una réplica y un mecanismo que reparta los pedidos entre ellas. ARCHITECTURE proponía replicar Catálogo por ser el más leído, pero en la Entrega 2 Catálogo es un mock. **Inventario es el servicio real**, así que se replica Inventario. Además es el que recibe las operaciones concurrentes sobre el stock (reservas), lo que pone a prueba que las réplicas no se pisen.

Ya existe un API Gateway con nginx (autenticación por clave, límite por grupo, correlation id). Inventario no guarda estado en memoria: todo vive en MySQL.

## Criterios

1. **Si una réplica cae, los pedidos siguen** sin intervención manual.
2. **No sumar piezas** si la que ya existe resuelve el problema.
3. **No reintentar lo que no es seguro reintentar** (ADR-005).
4. **Se puede demostrar** el reparto con evidencia (logs y métricas).
5. Una réplica nueva o reiniciada **entra sola** al balanceo.

## Decisión

### 1. Mecanismo

**nginx, el mismo gateway, balancea entre las réplicas de Inventario.** Docker Compose levanta dos réplicas (`deploy.replicas: 2`) bajo el mismo nombre `inventario`. nginx resuelve ese nombre con el DNS de Docker y reparte entre todas las IPs.

| Aspecto | Valor |
|---|---|
| Réplicas de Inventario | 2 |
| Algoritmo | Round robin (por turnos) |
| Descubrimiento | DNS de Docker (`resolver 127.0.0.11 valid=5s` y `resolve` en el upstream): una réplica con IP nueva entra sin reiniciar nginx |
| Detección de fallas | Pasiva: 2 fallas en 10 s sacan a la réplica del balanceo durante 10 s (`max_fails=2 fail_timeout=10s`) |
| Reintento en otra réplica | Si la réplica no acepta la conexión, no responde o devuelve 502. Máximo 2 intentos en total (`proxy_next_upstream_tries 2`) |
| Timeouts | Conexión 1 s, respuesta 5 s (ADR-005). Inventario corta cada pedido a los 4 s |
| Conexiones | `keepalive 16` hacia las réplicas |

### 2. Qué no se reintenta

- **Un POST que ya llegó a una réplica** y no respondió a tiempo: nginx no lo reenvía porque no sabe si se aplicó. Lo reintenta quien llamó, con su clave de idempotencia (ADR-005).
- **El 503:** Inventario lo usa para decir que el gimnasio no está disponible. Si contara como falla de la réplica, nginx sacaría del balanceo a réplicas sanas por un problema que no es de ellas.

### 3. Tráfico interno también balanceado

El mock de Ventas llama a Inventario por el gateway (puerto interno 8081, sin publicar), no directo a una réplica. Así las reservas de la capacidad publicada también se reparten y se benefician de la detección de fallas.

### 4. Réplicas sin estado compartido en memoria

- Todo el estado está en MySQL. No hace falta afinidad de sesión (sticky sessions).
- Los procesos periódicos (vencer reservas, enviar reposiciones) corren en las dos réplicas. La base evita que procesen lo mismo: bloqueo de la fila de la reserva (`FOR UPDATE`) y `FOR UPDATE SKIP LOCKED` en las reposiciones.

## Alternativas consideradas

| Alternativa | Por qué se descartó |
|---|---|
| **Solo el DNS de Docker** (que cada cliente resuelva `inventario` y elija) | No detecta réplicas caídas ni reintenta en otra; los clientes suelen quedarse con la primera IP que resolvieron. |
| **Traefik** | Descubre réplicas por etiquetas de Docker y tiene chequeos activos, pero es una pieza más para operar cuando nginx ya es el gateway. |
| **HAProxy** | Tiene chequeos activos de salud en la versión libre, pero obliga a sumar un proxy delante o detrás del gateway. |
| **Balanceo del lado del cliente** (Ventas elige la réplica) | Cada servicio que llama a Inventario tendría que implementarlo. |
| **Least connections en vez de round robin** | Con pedidos cortos y parecidos, como los de Inventario, no hay diferencia práctica. Round robin es más fácil de verificar. |
| **Replicar Catálogo** (como proponía ARCHITECTURE) | En esta entrega Catálogo es un mock: replicarlo no demuestra nada sobre un servicio real. Se puede replicar también cuando exista. |

## Consecuencias

**Positivas**

- Si una réplica cae, la otra sigue atendiendo. Los pedidos que no llegaron a conectarse se reenvían solos.
- No hay piezas nuevas: el balanceo vive en el gateway que ya existía.
- El reparto se ve en el log del gateway (`upstream`) y en Prometheus (`instance`, ADR-011).
- Escalar a más réplicas es cambiar un número en `docker-compose.yml`.

**Negativas aceptadas**

- **Detección solo pasiva:** nginx libre no tiene chequeos activos. Una réplica caída se descubre cuando falla un pedido real; ese pedido se reintenta en la otra si el error fue de conexión, pero un POST que se cortó a mitad de camino devuelve error.
- **El gateway es un solo contenedor:** si nginx cae, no entra ningún pedido. Se acepta para el entorno local **[?]** y se revisa en el despliegue (ADR-013).
- **Las dos réplicas comparten MySQL:** replicar Inventario no escala la base, que sigue siendo un punto único.
- **Round robin no mira la carga** de cada réplica: si una se pone lenta, sigue recibiendo la mitad de los pedidos hasta que falle.

## Pendientes

- **Chequeo activo** de `/readyz` **[?]**: con `healthcheck` en Docker Compose o con otra herramienta, si las pruebas de caída muestran que el chequeo pasivo no alcanza (ADR-010).
- **Replicar Catálogo** cuando deje de ser un mock.
- **Cantidad de réplicas** según las pruebas de carga (ADR-013).
- **Gateway redundante** en el despliegue con URL pública **[?]**.

## Validación prevista

- **Entrega 2:** con varios pedidos seguidos, el log del gateway (`upstream`) y `http_pedidos_total` por `instance` muestran el reparto entre las dos réplicas.
- **Entrega 2:** deteniendo una réplica (`docker stop` sobre uno de los contenedores de `inventario`), los pedidos siguen respondiendo y la réplica caída deja de recibir tráfico. Al levantarla, vuelve a recibir.
- **Entrega 2:** dos reservas concurrentes sobre la misma variante, atendidas por réplicas distintas, no apartan más stock del que hay.
- **Pruebas de carga (ADR-013):** latencia y errores con una y con dos réplicas.

---

**Si la decisión cambia:** no se borra este archivo. Se marca como *Reemplazada por ADR-YYY* y el nuevo ADR indica a cuál reemplaza y por qué hubo que revisarla.
