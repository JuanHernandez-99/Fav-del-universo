# TP MongoDB · Catálogo de FAV

Leé `tp-mongo/PROYECTO.md` completo antes de escribir código. Es la fuente única de decisiones y requisitos.

Reglas:

- Trabajá solo en `services/catalogo/` y `tp-mongo/`. No toques el docker-compose raíz, el gateway, `.env.example` ni `go.work`.
- No inventes reglas de negocio. Lo que no esté definido va a la sección 14 de PROYECTO.md con el valor provisorio indicado.
- Antes de usar ids o rutas de Inventario, leé su código real en `services/inventario/`.
- El servicio no maneja stock. La baja de producto es lógica (despublicar).
- Nunca subas `.env` ni credenciales. Solo haces commits en la rama `services-catalogo-mongo` si yo te lo indico explicitamente.
- La consigna del trabajo está en (Consigna_Proyecto_NoSQL_MongoDB.pdf), si estamos por hacer algo que tenga conflicto con esa consigna hacemelo saber antes.

## Modo de trabajo (obligatorio)

- Soy estudiante y no conozco MongoDB. El objetivo es aprender, no solo entregar.
- Trabajá de a un paso chico por vez. No generes varios archivos ni varias capas juntas.
- Antes de escribir código, explicame en pocas líneas qué vas a hacer y por qué (concepto de Mongo o de Go incluido).
- Después de hacerlo, mostrame cómo probarlo y qué resultado debería ver.
- Frená y esperá mi OK antes de pasar al paso siguiente.
- Si hay una decisión de diseño, ofrecé las opciones y recomendá una, en vez de elegir sola.
- Usá nombres y ejemplos del dominio (remeras, talles, promociones), no genéricos.
