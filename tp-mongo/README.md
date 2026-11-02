# Catálogo de FAV · TP de MongoDB

Servicio de catálogo de una tienda de ropa: productos con variantes (talle y color), categorías y promociones, guardados en **MongoDB**, con una **API en Go**, **Swagger**, un **análisis en Jupyter** y todo el entorno en **Docker Compose**.

El informe técnico está en [`tp-mongo/informe/`](informe/00-resumen.md) y las decisiones del trabajo en [`tp-mongo/PROYECTO.md`](PROYECTO.md).

## Requisitos

| Para qué | Qué hace falta |
|---|---|
| Levantar todo (obligatorio) | **Docker Desktop** (o Docker Engine con Compose v2) |
| Prueba de concurrencia y regenerar datos | Python 3 (solo biblioteca estándar; probado con 3.13) |
| Ver el notebook ejecutado | Un navegador (`analisis/catalogo.html`) |
| Volver a ejecutar el notebook | Python y `pip install -r analisis/requirements.txt` |
| Tests de Go | Go 1.26 |

No hace falta instalar MongoDB ni Go para levantar y probar el proyecto.

## Inicio rápido

Todos los comandos se corren desde la carpeta `tp-mongo/`. Sirven igual en PowerShell, Git Bash, macOS y Linux.

```bash
cd tp-mongo
cp .env.example .env              # en PowerShell: copy .env.example .env
docker compose up -d --build      # MongoDB, API y Swagger UI
docker compose run --rm cargador  # crea colecciones, esquema e índices, carga los datos y verifica
```

La carga termina con `verificación OK`. Después:

| Qué | Dónde |
|---|---|
| API | http://localhost:8085 (por ejemplo, `/healthz` y `/v1/admin/productos`) |
| **Swagger UI** (probar la API) | http://localhost:8086 |
| MongoDB (Compass, notebook) | `localhost:27017` |

La primera vez descarga las imágenes y compila el servicio. La API reintenta la conexión mientras MongoDB arranca.

### Variables (`.env`)

`.env.example` trae valores de ejemplo, sin credenciales reales. Copialo a `.env` y cambiá la clave si querés. El `.env` no se entrega ni se sube al repositorio.

| Variable | Para qué |
|---|---|
| `MONGO_INITDB_ROOT_USERNAME` y `MONGO_INITDB_ROOT_PASSWORD` | Usuario root de MongoDB, que se crea la primera vez que arranca con el volumen vacío |
| `MONGO_DB` | Nombre de la base (`catalogo`) |

La API se conecta por el **nombre del servicio** (`mongo-catalogo:27017`), nunca por `localhost`.

## Iniciar, detener y reiniciar

| Acción | Comando |
|---|---|
| Iniciar | `docker compose up -d` |
| Detener (conserva los datos) | `docker compose down` |
| **Reiniciar de cero** (borra el volumen) | `docker compose down -v`, luego `up -d --build` y `run --rm cargador` |
| Volver a cargar sin borrar el volumen | `docker compose run --rm cargador` (vacía las colecciones y reimporta, se puede repetir) |
| Cargar solo los 2 productos del contrato de FAV | `docker compose run --rm -e CARGA=minimo cargador` |
| Ver logs de la API | `docker compose logs catalogo` |

## Qué se carga

279 documentos: 13 categorías, 252 productos (881 variantes) y 14 promociones, con casos límite (despublicados, sin fotos, talle único, nombres con ñ y tildes, promociones vencidas y futuras). Los datos están en `datos/` y los genera `datos/generar.py` con semilla fija. Los scripts de `scripts/` son `01` (colecciones, esquema de validación e índices), `02` (carga) y `03` (verificación).

## Probar la API

1. Abrí **http://localhost:8086**.
2. `GET /v1/admin/productos` trae los 20 primeros de 252. Probá un filtro a la vez (`categoria=remeras`, `talle=M&color=Negro`, `precioMin=10000&precioMax=30000`, `q=algodón`).
3. `POST /v1/admin/productos` con el ejemplo `valido`: crea el producto, que nace con `publicado: false`.
4. `POST /v1/admin/productos/{id}/variantes` con `{"talle":"S","color":"Blanco"}`: `201` la primera vez y **`409`** la segunda.
5. `PUT` con la `version` actual: `200`. Repetir el mismo `PUT`: **`409`** (conflicto de versión).
6. `DELETE`: `204` (despublica; no borra). Repetirlo también da `204`.

También hay requests de ejemplo en `pruebas/` (`crear_producto_valido.json` y `crear_producto_invalido.json`).

## Consultas

Las 10 consultas (más una de texto) están comentadas en `consultas/consultas.js`, con objetivo, comando y resultado esperado.

```bash
docker compose exec mongo-catalogo sh -c 'mongosh --quiet -u "$MONGO_INITDB_ROOT_USERNAME" -p "$MONGO_INITDB_ROOT_PASSWORD" --authenticationDatabase admin catalogo --file /consultas/consultas.js'
```

La misma forma sirve para `indices_explain.js` (mediciones de índices) y para `pruebas/validacion_esquema.js`, cambiando el archivo.

**Para verlas con interfaz gráfica:** MongoDB Compass, con la URI `mongodb://USUARIO:CLAVE@localhost:27017/?authSource=admin` (usuario y clave de tu `.env`).

## Pruebas

| Prueba | Comando | Resultado esperado |
|---|---|---|
| **Concurrencia**: 20 pedidos simultáneos con la misma variante | `python pruebas/concurrencia_variante.py` (con la API levantada) | Exactamente un `201` y diecinueve `409`; `RESULTADO: OK`. Guarda `evidencia/concurrencia_variante.txt` |
| **Esquema de la base**: inserciones inválidas | `docker compose exec` con `--file /pruebas/validacion_esquema.js` (misma forma que las consultas) | Siete líneas `rechazado OK` |
| **Ver el esquema guardado en MongoDB** | `docker compose exec` con `--file /pruebas/ver_esquema.js` | El validador `$jsonSchema` de `productos` y `promociones` |
| **Tests de Go** | `cd ../services/catalogo && go test ./...` | `ok` en cada paquete |

La prueba de concurrencia crea un producto nuevo cada vez, así que se puede repetir.

## Análisis de datos

- **Sin instalar nada:** abrí `analisis/catalogo.html` en el navegador. Es el notebook ya ejecutado, con 4 indicadores, gráficos, interpretación y conclusión.
- **Para ejecutarlo de nuevo**, con la base levantada y cargada:
  ```bash
  pip install -r analisis/requirements.txt
  jupyter lab analisis/catalogo.ipynb
  ```
  El notebook toma las credenciales de `tp-mongo/.env` y se conecta a `localhost:27017`.

## Estructura de lo entregado

```
services/catalogo/      El servicio en Go: handler, service, repository, tests y Dockerfile
tp-mongo/
├── informe/            Informe técnico (empezar por 00-resumen.md)
├── datos/ · scripts/   Datos de prueba y scripts de creación, carga y verificación
├── consultas/          Las 10 consultas y las mediciones de índices
├── api/openapi.yaml    Especificación para Swagger
├── pruebas/            Requests de ejemplo, concurrencia y esquema
├── analisis/           Notebook ejecutado, versión HTML y requirements.txt
├── evidencia/          Salidas de consultas, explain, concurrencia y esquema
├── docker-compose.yml · .env.example
└── PROYECTO.md         Decisiones y requisitos
```

## Problemas frecuentes

| Síntoma | Causa y solución |
|---|---|
| `failed to connect to the docker API` | Docker Desktop no está abierto. Abrilo y esperá a que diga que está corriendo |
| `port is already allocated` | Otro programa usa el 27017, el 8085 o el 8086. Cerralo, o cambiá el puerto en `docker-compose.yml` |
| La API no conecta o el cargador pide autenticación | Cambiaste el `.env` después de crear el volumen: el usuario de MongoDB se crea solo la primera vez. Hacé `docker compose down -v` y volvé a empezar |
| "Try it out" de Swagger falla con `Failed to fetch` | Abrí Swagger en `http://localhost:8086`, no en otra dirección: la API solo permite ese origen (variable `CORS_ORIGENES`) |
| Los números del informe no coinciden | Se crearon productos de prueba. `docker compose run --rm cargador` vuelve a los 252 originales |

## Integración con FAV

Este servicio es el mismo que usará FAV. La integración al Compose general, el gateway, la consulta por lote, el precio vigente y las promociones por API quedan para la etapa siguiente (ver `informe/06-conclusiones.md`).
