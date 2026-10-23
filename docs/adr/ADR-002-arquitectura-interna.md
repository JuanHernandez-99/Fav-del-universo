# ADR-002 · Arquitectura interna de los servicios

|                             |                                                                                                                                                                                                                                                                                                       |
| --------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Estado**                  | Aceptada · Entrega 2. Implementada y verificada en Inventario; el resto de los servicios sigue _previsto_.                                                                                                                                                                                            |
| **Fecha**                   | 23/10/2026                                                                                                                                                                                                                                                                                            |
| **Decisión de la consigna** | D2 · Arquitectura interna de los servicios                                                                                                                                                                                                                                                            |
| **Relacionadas**            | [ADR-001](ADR-001-limites-de-los-servicios.md) (límites), [ADR-003](ADR-003-persistencia.md) (persistencia), [ADR-004](ADR-004-consistencia-y-concurrencia.md) (concurrencia), [ADR-009](ADR-009-consumo-del-gimnasio.md) (adaptador del gimnasio), [ADR-012](ADR-012-balanceo-de-carga.md) (réplicas) |

> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar · _previsto_: forma parte del diseño pero todavía no está implementado.

## Contexto

La consigna pide que el sistema aplique **dos estilos o patrones de arquitectura interna distintos, de manera reconocible**, y que se justifique la adecuación de cada uno al problema del servicio. El apunte de estilos de backend plantea el criterio: capas cuando hay pocas reglas y consultas sencillas; puertos y adaptadores (hexagonal) cuando hay invariantes de negocio o el núcleo depende de proveedores externos que pueden cambiar.

Los cuatro servicios tienen problemas de distinta naturaleza (ARCHITECTURE §4):

- **Inventario** tiene las reglas más críticas: no vender de más, reservar todo o nada, transferir sin dejar stock negativo y las transiciones de la reserva. Además consume la capacidad de un gimnasio cuyo contrato no controlamos (ADR-009) y recibe pedidos por varias entradas (Ventas, el panel y procesos periódicos).
- **Ventas** tiene el ciclo de estados de la venta y depende de varios sistemas externos (Mercado Pago, el proveedor de mail, RabbitMQ, Catálogo e Inventario).
- **Catálogo** y **Usuarios** tienen pocas reglas: altas, ediciones y lecturas, y emisión del token.

## Criterios

1. **Adecuación al problema:** la estructura sigue la complejidad real del servicio.
2. **Poder probar las reglas sin infraestructura**, con adaptadores falsos.
3. **Poder cambiar un proveedor externo** sin tocar el núcleo.
4. **Cumplir la consigna:** dos estilos distintos y reconocibles.
5. **Costo para un equipo de dos personas:** no agregar piezas sin una variación que proteger.

## Decisión

| Servicio       | Estilo                                    | Estado           |
| -------------- | ----------------------------------------- | ---------------- |
| **Inventario** | Hexagonal (puertos y adaptadores)         | **Implementado** |
| **Ventas**     | Hexagonal                                 | _Previsto_       |
| **Catálogo**   | Capas: Handler → Service → Repository     | _Previsto_       |
| **Usuarios**   | Capas                                     | _Previsto_       |

Las razones de cada estilo están en ARCHITECTURE §4. Este ADR registra cómo quedó aplicado en Inventario, que es el único servicio real de esta entrega.

### Estructura de Inventario

```
services/inventario/
├── cmd/inventario/main.go          arma las piezas y arranca (composición)
└── internal/
    ├── dominio/                    reglas: reserva, stock, ingreso, transferencia, reposición, errores
    ├── aplicacion/                 casos de uso: Reservar, Confirmar, Liberar, VencerReserva, Transferir, Ingresar, Reponer…
    ├── puertos/                    interfaces: Repositorio y Gimnasio
    ├── adaptadores/
    │   ├── http/                   entrada: API REST, Problem Details, rutas
    │   ├── procesos/               entrada: vencimiento de reservas y envío de reposiciones
    │   ├── mysql/                  salida: implementa Repositorio (transacciones y actualizaciones condicionales)
    │   └── gimnasio/               salida: implementa Gimnasio (capa anticorrupción, circuit breaker)
    └── observabilidad/             transversal: logs, trazas y métricas
```

**Regla de dependencias:** `dominio` ← `aplicacion` ← `adaptadores`, y los adaptadores implementan los `puertos` que declara el núcleo. Comprobado sobre el código:

| Paquete      | Importa                                                                       |
| ------------ | ----------------------------------------------------------------------------- |
| `dominio`    | Solo la biblioteca estándar                                                   |
| `puertos`    | `dominio`                                                                     |
| `aplicacion` | `dominio`, `puertos`, `slog` y `google/uuid` (para generar ids)               |
| `adaptadores`| `dominio`, `aplicacion` o `puertos`, y las bibliotecas de cada tecnología     |

### Dónde vive cada regla

| Regla                                                                         | Capa                          | Cómo                                                                                          |
| ----------------------------------------------------------------------------- | ----------------------------- | --------------------------------------------------------------------------------------------- |
| Validación del pedido (ítems, cantidades, vigencia, clave)                    | `dominio`                     | `NuevaReserva`, `NuevoIngreso`, `NuevaTransferencia`                                          |
| Transiciones de la reserva (activa → confirmada, liberada o vencida)          | `dominio`                     | `Confirmar`, `Liberar`, `Vencer` sobre la reserva                                             |
| Orden de bloqueo de las filas (ítems ordenados por variante)                  | `dominio`                     | `NuevaReserva` ordena los ítems                                                               |
| Huella del pedido (detectar una clave reutilizada con otro cuerpo)            | `dominio`                     | `huellaReserva`                                                                               |
| Cuándo reintentar un envío al gimnasio y cuánto esperar                       | `dominio` y `aplicacion`      | `EsperaReintento`, `EnviarReposicion`                                                         |
| **No reservar más de lo disponible** (la carrera entre dos reservas)          | `adaptadores/mysql`           | `UPDATE … WHERE online - reservado >= ?` en una transacción. El puerto expresa la operación completa |
| Garantía de idempotencia                                                      | `adaptadores/mysql`           | Restricción única sobre la clave                                                              |
| Traducir errores del dominio a HTTP                                           | `adaptadores/http`            | Problem Details con `codigo` estable                                                          |

La carrera la decide la base, no el núcleo: el puerto del repositorio expresa **operaciones completas** ("reservar si alcanza"), y el adaptador de MySQL las hace atómicas. Es la decisión de fondo del estilo en este servicio (ARCHITECTURE §4).

### Por qué se puede probar sin infraestructura

- El dominio se prueba sin base ni red (`dominio/reserva_test.go`).
- El servicio de aplicación recibe sus dependencias por constructor (`NuevoServicio(repo, gimnasio)`), así que se puede probar con repositorio y gimnasio falsos. **Falta escribir esos tests** (ver Pendientes).
- Los adaptadores se prueban contra una dependencia real: `adaptadores/mysql/repositorio_test.go` corre contra un MySQL (ADR-004).

## Alternativas consideradas

| Alternativa                                                    | Por qué se descartó                                                                                                                                                                                                                            |
| -------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Capas para Inventario**                                      | Las reglas críticas quedarían repartidas entre el servicio y el repositorio, y el servicio quedaría atado a MySQL y al contrato del gimnasio. Cambiar el proveedor del gimnasio (cuyo contrato todavía no existe) tocaría la lógica de negocio. |
| **Hexagonal para Catálogo y Usuarios**                         | No hay reglas con invariantes fuertes ni proveedores externos que puedan cambiar. Los puertos agregarían piezas sin una variación que proteger.                                                                                                |
| **Un único estilo en todos los servicios**                     | No cumple la consigna (dos estilos) y obliga a pagar el costo de los puertos donde no hace falta o a dejar sin protección donde sí.                                                                                                            |
| **DDD táctico completo (agregados, repositorios por agregado)**| El tamaño del dominio no lo justifica. Se toma lo útil (reglas en el núcleo, transiciones como métodos) sin el resto de la ceremonia.                                                                                                           |
| **CQRS o Event Sourcing en Inventario**                        | Las lecturas de stock son por id y de bajo volumen. Event Sourcing complicaría las operaciones críticas sin resolver un problema que hoy exista.                                                                                                |

## Consecuencias

**Positivas**

- Las reglas críticas están en un paquete sin dependencias externas y son fáciles de leer y de probar.
- El contrato supuesto del gimnasio queda aislado en un paquete (`adaptadores/gimnasio`): cuando llegue el real, cambia solo ese paquete.
- La entrada HTTP y los procesos periódicos comparten los mismos casos de uso.

**Negativas aceptadas**

- **Más archivos y capas** que un servicio por capas, para un equipo de dos personas.
- **Desvíos de la regla de dependencias que se aceptan por ahora:**
  - `dominio/errores.go` tiene etiquetas `json` en `CampoInvalido` y `Faltante`, porque el adaptador HTTP las serializa tal cual. Es una filtración del formato de salida hacia el núcleo.
  - `aplicacion` importa `google/uuid` y escribe logs con `slog` directamente, en lugar de recibir un generador de ids y un puerto de log.
  - `adaptadores/mysql` importa `observabilidad` solo para leer el identificador de correlación y guardarlo en el outbox.
- **Dos estilos que mantener** en el mismo repositorio.

## Pendientes

- **Tests de la capa `aplicacion` con repositorio y gimnasio falsos.** Hoy el núcleo se cubre con tests de dominio y con los de integración contra MySQL. Los de integración usan el repositorio real y no reemplazan a los unitarios del servicio de aplicación.
- **Sacar las etiquetas `json` del dominio** y mover el formato al adaptador HTTP.
- **Aplicar el estilo hexagonal a Ventas y el de capas a Catálogo y Usuarios** cuando se implementen. La consigna pide dos estilos aplicados de manera reconocible para la presentación grupal.
- **Publicador hacia RabbitMQ** como adaptador de salida de Inventario (un puerto nuevo). Existe una versión previa en el historial (commit `54749f9` de `development`); se adapta a esta estructura.

## Validación

- La regla de dependencias se verificó sobre las importaciones de cada paquete (tabla de arriba).
- Los tests de dominio y los 7 de integración contra MySQL real (ADR-004) pasan.
- **[propuesta]** Agregar una comprobación automática en CI que falle si `dominio` importa algo que no sea la biblioteca estándar.
