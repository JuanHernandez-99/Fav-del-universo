# Preguntas para la cátedra — Entrega 1

> Grupo FAV · Tienda Online · Arquitectura de Software 2026
> Archivo de trabajo del equipo. **No se sube al repositorio.**
> Convención: 🔴 afecta la Entrega 1 · 🟡 afecta entregas siguientes.

---

## 1. Contrato y capacidad

1. 🔴 **Formato del contrato.** Los dos contratos propuestos están en **OpenAPI 3** (REST). No lo vimos en clase. ¿Es un formato aceptado, o prefieren otro?
2. 🔴 **Capacidad propuesta.** Llevamos dos opciones independientes del dominio de ropa: **A, reserva de cupos con vencimiento** (Inventario) y **B, envío de emails** (Ventas). ¿Qué grupo nos consume y qué flujo suyo cubriría la capacidad? ¿Podemos elegir entre A y B después de saberlo?
2b. 🔴 **"Flujo importante".** ¿Un aviso por mail cuenta como flujo importante del consumidor, o se espera algo más central en su negocio?
2c. 🟡 **Cambio de capacidad.** Si después de la Entrega 1 el consumidor asignado no necesita la opción elegida, ¿podemos cambiarla?
2d. 🟡 **Dónde vive.** ¿Es válido que la capacidad viva dentro de un servicio existente (Inventario o Ventas) y no en un servicio propio?
3. 🔴 **Qué debe contener el contrato.** ¿Hay algún mínimo (operaciones, errores, versionado, ejemplos) que quieran ver en esta entrega además de lo que dice el enunciado?
4. 🔴 **Mock.** ¿Alcanza con un servidor que devuelva respuestas fijas según el contrato? ¿Tiene que correr con Docker en esta entrega?
5. 🟡 **Autenticación del consumidor externo.** Proponemos una clave propia por grupo en el header `X-API-Key`. ¿Les parece suficiente o esperan otro mecanismo (por ejemplo, un token)?
6. 🟡 **Capacidad a consumir.** ¿Cuándo y cómo se asigna qué capacidad de otro grupo consumimos?

## 2. Documentación y ADR

7. 🔴 **Formato de ADR.** El enunciado menciona un formato de ADR en una "sección 8" que no encontramos en el PDF. ¿Dónde está ese formato o cuál usamos? (Usamos una plantilla propia, `ADR-000`, hasta saberlo.)
8. 🔴 **Numeración.** Numeramos cada ADR con el número de su decisión (ADR-001 = D1, ADR-003 = D3, y así). ¿Les sirve esa forma?
9. 🔴 **Diagramas.** Los hacemos en draw.io y los incluimos como PNG en los `.md` (con el `.drawio` como fuente). ¿Está bien ese formato?

## 3. Arquitectura

10. 🔴 **Cantidad de servicios.** Tenemos cuatro servicios de negocio (Catálogo, Inventario, Ventas, Usuarios) más un API Gateway y un frontend. ¿El gateway cuenta como microservicio a efectos de "una base por servicio"? (El gateway no tiene base.)
11. 🟡 **Dos estilos de arquitectura.** Usaríamos **capas** (Catálogo y Usuarios) y **hexagonal** (Ventas). ¿Cumple con el mínimo de dos estilos?
12. 🟡 **Bases de datos.** Catálogo con MongoDB y los otros tres con MySQL (una instancia por servicio). ¿Hay algún motivo para preferir otro tipo de base?
12b. 🟡 **Almacenamiento no relacional.** ¿Redis (caché) o el motor de búsqueda cuentan como el almacenamiento no relacional que pide el requisito 2.8, o tiene que ser la base de un servicio?

## 4. Despliegue y operación

13. 🟡 **Nube.** ¿Hay un proveedor recomendado o que debamos usar para el despliegue público de la capacidad? ¿Hay algún límite de costo?
14. 🟡 **Observabilidad.** ¿Podemos elegir las herramientas de logs, métricas y trazas, o hay un conjunto esperado? (Lo dejamos pendiente hasta el teórico de observabilidad.)
15. 🟡 **Búsqueda.** ¿Hay alguna preferencia entre Elasticsearch u OpenSearch para el motor de búsqueda?
16. 🟡 **Pruebas de carga.** Nuestro volumen real es bajo (10 a 20 prendas por semana). ¿Aceptan que el pico sea una hipótesis documentada, simulada en la prueba de carga?

---

**Respuestas** (completar):

| # | Respuesta | Fecha |
|---|---|---|
| | | |
