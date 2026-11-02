# Informe técnico · Catálogo de FAV con MongoDB

**Trabajo Práctico Integrador · Base de Datos Aplicada** · Entrega: 3/11/2026 · Dominio aprobado: catálogo y gestión de productos

## En pocas líneas

**Problema.** FAV es una tienda de ropa que hoy vende solo en el local y suma la venta online. Este servicio resuelve **qué se vende y cómo se muestra**: productos con variantes (talle y color), fotos, categorías, precios y promociones. El administrador carga y edita el catálogo; el comprador lo ve y lo filtra.

**Por qué MongoDB.** Un producto se lee y se edita como una unidad (nombre, fotos, variantes y precio viajan juntos), los atributos cambian según la categoría, la carga es sobre todo de lectura y las escrituras tocan un solo producto. Un documento por producto evita las uniones.

**Qué se entrega.** Una base con tres colecciones y 279 documentos, 10 consultas, índices medidos con `explain`, una API en Go con repositorio y Swagger, un caso de consistencia con su prueba de concurrencia, un análisis en Jupyter y todo el entorno en Docker Compose.

## Índice del informe

| Archivo | Contenido |
|---|---|
| [`01-diseno.md`](01-diseno.md) | Requerimientos, colecciones, embeber o referenciar, decisiones de modelado y validaciones |
| [`02-consultas.md`](02-consultas.md) | Las 10 consultas y los operadores que muestran |
| [`03-indices.md`](03-indices.md) | Índices, `explain("executionStats")`, beneficio y costo |
| [`04-consistencia.md`](04-consistencia.md) | La variante repetida, la operación atómica y la prueba de 20 pedidos |
| [`05-api-y-datos.md`](05-api-y-datos.md) | La API, los datos, el análisis y el despliegue |
| [`06-conclusiones.md`](06-conclusiones.md) | Resultados, alcance recortado, limitaciones y mejoras |

## Dónde está cada cosa que pide la consigna

| Criterio de evaluación | Pts | Dónde verlo |
|---|---|---|
| Diseño documental y justificación | 15 | `01-diseno.md` |
| Implementación y calidad de los datos | 10 | `05-api-y-datos.md` · `datos/` · `scripts/` |
| Consultas y agregaciones | 15 | `02-consultas.md` · `consultas/consultas.js` · `evidencia/consultas_salida.txt` |
| API y repositorios | 20 | `05-api-y-datos.md` · `services/catalogo/` · `api/openapi.yaml` · Swagger en `localhost:8086` |
| Índices y consistencia | 10 | `03-indices.md` · `04-consistencia.md` · `evidencia/` |
| Análisis de datos | 10 | `05-api-y-datos.md` · `analisis/catalogo.ipynb` y `catalogo.html` |
| Docker, reproducibilidad y pruebas | 10 | `05-api-y-datos.md` · `docker-compose.yml` · `README.md` |
| Documentación y defensa individual | 10 | Este informe y el `README.md` |

## Cómo se organiza la entrega

```
services/catalogo/      El servicio en Go (código, tests, Dockerfile)
tp-mongo/
├── informe/            Este informe
├── datos/ · scripts/   Datos de prueba y scripts de creación, carga y verificación
├── consultas/          Las consultas y las mediciones de índices
├── api/                openapi.yaml, para Swagger
├── pruebas/            Requests de ejemplo, prueba de concurrencia y de esquema
├── analisis/           Notebook ejecutado, versión HTML y requirements.txt
├── evidencia/          Salidas de consultas, explain, concurrencia y esquema
├── docker-compose.yml · .env.example
└── PROYECTO.md         Decisiones y requisitos (fuente única del trabajo)
```

Para levantar el entorno y probarlo, ver el `README.md` de la raíz.
