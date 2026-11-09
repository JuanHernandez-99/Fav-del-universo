# Prueba de carga

## Caché de Catálogo (ADR-019)

```bash
bash loadtest/medir_catalogo.sh 600    # una tasa: latencia, errores y consultas a MongoDB
bash loadtest/comparar_cache.sh        # con y sin caché, mismas tasas, unos 15 minutos
```

Para ver el efecto con una base chica, se limita la CPU de MongoDB y después se restaura. `docker update --cpus 0` **no** quita el límite, pero recrear el contenedor sí, y conserva los datos:

```bash
docker update --cpus 0.25 $(docker compose ps -q mongo-catalogo)
TASAS=600 REPETICIONES=4 bash loadtest/comparar_cache.sh
docker compose up -d --no-deps --force-recreate mongo-catalogo
```

Los resultados están en el ADR-007.

## Inventario

Guion de k6 para medir Inventario detrás del gateway (ADR-013).

> **Estado:** Una sola corrida de 60 s, para validar el guion (04/11/2026): unos 100 pedidos por segundo a Inventario, sin errores, con p95 de 21 ms y un máximo de 2,68 s. **No es el análisis de capacidad:** falta el escenario de pico, su límite principal y los costos.

## Correr

Con el sistema levantado (`docker compose up -d`):

```bash
docker run --rm -i -e BASE_URL=http://host.docker.internal:8080 grafana/k6 run - < loadtest/inventario.js
```

Variables: `BASE_URL` (por defecto `http://host.docker.internal:8080`), `ADMIN_EMAIL` y `ADMIN_PASSWORD` (por defecto, los de Docker Compose). El guion inicia sesión al empezar. El token dura 1 hora, más que la prueba.

## Qué mirar mientras corre

- **Prometheus** (`http://localhost:9090`): `histogram_quantile(0.95, sum by (le) (rate(http_duracion_segundos_bucket[1m])))` y `sum by (instance) (rate(http_pedidos_total[1m]))`.
- **Jaeger** (`http://localhost:16686`): las trazas más lentas.
- Registrar los resultados en el ADR-013.
