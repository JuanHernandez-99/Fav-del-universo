# Capacidad publicada por FAV

> **Estado: propuesta para la Entrega 1** · 09/10/2026
>
> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido por el equipo, sin confirmar.

FAV propone **dos capacidades alternativas** para que las use otro grupo. Se publicará **una sola**, según el grupo consumidor que asigne la cátedra. Las dos son independientes del dominio de FAV (ropa): sirven para sistemas de otros dominios.

| Opción | Qué ofrece | Servicio de FAV | Contrato |
|---|---|---|---|
| **A · Reserva de cupos** | Apartar algo limitado durante un tiempo, sin sobreventa, y después confirmarlo o liberarlo | Inventario | [`cupos-v1.openapi.yaml`](cupos-v1.openapi.yaml) |
| **B · Envío de emails** | Enviar un email armado por el consumidor, en el momento o programado, con reintentos y estado consultable | Ventas | [`emails-v1.openapi.yaml`](emails-v1.openapi.yaml) |

Los contratos están en **OpenAPI 3.0.3** y son la fuente de verdad. Se pueden ver en [Swagger Editor](https://editor.swagger.io) pegando el YAML.

---

## 1. Reglas comunes a las dos opciones

### Autenticación
Cada grupo consumidor recibe una **clave propia** y la envía en el header `X-API-Key` **[propuesta]**. La clave separa los datos de cada grupo: un grupo solo ve y opera lo suyo. Lo de otro grupo responde como inexistente (`404`). **[?]** cómo se entrega y se rota la clave.

### Idempotencia
- Las operaciones que **crean** algo (reservar, enviar un email) exigen el header `Idempotency-Key`: una clave única por cada operación lógica (se recomienda un UUID).
- Ante un timeout, un `500` o un `503`, se reintenta **con la misma clave y el mismo cuerpo**: se obtiene el mismo resultado, sin duplicar.
- La misma clave con otro cuerpo devuelve `409 CLAVE_IDEMPOTENCIA_REUTILIZADA`.
- Vigencia de la clave: 48 horas **[propuesta]**.
- Las operaciones sobre algo existente (confirmar, liberar, cancelar, registrar un cupo) son idempotentes por sí mismas: repetirlas no tiene efecto adicional.

### Errores
Todos los errores usan **Problem Details** (RFC 9457) con un campo `codigo` estable. **Decidir siempre por `codigo`**, no por los textos.

```json
{
  "type": "https://fav.example/errores/pedido-invalido",
  "title": "Pedido inválido",
  "status": 400,
  "codigo": "PEDIDO_INVALIDO",
  "errores": [{ "campo": "items[0].cantidad", "mensaje": "debe ser mayor o igual a 1" }]
}
```

Errores comunes a las dos opciones:

| HTTP | `codigo` | Significado | ¿Reintentar? |
|---|---|---|---|
| 400 | `PEDIDO_INVALIDO` | Falta un header obligatorio o el cuerpo no cumple el contrato. `errores` indica los campos. | No. Corregir. |
| 401 | `NO_AUTENTICADO` | Falta la clave del grupo o no es válida. | No. |
| 409 | `CLAVE_IDEMPOTENCIA_REUTILIZADA` | La clave ya se usó con otro cuerpo. | No. Usar otra clave. |
| 429 | `LIMITE_EXCEDIDO` | Demasiados pedidos del grupo. **[?]** valor del límite. | Sí, después de `Retry-After`. |
| 500 | `ERROR_INTERNO` | Error inesperado. | Sí, con la misma `Idempotency-Key`. |
| 503 | `NO_DISPONIBLE` | El servicio o una dependencia no está disponible. | Sí, con la misma `Idempotency-Key`. |

### Recomendaciones para el consumidor
- Timeout de 2 segundos por llamada **[propuesta]**.
- Reintentar solo ante timeout, `429`, `500` o `503`, hasta 3 veces con espera creciente **[propuesta]**.
- Enviar `X-Correlation-Id` para seguir una operación en los logs de ambos sistemas. Siempre se devuelve en la respuesta.
- Ignorar los campos de respuesta que no conozca (ver versionado).

---

## 2. Opción A · Reserva de cupos

Un **cupo** es algo limitado con una cantidad, por ejemplo "tortas del sábado, 10". FAV no sabe qué representa: para FAV es un identificador y un número.

**Disponible = capacidad − reservado − confirmado.** Nunca se reserva más de lo disponible.

| Operación | Ruta | Qué hace |
|---|---|---|
| Registrar o ajustar un cupo | `PUT /v1/cupos/{cupoId}` | Crea el cupo o cambia su capacidad total. El consumidor elige el `cupoId`. |
| Consultar un cupo | `GET /v1/cupos/{cupoId}` | Devuelve capacidad, reservado, confirmado y disponible. |
| Reservar | `POST /v1/reservas` | Aparta unidades de uno o más cupos durante `vencimientoMinutos` (de 1 a 2880 **[propuesta]**). Todo o nada. |
| Confirmar | `POST /v1/reservas/{id}/confirmar` | Las unidades se consumen definitivamente. |
| Liberar | `POST /v1/reservas/{id}/liberar` | Las unidades vuelven a estar disponibles. |

### Ciclo de vida de una reserva

```
            confirmar
  ACTIVA ─────────────► CONFIRMADA
    │  │
    │  │ liberar
    │  └──────────────► LIBERADA
    │
    │ pasa venceEn
    └─────────────────► VENCIDA
```

Los tres estados finales no cambian más. Si una confirmación coincide con el vencimiento, gana una sola de las dos.

### Ejemplo

```bash
# 1. Registrar el cupo (una vez)
curl -X PUT http://localhost:8090/v1/cupos/tortas-2026-10-10 \
  -H "X-API-Key: <clave-del-grupo>" -H "Content-Type: application/json" \
  -d '{ "descripcion": "Tortas del sábado 10/10", "capacidad": 10 }'

# 2. Reservar 1 unidad por 24 horas
curl -X POST http://localhost:8090/v1/reservas \
  -H "X-API-Key: <clave-del-grupo>" -H "Content-Type: application/json" \
  -H "Idempotency-Key: 6f1c2b8e-3d4a-4e5f-9a7b-1c2d3e4f5a6b" \
  -d '{ "referenciaExterna": "pedido-4521", "vencimientoMinutos": 1440,
        "items": [ { "cupoId": "tortas-2026-10-10", "cantidad": 1 } ] }'
# → 201 { "id": "3fa85f64-…", "estado": "ACTIVA", "venceEn": "2026-10-10T15:00:00Z", … }

# 3. Confirmar (o liberar)
curl -X POST http://localhost:8090/v1/reservas/3fa85f64-5717-4562-b3fc-2c963f66afa6/confirmar \
  -H "X-API-Key: <clave-del-grupo>"
# → 200 { "estado": "CONFIRMADA", … }
```

### Errores propios

| HTTP | `codigo` | Operación | Significado |
|---|---|---|---|
| 404 | `CUPO_INEXISTENTE` | Consultar cupo | El cupo no existe o no es del grupo. |
| 422 | `CUPO_INEXISTENTE` | Reservar | Algún cupo del pedido no existe. No se reservó nada. |
| 409 | `CUPO_INSUFICIENTE` | Reservar | Algún cupo no alcanza. `faltantes` indica cuáles y cuánto hay. No se reservó nada. |
| 409 | `CAPACIDAD_MENOR_QUE_EN_USO` | Registrar cupo | La nueva capacidad es menor que lo reservado más lo confirmado. |
| 404 | `RESERVA_INEXISTENTE` | Confirmar, Liberar | La reserva no existe o no es del grupo. |
| 409 | `RESERVA_VENCIDA` | Confirmar | Venció antes de confirmarse. |
| 409 | `RESERVA_LIBERADA` | Confirmar | Se liberó antes de confirmarse. |
| 409 | `RESERVA_CONFIRMADA` | Liberar | Ya se confirmó; no se puede liberar. |

---

## 3. Opción B · Envío de emails

El consumidor manda **destinatario, asunto y texto ya armados**. FAV no decide ni interpreta el contenido. No hay plantillas en esta versión **[propuesta]**.

El envío es **asincrónico**: el pedido se responde con `202` apenas se registra, sin esperar al proveedor de mail. El estado se consulta después.

| Operación | Ruta | Qué hace |
|---|---|---|
| Enviar | `POST /v1/emails` | Registra el email para enviarlo ya o en `enviarEn` (hasta 30 días **[propuesta]**). |
| Consultar | `GET /v1/emails/{id}` | Devuelve el estado, los intentos y, si falló, el motivo. |
| Cancelar | `POST /v1/emails/{id}/cancelar` | Cancela un email programado que todavía no se envió. |

### Estados

```
               se envía
 PROGRAMADO ─────────────► ENVIADO
    │   │
    │   │ se agotan los reintentos (5 [propuesta])
    │   └─────────────────► FALLIDO
    │
    │ cancelar
    └─────────────────────► CANCELADO
```

`ENVIADO` significa que el proveedor de mail aceptó el mensaje; no garantiza que llegue a la bandeja de entrada.

Límites **[propuesta]**: un destinatario por email, asunto de hasta 200 caracteres, texto plano de hasta 10.000 caracteres.

### Ejemplo

```bash
# 1. Programar un recordatorio
curl -X POST http://localhost:8091/v1/emails \
  -H "X-API-Key: <clave-del-grupo>" -H "Content-Type: application/json" \
  -H "Idempotency-Key: 0c1d2e3f-4a5b-4c6d-8e7f-9a0b1c2d3e4f" \
  -d '{ "destinatario": "paciente@example.com", "asunto": "Recordatorio de turno",
        "texto": "Tu turno es mañana a las 10:00.", "enviarEn": "2026-10-14T12:00:00Z",
        "referenciaExterna": "turno-981" }'
# → 202 { "id": "9b2e7c41-…", "estado": "PROGRAMADO", "intentos": 0, … }

# 2. Consultar el estado
curl http://localhost:8091/v1/emails/9b2e7c41-0d3a-4f6e-8a51-2c7d9e0f1a23 \
  -H "X-API-Key: <clave-del-grupo>"

# 3. Cancelar (si el turno se canceló)
curl -X POST http://localhost:8091/v1/emails/9b2e7c41-0d3a-4f6e-8a51-2c7d9e0f1a23/cancelar \
  -H "X-API-Key: <clave-del-grupo>"
```

### Errores propios

| HTTP | `codigo` | Operación | Significado |
|---|---|---|---|
| 404 | `EMAIL_INEXISTENTE` | Consultar, Cancelar | El email no existe o no es del grupo. |
| 409 | `EMAIL_NO_CANCELABLE` | Cancelar | El email ya se envió o ya falló. |

**[?]** proveedor de mail, dirección remitente y cuánto tiempo se conservan los mensajes.

---

## 4. Entornos

| Entorno | Opción A | Opción B | Estado |
|---|---|---|---|
| Mock local | `http://localhost:8090` **[propuesta]** | `http://localhost:8091` **[propuesta]** | En `mocks/`, para la Entrega 1 |
| Desplegado (nube) | **[?]** | **[?]** | Antes de la presentación grupal. Proveedor de nube pendiente. |

## 5. Versionado y compatibilidad

- La versión mayor va en la ruta (`/v1/...`) y en `info.version` del contrato (versionado semántico).
- **Cambios compatibles** (suben la versión menor): operaciones nuevas, campos opcionales nuevos en los pedidos, campos nuevos en las respuestas, nuevos valores de `codigo`.
- **Cambios incompatibles** (nueva versión mayor, `/v2/...`): quitar o renombrar campos u operaciones, volver obligatorio un campo opcional, cambiar el significado de un estado o de un código.
- Si sale una `v2`, la `v1` sigue operativa hasta el fin de la evaluación.
- Todo cambio se registra en el historial y en el ADR-008 (D8).

## 6. Historial de versiones

| Versión | Fecha | Cambios |
|---|---|---|
| 1.0.0 | 09/10/2026 | Primera versión de las dos propuestas (A: cupos, B: emails). |

## 7. Contacto

**[?]** Canal para que el grupo consumidor reporte problemas o pida cambios (por ejemplo, issues del repositorio).
