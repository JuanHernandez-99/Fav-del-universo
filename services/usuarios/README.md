# Usuarios

Registro, inicio de sesión, emisión del token y roles (comprador y administrador).

> Estado: **funcionando**. El gateway todavía no valida el token: hasta entonces se puede iniciar sesión, pero el token no abre "Mis compras" ni el panel.

| | |
|---|---|
| **Base de datos** | MySQL, con email único garantizado por la base ([ADR-003](../../docs/adr/ADR-003-persistencia.md)) |
| **Estilo interno** | Capas: `handler` → `service` → `repository` ([ADR-002](../../docs/adr/ADR-002-arquitectura-interna.md)) |
| **Token** | JWT con `sub`, `email`, `role`, `iat` y `exp`. Firma RS256 y vencimiento de 1 hora, sin refresh token ([ADR-001](../../docs/adr/ADR-001-limites-de-los-servicios.md), [ADR-015](../../docs/adr/ADR-015-claves-del-token-y-cuenta-de-administrador.md)) |
| **Contraseñas** | bcrypt con costo 12. Nunca se guardan ni se devuelven (HU-18.2) |
| **Administrador** | Una cuenta, creada al arrancar con `ADMIN_EMAIL` y `ADMIN_PASSWORD` (RN-23) |

## Qué no hace

No conoce las ventas: Ventas guarda la referencia al comprador. El gateway valida el token con la clave pública, **sin llamar a Usuarios**.

## Probarlo

Con el sistema levantado (`docker compose up --build`), por el gateway:

```bash
J="Content-Type: application/json"

curl localhost:8080/usuarios/v1/registro -H "$J" -d '{"email":"ana@example.com","contrasena":"remera-blanca"}'
curl localhost:8080/usuarios/v1/sesiones -H "$J" -d '{"email":"ana@example.com","contrasena":"remera-blanca"}'

# Administrador de prueba (valores por defecto de Docker Compose)
curl localhost:8080/usuarios/v1/sesiones -H "$J" -d '{"email":"admin@fav.example","contrasena":"cambiar-admin-local"}'
```

## API

| Método y ruta | Qué hace | Respuestas |
|---|---|---|
| `POST /v1/registro` | Crea un comprador. Cuerpo: `email`, `contrasena` | `201` · `400 PEDIDO_INVALIDO` · `409 EMAIL_EN_USO` |
| `POST /v1/sesiones` | Inicia sesión. Cuerpo: `email`, `contrasena` | `200` con `token`, `tipo` y `venceEn` · `401 CREDENCIALES_INVALIDAS` · `429` por exceso de intentos |
| `GET /healthz`, `/readyz`, `/metrics` | Internas | No se exponen por el gateway |

Se comunica solo con el gateway (HTTP). No publica ni consume eventos. Los errores siguen RFC 9457 con un `codigo` estable.

## Tests

```bash
go test ./services/usuarios/...
docker compose up -d mysql-usuarios
USUARIOS_TEST_DSN="root:cambiar@tcp(localhost:3309)/usuarios_test?parseTime=true&loc=UTC&multiStatements=true" \
  go test ./services/usuarios/...
```

**Verificado el 04/11/2026** con el sistema levantado por Docker: registro, email repetido, inicio de sesión, firma verificada con la clave pública generada, hash en la base, cuenta de administrador, claves conservadas al reiniciar, una compra de invitado con Usuarios detenido y límite de intentos (`429`).

**Sin probar:** una computadora limpia desde cero (sí, volúmenes de claves y base nuevos), y un token vencido, que se rechazará recién cuando el gateway lo valide.

## Configuración

| Variable | Por defecto | |
|---|---|---|
| `USUARIOS_DB_HOST`, `_NAME`, `_USER`, `_PASSWORD` | `mysql-usuarios:3306`, `usuarios` | Conexión a MySQL |
| `ADMIN_EMAIL`, `ADMIN_PASSWORD` | valores de prueba en Compose | Cuenta de administrador. Cambiar en un entorno público |
| `JWT_EXPIRATION_MINUTES` | `60` | Vencimiento del token |
| `JWT_CLAVE_PRIVADA`, `JWT_CLAVE_PUBLICA` | `/claves/privada/jwt_private.pem`, `/claves/publica/jwt_public.pem` | Las genera `usuarios-init` |

## Más información

[ADR-015](../../docs/adr/ADR-015-claves-del-token-y-cuenta-de-administrador.md) · [ADR-001](../../docs/adr/ADR-001-limites-de-los-servicios.md) · [Arquitectura §4 y §6](../../docs/ARCHITECTURE.md)
