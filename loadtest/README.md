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

## Pico de la tienda (ADR-013)

Compradores por el gateway: fichas, listados, búsqueda, unidades disponibles y compras (`tienda.js`). El script recorre tasas de 60 s y muestra, por tasa, latencia por operación, errores, esperas por bloqueo en MySQL de Inventario y la CPU de los contenedores:

```bash
bash loadtest/pico_tienda.sh                      # 50 a 1200 pedidos por segundo, unos 9 minutos
TASAS="600 800" bash loadtest/pico_tienda.sh
docker run --rm -i -e TASA=100 grafana/k6 run - < loadtest/tienda.js   # una sola tasa
```

Necesita Docker y Node. Deja compras de prueba y suma stock a 10 variantes. Resultados y análisis en el ADR-013.

## Inventario

Guion de k6 para medir solo Inventario detrás del gateway. Es el escenario de la Entrega 2, cuando Inventario era el único servicio real; el pico de la tienda lo reemplaza para el análisis de capacidad.

Con el sistema levantado (`docker compose up -d`):

```bash
docker run --rm -i -e BASE_URL=http://host.docker.internal:8080 grafana/k6 run - < loadtest/inventario.js
```

Variables: `BASE_URL` (por defecto `http://host.docker.internal:8080`), `ADMIN_EMAIL` y `ADMIN_PASSWORD` (por defecto, los de Docker Compose). El guion inicia sesión al empezar. El token dura 1 hora, más que la prueba.

## Qué mirar mientras corre

- **Prometheus** (`http://localhost:9090`): `histogram_quantile(0.95, sum by (le) (rate(http_duracion_segundos_bucket[1m])))` y `sum by (instance) (rate(http_pedidos_total[1m]))`.
- **Grafana**, tablero "FAV · Logs": advertencias y errores por servicio durante la carga. Para comparar con los logs en MySQL apagados, `LOGS_MYSQL_ACTIVO=false docker compose up -d catalogo inventario ventas usuarios` ([ADR-020](../docs/adr/ADR-020-logs-y-track-id-modelo-catedra.md)).
- Registrar los resultados en el ADR-013.
