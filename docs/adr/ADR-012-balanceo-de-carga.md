# ADR-012 · Balanceo de carga y gateway

|                             |                                                                                                                                                                  |
| --------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Estado**                  | Aceptada · Entrega 2. Implementada para Inventario (2 réplicas) y para la capacidad publicada (mock).                                                            |
| **Fecha**                   | 23/10/2026                                                                                                                                                       |
| **Decisión de la consigna** | D12 · Balanceo de carga                                                                                                                                          |
| **Relacionadas**            | [ADR-005](ADR-005-comunicacion.md) (timeouts y correlación), [ADR-010](ADR-010-resiliencia.md) (fallas), [ADR-011](ADR-011-observabilidad.md) (evidencia), [ADR-013](ADR-013-capacidad-y-costos.md) (capacidad) |

> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar · _previsto_: forma parte del diseño pero todavía no está implementado.

## Contexto

La consigna pide que al menos un servicio corra con **varias instancias detrás de un balanceador** y que se justifique el algoritmo, la verificación de disponibilidad y qué pasa cuando una instancia cae. El contrato además exige un punto de entrada único con autenticación por grupo y un límite de pedidos (`429`).

Este ADR también cierra el pendiente de ARCHITECTURE sobre **qué tecnología usar como gateway**.

El servicio que se replica es Inventario: no guarda estado en memoria que importe entre pedidos (el estado está en MySQL), así que cualquier réplica puede atender cualquier pedido. Las carreras entre réplicas las resuelve la base (ADR-004).

## Criterios

1. **Ninguna réplica es especial:** el reparto no necesita afinidad de sesión.
2. **Una réplica caída no debe verse desde afuera** (o lo menos posible).
3. **Una réplica nueva o reiniciada entra sola**, aunque cambie de IP.
4. **Cumplir el contrato:** clave por grupo, límite de pedidos, correlación.
5. **Poco que operar** para un equipo de dos personas.

## Decisión

### Tecnología: nginx como gateway y balanceador

Una sola pieza hace de **API Gateway** (entrada pública, autenticación, límite, correlación) y de **balanceador** (reparto entre réplicas). Se usa nginx 1.28 porque cubre todo lo necesario con un archivo de configuración y una imagen oficial.

### Réplicas

Inventario se levanta con `deploy.replicas: 2` en Docker Compose. Todas comparten el nombre `inventario` en la red de Docker, y el DNS interno devuelve una IP por réplica.

### Algoritmo: round robin

Se reparte por turnos entre las IPs del nombre. Se justifica porque las réplicas son **idénticas** y los pedidos tienen un costo **parecido** (transacciones cortas contra la misma base). Con costos muy distintos por pedido convendría `least_conn`.

**Supuesto a validar:** que el costo por pedido sea parejo. Si la prueba de carga (ADR-013) muestra réplicas desparejas, se pasa a `least_conn`.

### Verificación de disponibilidad (pasiva)

| Parámetro                              | Valor                           | Efecto                                                                                           |
| -------------------------------------- | ------------------------------- | ------------------------------------------------------------------------------------------------ |
| `max_fails` / `fail_timeout`           | 2 fallas en 10 s                | La réplica sale del reparto 10 s y se vuelve a probar                                            |
| `proxy_next_upstream`                  | `error timeout http_502`        | Ante esas fallas, el pedido se prueba en la otra réplica                                         |
| `proxy_next_upstream_tries`            | 2                               | Una sola segunda oportunidad                                                                     |
| `proxy_connect_timeout`                | 1 s                             | Una réplica que no acepta la conexión se descubre rápido                                         |
| `proxy_read_timeout`                   | 5 s                             | Techo para que responda la réplica                                                               |
| `resolve` + `resolver … valid=5s`      | cada 5 s                        | Una réplica nueva o reiniciada (con otra IP) entra sin reiniciar nginx                           |
| `keepalive`                            | 16 conexiones                   | Reutiliza conexiones con las réplicas                                                            |

Decisiones puntuales:

- **El `503` no cuenta como falla.** Inventario lo usa para decir "una dependencia (el gimnasio) no está disponible". Contarlo sacaba del reparto a réplicas sanas.
- **Un `POST` que ya llegó a una réplica no se reenvía.** nginx solo lo reintenta si no pudo conectarse. Evita duplicar una operación; si igual se reenviara, la clave de idempotencia lo cubre (ADR-004).
- **Cada réplica expone `/healthz` y `/readyz`, pero nginx de código abierto no hace verificación activa**: se entera de que una réplica cayó cuando un pedido real falla.

### Entradas

| Entrada                      | Quién la usa                         | Controles                                                                                          |
| ---------------------------- | ------------------------------------ | -------------------------------------------------------------------------------------------------- |
| `:8080` (pública)            | Consumidores y panel                 | `X-API-Key` por grupo y límite de 60 pedidos por minuto con ráfaga de 20 (`429`); `X-Admin-Key` para `/inventario/` |
| `:8081` (solo red de Docker) | Ventas hacia Inventario              | Sin puerto publicado. Las llamadas internas también se reparten entre réplicas                     |

En `/inventario/`, el gateway **fija** `X-User-Id` y `X-User-Role` hacia Inventario, así el cliente no puede declarar su propia identidad. Devuelve `X-Correlation-Id` también en sus errores.

### Qué pasa cuando una réplica cae

1. Los pedidos que le tocaban fallan al conectar (hasta 1 s) y nginx los prueba en la otra réplica.
2. A las 2 fallas, la réplica sale del reparto durante 10 s. Después se prueba de nuevo.
3. Si Docker la reinicia con otra IP, entra al reparto en un máximo de 5 s.
4. Las reservas que tenía en curso no se pierden: están en MySQL, y los procesos de vencimiento y reposición los retoma la otra réplica (`SKIP LOCKED`).

## Alternativas consideradas

| Alternativa                              | Por qué se descartó                                                                                                                                                  |
| ---------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Traefik**                              | Descubre contenedores solo y tiene verificación activa, pero agrega otra tecnología con su propio modelo de configuración. nginx alcanza para dos réplicas.         |
| **HAProxy**                              | Excelente balanceador con verificación activa, pero no resuelve autenticación ni límites por clave sin más piezas.                                                  |
| **Envoy**                                | Más capacidades de las que se necesitan (reintentos con presupuesto, detección de valores atípicos) a costa de una configuración mucho más pesada.                |
| **DNS round robin sin gateway**          | Los clientes cachean las IPs y no se retiran réplicas caídas. No resuelve autenticación ni límites.                                                                 |
| **`least_conn`**                         | No hay evidencia de pedidos desparejos. Se reevalúa con la prueba de carga.                                                                                         |
| **Afinidad de sesión**                   | Las réplicas no guardan estado de sesión; la afinidad quitaría reparto sin aportar nada.                                                                            |
| **Un balanceador por servicio**          | Duplica configuración. Un solo gateway alcanza para el volumen de esta entrega.                                                                                     |

## Consecuencias

**Positivas**

- Cualquier réplica atiende cualquier pedido y se puede subir o bajar su número sin cambiar código.
- La caída de una réplica es casi invisible para el consumidor.
- El reparto se puede **demostrar** (ver Validación).

**Negativas aceptadas**

- **El gateway es una única instancia:** es un punto único de falla. Es un anti-patrón conocido; aceptado en esta entrega y a documentar en la presentación. Mitigación posible: dos instancias detrás de otra capa, o un balanceador del proveedor en producción.
- **Verificación solo pasiva:** el primer pedido que cae sobre una réplica caída paga hasta 1 s de espera de conexión.
- **Validación del token en el gateway:** nginx de código abierto no valida JWT por sí solo. Hoy el panel usa una clave provisoria (`X-Admin-Key`); la validación real del JWT de Usuarios necesita `auth_request` o un módulo, y queda pendiente.
- **El límite de pedidos es por instancia de nginx,** no distribuido: no se comparte si hay más de un gateway.
- **Los reintentos del gateway se suman a los del mock de Ventas** (ADR-010).
- **Una réplica lenta, pero no caída, sigue recibiendo pedidos** (round robin no mira la carga).

## Pendientes

- **Validación del JWT** de Usuarios en el gateway (hoy `X-Admin-Key`).
- **Segunda instancia del gateway** o un plan documentado para producción.
- **Prueba de carga** para validar el round robin (ADR-013).
- **[?]** Verificación activa si se cambia de tecnología.

## Validación

- **Reparto:** `for i in $(seq 1 20); do curl -s -o /dev/null "localhost:8080/inventario/v1/stock?variantes=var-remera-basica-blanca-m" -H "X-Admin-Key: clave-admin-local"; done` y mirar `docker compose logs gateway`: el campo `upstream` alterna las dos IPs. En Prometheus, `sum by (instance) (http_pedidos_total)` crece en ambas.
- **Caída de una réplica:** `docker stop <contenedor de una réplica>` y repetir los pedidos: siguen respondiendo y todo va a la otra. `docker start` y, en unos segundos, vuelve al reparto.
- **Límite:** más de 25 pedidos seguidos en pocos segundos a `/v1/productos` con la misma `X-API-Key` devuelven `429` (ráfaga de 20 y luego 1 por segundo).
- **Autenticación:** sin `X-API-Key` o sin `X-Admin-Key`, `401` con Problem Details y `X-Correlation-Id`.
- **Pendiente de medir:** reparto con carga sostenida y tiempo real de recuperación (se registra en `docs/POSTMORTEM.md`).
