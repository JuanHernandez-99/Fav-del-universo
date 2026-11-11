# Postmortem · Ensayo de caídas controladas

| | |
|---|---|
| **Tipo** | Ensayo provocado, en local. La jornada de fallas controladas de la cátedra todavía no se hizo |
| **Fecha** | 09/11/2026: primera corrida de 04:34 a 04:50 UTC; [repetición tras corregir](#repetición-tras-corregir) de 04:58 a 05:11 UTC |
| **Evidencia** | [`services/inventario/pruebas/ensayo_caidas.sh`](../services/inventario/pruebas/ensayo_caidas.sh), tablero de Grafana y mails en Mailpit |
| **Decisiones relacionadas** | [ADR-010](adr/ADR-010-resiliencia.md), [ADR-011](adr/ADR-011-observabilidad.md), [ADR-012](adr/ADR-012-balanceo.md), [ADR-018](adr/ADR-018-consumo-del-gimnasio-contrato-real.md) |

## Resumen

Se apagaron, de a una, tres dependencias de Inventario con tráfico constante: el gimnasio, una réplica y MySQL. **Las fallas quedaron contenidas y no se detectó pérdida ni duplicación en lo comprobado.** La tienda respondió siempre, y el sistema se recuperó solo.

**El ensayo encontró un problema, ya corregido:** con MySQL caído, Inventario respondía `500 ERROR_INTERNO`, aunque el contrato reserva `503 NO_DISPONIBLE` para una dependencia caída, y Ventas no reintenta ante un `500`.

Las secciones que siguen, hasta "Causa", describen la **primera corrida**, antes de las correcciones.

## Impacto

Pedidos recibidos por cada ruta, a unos 2 por segundo cada una. Antes de las caídas, las cuatro rutas dieron 100 % de éxito, con un máximo de 0,03 s.

| Caída | Duración | Tienda | Disponibilidad | Reservar | Stock del gimnasio (panel) |
|---|---|---|---|---|---|
| **Gimnasio** | 180 s | 311 `200` | 311 `200` | 309 `201` | **234 `503`**, máx. 3,02 s |
| **Una réplica** | 90 s | 157 `200` | 155 `200`, máx. 1 s | 153 `201`, máx. 1,01 s | 157 `200` |
| **MySQL de Inventario** | 153 s | 266 `200` | **91 `500`**, máx. 2,3 s | **92 `500`**, máx. 2,22 s | 267 `200` |

- **Gimnasio:** solo se afectó lo que depende de él. Una reposición de 2 unidades quedó pendiente y se confirmó al volver, con el stock físico descontado una sola vez (de 55 a 53).
- **Réplica:** ningún error. Algunos pedidos tardaron hasta 1 s, frente a 0,03 s en situación normal.
- **MySQL:** no se pudo consultar stock ni reservar. Lo demás siguió respondiendo.

## Línea de tiempo (UTC)

| Hora | Hecho |
|---|---|
| 04:34:31 | Comienza el tráfico en situación normal |
| **04:35:32** | **Se apaga el gimnasio.** La reposición se acepta con `202` |
| 04:36:35 | Mail: `CircuitoGimnasioAbierto` |
| 04:37:25 | Mail: `ErroresDelServidor` |
| 04:38:33 | Vuelve el gimnasio |
| 04:39:35 | Mail: se resolvió `CircuitoGimnasioAbierto` |
| 04:41:25 | Mail: se resolvió `ErroresDelServidor` |
| 04:41:42 | La reposición queda `CONFIRMADA`, 190 s después de la vuelta |
| **04:43:13** | **Se apaga una réplica de Inventario** |
| 04:44:10 | Mail: `ReplicaCaidaInventario` |
| 04:44:43 | Vuelve la réplica |
| **04:45:46** | **Se apaga MySQL.** Reservar responde `500` |
| 04:46:10 | Mail: se resolvió `ReplicaCaidaInventario` |
| 04:47:25 | Mail: `ErroresDelServidor` |
| 04:48:19 | Vuelve MySQL |
| 04:48:22 | Inventario responde otra vez. La reserva que había fallado se crea al repetirla, y otra repetición devuelve la misma |
| 04:48:40 | Mail: `LatenciaAlta`, con el servicio ya recuperado |
| 04:50:25 | Mail: se resolvió `ErroresDelServidor` |
| 04:50:40 | Mail: se resolvió `LatenciaAlta` |

## Detección

| Caída | Primer aviso | Tiempo |
|---|---|---|
| Gimnasio | `CircuitoGimnasioAbierto` | 63 s |
| Una réplica | `ReplicaCaidaInventario` | 57 s |
| MySQL | `ErroresDelServidor` | 99 s |

**Las tres caídas se detectaron por alerta, sin mirar cada componente.** Los logs permitieron seguir el problema de MySQL: hay 194 líneas `error inesperado`, con su `correlation_id`, y muestran errores como `invalid connection`.

> **Nota (10/11/2026):** desde [ADR-020](adr/ADR-020-logs-y-track-id-modelo-catedra.md), el seguimiento se hace por track id (el `correlation_id`) en el tablero "FAV · Logs" de Grafana, que lee la tabla `app_logs`. Si este ensayo se repitiera, esas líneas de error se buscarían ahí. Con MySQL de Inventario caído, `mysql-logs` sigue funcionando, porque es otra base.

## Respuesta

Es un ensayo: las caídas y las vueltas las ejecutó el script, y **no se ensayó la respuesta de una persona** a los avisos. Una vez que volvió cada dependencia, nadie tuvo que intervenir:

- **Gimnasio:** el circuit breaker cortó las llamadas y el reintento persistido envió la reposición pendiente.
- **Réplica:** los pedidos siguieron respondiendo sin errores con la otra réplica (ADR-012). No se comprobó el reparto después de que volvió.
- **MySQL:** Inventario volvió a responder 4 s después de encender la base.

## Causa

Las caídas fueron provocadas. Lo que hay que explicar es cómo reaccionó el sistema:

| Observado | Causa | ¿Es un problema? |
|---|---|---|
| `500` con MySQL caído | `escribirError` solo traducía a `503` los vencimientos de tiempo. Un error de conexión con la base caía en el caso por defecto. Ventas trata el `500` como un rechazo y no reintenta | **Sí**, corregido: no cumplía el significado del contrato, y una caída breve de la base no se absorbía con un reintento |
| `ErroresDelServidor` con el gimnasio caído | Esa alerta contaba el `503 GIMNASIO_NO_DISPONIBLE`, aunque es una degradación prevista. Cerca de un cuarto del tráfico de Inventario en el ensayo iba a esa ruta | Ruido, corregido: dos avisos por una sola falla |
| `LatenciaAlta` con el servicio ya recuperado | La regla mira 2 minutos hacia atrás y requiere 2 minutos más: avisa con retraso | Aceptable: una falla breve puede avisar tarde |
| Reposición confirmada 190 s después de la vuelta | La espera entre reintentos se duplica hasta 5 minutos (ADR-010). Es más que los 7 s de una caída corta (ADR-018) | Aceptable: la entrega es segura, pero puede tardar hasta 5 minutos |

## Repetición tras corregir

Se repitieron las caídas del gimnasio y de MySQL. La de la réplica no, porque las correcciones no la afectan.

| Comprobación | Primera corrida | Repetición |
|---|---|---|
| Disponibilidad con MySQL caído | 91 `500` | **93 `503`** |
| Reservar con MySQL caído | 92 `500` | **93 `503`**, máx. 3 s |
| Pedido completo por Ventas con MySQL caído | No se probó | **`503 NO_DISPONIBLE`** en 3,3 s |
| Líneas `error inesperado` en los logs | 194 | **0** |
| `ErroresDelServidor` con el gimnasio caído | Se disparó | **No se disparó** |
| `ErroresDelServidor` con MySQL caído | Avisó a los 99 s | Avisó a los 110 s |
| `CircuitoGimnasioAbierto` | Avisó a los 63 s | Avisó a los 64 s |
| Inventario, después de encender MySQL | Responde a los 4 s | Responde a los 3 s |
| Reposición, después de volver el gimnasio | Confirmada a los 190 s | Confirmada a los 181 s, descontada una vez |

- **Reintento seguro:** el pedido y la reserva que fallaron se crearon al repetirlos con la misma clave. Otra repetición devolvió el mismo pedido y la misma reserva.
- **La tienda y el stock del gimnasio** siguieron respondiendo durante la caída de MySQL: 270 `200` cada uno, sin errores.
- **Situación normal de la repetición:** hubo 4 `503` en cada ruta de Inventario en los primeros segundos, a las 04:58:00–02. El ensayo arrancó con Inventario recién reiniciado por el despliegue, como muestra el log del gateway. Después de eso no hubo más errores en esa etapa.
- **Sigue igual:** `LatenciaAlta` avisó con el servicio ya recuperado (05:09:25).

## Acciones de mejora

| Acción | Estado |
|---|---|
| Responder `503 NO_DISPONIBLE` cuando no se puede usar la base, para que Ventas reintente | **Hecho** y comprobado en la repetición |
| No contar `GIMNASIO_NO_DISPONIBLE` en `ErroresDelServidor` | **Hecho** y comprobado en la repetición |
| Probar un pedido completo por Ventas durante la caída | **Hecho** con MySQL caído |
| Repetirlo con el gimnasio real, en la jornada de fallas controladas | **Pendiente:** falta su URL pública y su clave |
| Segunda instancia del gateway (ADR-012) | **Pendiente** |

## Qué no se probó

- **La caída del gimnasio real:** se usó el mock local.
- **Una compra desde la tienda durante la caída:** se probó el pedido de la capacidad publicada, no el flujo del comprador.
- **Las pantallas:** no se vieron en el navegador el mensaje del panel ni el de la tienda.
- **Un pico de carga:** fueron unos 2 pedidos por segundo por ruta, con todo en una sola computadora.
- **La integridad completa de los datos:** se comprobaron el stock físico de la reposición y la idempotencia de una reserva. No se auditó toda la base después de la caída de MySQL.
- **Otras dependencias en este ensayo:** Redis, RabbitMQ, Ventas y Usuarios se apagaron solo para probar sus alertas (ADR-011), sin medir el impacto en los usuarios.
