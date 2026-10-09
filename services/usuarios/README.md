# Usuarios

Registro, inicio de sesión, emisión del token y roles (comprador y administrador).

> Estado: **previsto** (Entrega 2). Hoy solo está el diseño. **[propuesta]** marca lo no confirmado.

| | |
|---|---|
| **Base de datos** | MySQL, con email único garantizado por la base ([ADR-003](../../docs/adr/ADR-003-persistencia.md)) |
| **Estilo interno** | Capas **[propuesta]** |
| **Token** | JWT con `sub`, `email`, `role`, `iat` y `exp`. Firma RS256 y vencimiento de 1 hora, sin refresh token **[propuesta]** ([ADR-001](../../docs/adr/ADR-001-limites-de-los-servicios.md)) |

## Qué no hace

No conoce las ventas: Ventas guarda la referencia al comprador. El gateway valida el token con la clave pública, **sin llamar a Usuarios**.

## Se comunica con

Solo con el gateway (HTTP). No publica ni consume eventos.

## Más información

[Arquitectura §4 y §6](../../docs/ARCHITECTURE.md) · [ADR-001](../../docs/adr/ADR-001-limites-de-los-servicios.md)
