# ADR-015 · Claves del token, cuenta de administrador y contraseñas

| | |
|---|---|
| **Estado** | Aceptada · 04/11/2026 |
| **Fecha** | 04/11/2026 |
| **Decisión de la consigna** | Adicional |
| **Relacionadas** | [ADR-001](ADR-001-limites-de-los-servicios.md) (identidad y token), [ADR-003](ADR-003-persistencia.md) (persistencia), [ADR-002](ADR-002-arquitectura-interna.md) (estilo en capas) |

> Convenciones: **[?]** pendiente de definir · **[propuesta]** sugerido, sin confirmar.

## Contexto

Al implementar Usuarios quedaban tres puntos abiertos.

- **Claves del token.** ADR-001 propuso firmar con RS256 y dejó la confirmación para la implementación. La consigna (2.12) pide levantar el sistema con un procedimiento único, sin configuraciones manuales y sin secretos en el repositorio. `.env.example` esperaba dos archivos de claves creados a mano.
- **Cuenta de administrador.** RN-23 pide una sola cuenta, pero nada dice cómo nace. No puede salir del registro público: cualquiera se haría administrador.
- **Contraseña.** HU-18 pide guardarla con hash y no devolverla nunca, pero no define requisitos ni algoritmo.

## Criterios

1. Arrancar con un comando, sin pasos manuales y sin secretos en el repositorio.
2. Que solo Usuarios pueda emitir tokens.
3. Que nadie llegue a administrador por el registro público.
4. No imponer reglas que el cliente no pidió.

## Decisión

**1. RS256, con las claves generadas por un contenedor `usuarios-init`.** Corre al levantar, usa la misma imagen que Usuarios y crea el par si no existe. Si ya existe, no lo cambia, para no invalidar las sesiones iniciadas.

| Volumen de Docker | Contiene | Lo monta |
|---|---|---|
| `jwt-privada` | Clave privada, con permiso `600` | `usuarios-init` y Usuarios |
| `jwt-publica` | Clave pública | `usuarios-init` y, desde que valide el token, el gateway |

Son volúmenes separados para que el gateway no tenga acceso a la privada. Esto confirma la propuesta de ADR-001.

**2. La cuenta de administrador se crea al arrancar**, con `ADMIN_EMAIL` y `ADMIN_PASSWORD`, si no existe. El registro público siempre da rol `comprador`, y el pedido no admite un campo de rol.

- Si ya existe, no se toca: cambiar la variable no cambia la contraseña guardada.
- Si ese email ya es de un comprador, Usuarios no arranca y lo informa: no le cambia el rol.
- En local usa un valor de prueba, como las demás claves del Compose. **En la nube hay que definir uno propio.**

**3. Contraseñas con bcrypt, costo 12, y un mínimo de 8 caracteres** **[propuesta]** (SPEC RN-26), sin exigir mayúsculas ni símbolos. El máximo es de 72 bytes, porque bcrypt no admite más.

**4. Vencimiento de 1 hora y sin refresh token**, como proponía ADR-001. El vencimiento se configura con `JWT_EXPIRATION_MINUTES`.

**5. El inicio de sesión tiene un límite por IP en el gateway:** 30 pedidos por minuto, con ráfagas de hasta 10 **[propuesta]**. Frena los intentos masivos de adivinar contraseñas.

## Alternativas consideradas

| Alternativa | Por qué se descartó |
|---|---|
| **Secreto compartido (HS256)** | Más simple, pero quien verifica también podría emitir tokens. Contradice ADR-001. |
| **Claves creadas a mano** | No cumple la consigna: sería una configuración que solo existe en la computadora de cada integrante. |
| **Generar las claves al arrancar Usuarios** | Con varias réplicas, cada una podría generar la suya. Un contenedor que corre una sola vez evita la carrera. |
| **Administrador creado con un script manual** | Es un paso manual. |
| **Primer usuario registrado es administrador** | Quien llegue primero se queda con la cuenta. |
| **argon2id en lugar de bcrypt** | También es válido. bcrypt no requiere ajustar parámetros de memoria y alcanza para este volumen. |
| **Reglas de complejidad** (mayúsculas, números) | El cliente no las pidió y molestan a quien compra. |

## Consecuencias

**Positivas**

- `docker compose up` deja el login funcionando en una computadora limpia.
- Las claves no están en el repositorio ni las ve el gateway.
- Un email inexistente y una contraseña incorrecta dan la misma respuesta, y las dos calculan un hash, así que tampoco se distinguen por lo que tardan (HU-18.3).

**Negativas aceptadas**

- **`docker compose down -v` borra las claves:** se generan otras y las sesiones iniciadas dejan de valer. Como los tokens duran una hora, se acepta.
- **Un token no se puede revocar** antes de que venza (ADR-001).
- **La contraseña de administrador de prueba está en el Compose**, como las demás de desarrollo. No sirve para un entorno público.
- **Cambiar la contraseña de administrador exige tocar la base:** no hay una función para hacerlo en v1.
- **Registrarse con un email ya usado responde `409`,** como pide HU-18.1. Eso revela que el email existe.

## Pendientes

- **[?]** Cómo se entregan las claves y la contraseña de administrador en la nube: se resuelve con el despliegue.
- **Validar el token en el gateway:** es el paso siguiente, y ahí se monta `jwt-publica`.
- Confirmar con el cliente el mínimo de 8 caracteres (SPEC RN-26).

## Validación

Ver [`services/usuarios/README.md`](../../services/usuarios/README.md).

---

**Si la decisión cambia:** no se borra este archivo. Se marca como *Reemplazada por ADR-YYY* y el nuevo ADR indica a cuál reemplaza y por qué hubo que revisarla.
