# FAV del universo

Antes de escribir código, leé lo que corresponda a la tarea: `SPEC.md`, `docs/ARCHITECTURE.md`, los ADR de `docs/adr/` y el `README.md` del servicio. Para Catálogo, además, `services/catalogo/DISENO.md`.

Reglas:

- No inventes reglas de negocio. Lo que no esté definido se anota como pendiente (en el ADR o en el diseño del servicio) y me lo consultás.
- Antes de usar ids, rutas o contratos de otro servicio, leé su código real.
- Si algo choca con la consigna (`docs/Enunciado TP Final.pdf`) o con un ADR, avisame antes de hacerlo.
- Nunca subas `.env` ni credenciales.
- **No hagas commits sin que te lo pida explícitamente.** Que te pida hacer o terminar una tarea no es pedir el commit. Cuando lo pida: sin línea de coautor. Nunca `push`.
- La documentación es concisa y relevante: no se documenta lo que todavía no existe ni se afirma lo que no se verificó.

## Modo de trabajo

- Soy estudiante: el objetivo es aprender, no solo entregar. Respuestas cortas; el detalle va en lo que tengo que entender o defender.
- Para una tarea que te pido completa: analizala entera, preguntame solo las dudas reales, hacela completa y después explicame qué hiciste, por qué y cómo probarlo.
- Para una tarea sin ese pedido: un paso chico por vez. Antes de escribir código, explicá en pocas líneas qué vas a hacer y por qué; después, cómo probarlo y qué resultado esperar. Esperá mi OK para seguir.
- Si hay una decisión de diseño, dame las opciones y recomendá una, en vez de elegir solo.
- Usá nombres y ejemplos del dominio (remeras, talles, promociones), no genéricos.
- Verificá lo que hacés (tests, `curl`, Docker) y decime con honestidad qué probaste y qué no.
