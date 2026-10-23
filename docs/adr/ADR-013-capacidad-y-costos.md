# ADR-013 · Capacidad, escalado y costos

|                             |                                                                                                                                                         |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Estado**                  | **Propuesta** · Entrega 2. Hay un guion de prueba de carga (`loadtest/`), **sin ejecutar**. No hay mediciones ni costos calculados.                      |
| **Fecha**                   | 23/10/2026                                                                                                                                              |
| **Decisión de la consigna** | D13 · Capacidad y costos                                                                                                                                |
| **Relacionadas**            | [ADR-004](ADR-004-consistencia-y-concurrencia.md) (contención), [ADR-007](ADR-007-cache.md) (caché), [ADR-011](ADR-011-observabilidad.md) (métricas), [ADR-012](ADR-012-balanceo-de-carga.md) (réplicas) |

> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar · **[completar]** dato a cargar con una medición o un precio real. Este ADR **no contiene números medidos**: los valores marcados son hipótesis.

## Contexto

La consigna pide estimar la capacidad del sistema, identificar el primer cuello de botella, justificar cómo escalar y estimar el costo de operación. Se evalúa con una prueba de carga y con un cálculo de costos sustentado.

## Criterios

1. **Medir antes de afirmar:** ninguna capacidad se declara sin una prueba.
2. **Encontrar el primer límite,** no solo cuánto aguanta.
3. **Escalar donde duele,** no todo por igual.
4. **Costos con supuestos explícitos y precios citables.**

## Decisión **[propuesta]**

### Escenario de carga

Se prueba **Inventario** detrás del gateway, con las dos réplicas, que es el único servicio real de esta entrega. Mezcla inicial **[propuesta]**:

| Operación                          | Peso   |
| ---------------------------------- | ------ |
| Consultar stock (`GET /v1/stock`)  | 70 %   |
| Reservar (`POST /v1/reservas`)     | 30 %   |

El guion (`loadtest/inventario.js`, k6) carga stock de una variante de prueba y ejecuta pedidos a tasa constante. Se sube la tasa por escalones hasta que aparezcan errores o la latencia pase el objetivo (p95 de 300 ms, ADR-011). **El guion no se ejecutó todavía.**

### Hipótesis de cuellos de botella (por validar)

| # | Hipótesis                                                                                                                                         | Cómo se vería                                                                 |
| - | ------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| 1 | **Contención sobre una fila de stock:** muchas reservas de la misma variante se serializan en el bloqueo de MySQL (ADR-004).                      | Latencia que sube solo en la variante popular; esperas de bloqueo en MySQL    |
| 2 | **Pool de conexiones:** 20 por réplica, 40 en total.                                                                                              | Pedidos que esperan una conexión; latencia que sube con la CPU baja          |
| 3 | **Una sola base MySQL:** réplicas más numerosas no ayudan si la base se satura.                                                                   | CPU o disco de MySQL al límite                                               |
| 4 | **Límite del gateway:** 60 pedidos por minuto por grupo con ráfaga de 20 (a propósito, por contrato). Para la prueba, se prueba contra Inventario por la entrada de administración, no por la clave de un grupo. | `429` en la prueba si se usa la clave de un grupo |

La hipótesis 1 es la candidata a **primer cuello**, porque el stock de una variante es un contador compartido por diseño. Se confirma o se descarta con la prueba.

### Cómo escalar

| Opción                                                   | Cuándo                                                                                      |
| -------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| **Más réplicas de Inventario**                           | Si el límite es CPU de la aplicación (sirve poco si el límite es la base)                    |
| **Subir el pool de conexiones**                          | Si hay espera de conexión y MySQL tiene margen                                              |
| **Más recursos para MySQL / réplica de lectura**         | Si el límite es la base. Las lecturas de stock podrían ir a una réplica, con atraso aceptable solo donde se toleran |
| **Cachear lecturas de catálogo**                         | Solo para Catálogo (ADR-007). **No** para disponibilidad                                    |
| **Partir el stock de una variante muy pedida** **[?]**   | Solo si la contención de una fila fuera el límite y no alcanzara lo anterior                |

### Costos **[completar]**

Método: costo mensual = cómputo (instancias × horas) + base de datos + almacenamiento + transferencia + servicios administrados. Se calcula para dos escenarios: **uso bajo** y **pico**.

| Componente                    | Cantidad (supuesto)                       | Precio unitario | Costo mensual |
| ----------------------------- | ----------------------------------------- | --------------- | ------------- |
| Réplicas de Inventario        | 2 [completar según la prueba]             | [completar]     | [completar]   |
| Gateway                       | 1 (2 si se resuelve el punto único)       | [completar]     | [completar]   |
| MySQL (Inventario, Ventas, Usuarios) | [completar]                        | [completar]     | [completar]   |
| MongoDB (Catálogo)            | [completar]                               | [completar]     | [completar]   |
| RabbitMQ                      | 1                                         | [completar]     | [completar]   |
| Observabilidad (Prometheus, Jaeger, Grafana) | [completar]                | [completar]     | [completar]   |
| Transferencia de datos        | [completar]                               | [completar]     | [completar]   |

Los precios se toman de la página del proveedor elegido, con fecha y enlace, **cuando se defina dónde se despliega** (hoy todo corre en Docker Compose local). No se inventan cifras.

## Alternativas consideradas

| Alternativa                                    | Por qué se descartó                                                                                         |
| ---------------------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| **Estimar capacidad por cálculo, sin prueba**  | Sin medición no se puede defender; la consigna pide la prueba.                                              |
| **Probar todos los servicios a la vez**        | Hoy solo Inventario es real; probar mocks no mide nada del sistema.                                         |
| **Escalar todo por igual**                     | Gasta donde no hace falta y no mueve el límite si el cuello es la base.                                     |

## Consecuencias

**Positivas**

- Quedan fijados el escenario, las hipótesis y el método antes de medir, así los resultados no se acomodan.
- El guion se puede repetir y comparar entre versiones.

**Negativas aceptadas**

- **Hoy no hay capacidad declarada.** Todo lo de arriba es hipótesis hasta correr la prueba.
- **La prueba corre en una máquina de desarrollo** con todo en Docker: los números no se extrapolan a producción sin cuidado.
- **Los costos dependen de una decisión de despliegue que todavía no existe.**

## Pendientes

- **Ejecutar la prueba** y registrar resultados (tasa máxima, latencia, primer cuello) en este ADR.
- **Decidir el entorno de despliegue** y completar la tabla de costos con precios citados.
- **Repetir la prueba con una réplica y con dos** para mostrar el efecto de escalar.

## Validación

1. Levantar el sistema y correr `loadtest/` (ver su README).
2. Mirar en Prometheus la latencia p95, los códigos y el reparto por réplica durante la prueba.
3. Registrar aquí la tasa en la que se rompe el objetivo y qué recurso se saturó primero.
