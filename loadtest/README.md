# Prueba de carga

Guion de k6 para medir Inventario detrás del gateway (ADR-013).

> **Estado: sin ejecutar.** Los cuerpos se armaron leyendo `internal/adaptadores/http/http.go` (`Idempotency-Key` va en el encabezado; el ingreso lleva `varianteId`, `canal` y `cantidad`), pero **nunca se corrieron**. Si un pedido devuelve `400`, ajustar el guion.

## Correr

Con el sistema levantado (`docker compose up -d`):

```bash
docker run --rm -i -e BASE_URL=http://host.docker.internal:8080 grafana/k6 run - < loadtest/inventario.js
```

Variables: `BASE_URL` (por defecto `http://host.docker.internal:8080`), `ADMIN_KEY` (por defecto `clave-admin-local`).

## Qué mirar mientras corre

- **Prometheus** (`http://localhost:9090`): `histogram_quantile(0.95, sum by (le) (rate(http_duracion_segundos_bucket[1m])))` y `sum by (instance) (rate(http_pedidos_total[1m]))`.
- **Jaeger** (`http://localhost:16686`): las trazas más lentas.
- Registrar los resultados en el ADR-013.
