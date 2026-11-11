# Despliegue en la nube

Una VM Linux con Docker Compose. Se usa el `docker-compose.yml` local más [`docker-compose.cloud.yml`](../docker-compose.cloud.yml), que:

- **Puertos:** cierra los puertos y deja a Caddy como única entrada (80 y 443, con HTTPS).
- **Secretos:** los exige, sin valores por defecto.
- **Reinicio:** reinicia todo solo.

```
Internet ──HTTPS──► Caddy :443 ──► gateway :8080 ──► servicios      (red interna de Docker)
                       └── /checkout/pref-… ──► simulador de Mercado Pago (solo la página de pago)
```

| Archivo | Para qué |
|---|---|
| [`Caddyfile`](Caddyfile) | HTTPS con Let's Encrypt y qué rutas pasan |
| [`proxy-confiable.conf`](proxy-confiable.conf) | El gateway toma la IP real del cliente solo si viene de Caddy (límite de inicios de sesión por IP) |
| [`arrancar.sh`](arrancar.sh) | Revisa el `.env` y levanta todo |
| [`verificar.sh`](verificar.sh) | Prueba desde afuera: HTTPS, claves, pedido con idempotencia, rutas y puertos cerrados |

## Qué queda accesible

| Desde | Qué |
|---|---|
| Internet | Solo `https://DOMINIO`: tienda, panel, capacidad publicada (`/v1/…`) y la página de pago simulada |
| La VM (túnel SSH) | Grafana `3000` (con el tablero "FAV · Logs"), Mailpit `8025`, Prometheus `9090`, Alertmanager `9093`, Elasticsearch `9200` |
| Nadie de afuera | MySQL (también `mysql-logs`), MongoDB, RabbitMQ, los servicios, el gateway directo y los simuladores |

## 1. Firewall de Azure

En el grupo de seguridad de red (NSG) de la VM, permitir solo la entrada a **22** (SSH, idealmente solo desde tu IP), **80** y **443**.

Esto es una segunda capa. Docker publica sus puertos por fuera de `ufw`, así que **lo que protege de verdad es que el Compose no los publique**.

## 2. Código y `.env`

```bash
git clone <repo> fav && cd fav
git checkout <rama>          # main, una vez que tenga el despliegue
cp .env.example .env && chmod 600 .env
```

En `.env`:

1. Descomentar `COMPOSE_FILE`, `DOMINIO` y, para la primera prueba, `ACME_CA` (certificado de prueba, ver paso 4).
2. Generar una contraseña propia para cada secreto. En hexadecimal, porque la de RabbitMQ va dentro de una URL:

   ```bash
   for v in INVENTARIO_DB_ROOT_PASSWORD INVENTARIO_DB_PASSWORD VENTAS_DB_ROOT_PASSWORD VENTAS_DB_PASSWORD \
            USUARIOS_DB_ROOT_PASSWORD USUARIOS_DB_PASSWORD MONGO_INITDB_ROOT_PASSWORD RABBITMQ_DEFAULT_PASS \
            LOGS_MYSQL_ROOT_PASSWORD LOGS_MYSQL_ESCRITOR_PASSWORD LOGS_MYSQL_LECTOR_PASSWORD \
            GRAFANA_ADMIN_PASSWORD MERCADOPAGO_WEBHOOK_SECRET ADMIN_PASSWORD; do
     sed -i "s/^$v=.*/$v=$(openssl rand -hex 24)/" .env
   done
   ```

3. A mano:
   - `ADMIN_EMAIL`.
   - `CAPACIDAD_API_KEY` y `CAPACIDAD_API_KEY_GIMNASIO`: distintas entre sí. Se les pasan a los grupos por un canal privado. De 32 caracteres (`openssl rand -hex 16`): con claves más largas el gateway no arrancó (`could not build map_hash`, límite de nginx).
   - `GIMNASIO_BASE_URL` y `GIMNASIO_API_KEY`: solo cuando el gimnasio dé las reales. Sin ellas, Inventario usa el simulador.
   - **En una VM que ya estaba andando antes de ADR-020:** agregar al `.env` las tres `LOGS_MYSQL_*_PASSWORD` (con `openssl rand -hex 24` cada una) antes del `git pull && docker compose up -d --build`. Sin ellas, Compose corta con `falta LOGS_MYSQL_...`. El volumen de `mysql-logs` es nuevo, así que toman esos valores.

4. Guardar una copia del `.env` fuera de la VM, en un lugar privado. Sin ella, un respaldo de las bases no se puede usar con otras contraseñas.

> **Las contraseñas de MySQL (también las tres de `mysql-logs`), MongoDB y Grafana se fijan la primera vez que se crea cada volumen.** Cambiarlas después en `.env` no cambia la base, y los servicios dejan de conectarse. Por eso el `.env` va completo antes del primer arranque.

## 3. Arrancar

```bash
bash desplegar/arrancar.sh
```

Revisa que el `.env` no tenga los valores públicos de `.env.example` y que las dos claves de los grupos sean distintas. Después levanta todo y muestra el estado.

Si falta un secreto, Compose corta con `required variable X is missing a value: falta X en .env`.

## 4. Certificado: primero el de prueba

Let's Encrypt limita cuántos certificados reales se pueden pedir por semana. Por eso la primera vez se usa su entorno de prueba (staging), con `ACME_CA` descomentada:

1. Con `ACME_CA` de staging, revisar que Caddy obtenga el certificado:

   ```bash
   docker compose logs caddy | grep -i "certificate obtained"
   ```

   El navegador va a avisar que no confía en él: es lo esperado.
2. Pasar al certificado real: borrar la línea `ACME_CA` de `.env` y ejecutar `docker compose up -d caddy`.

## 5. Verificar desde afuera

Desde tu computadora, no desde la VM:

```bash
DOMINIO=fav-ucc.chilecentral.cloudapp.azure.com CAPACIDAD_API_KEY=... CAPACIDAD_API_KEY_GIMNASIO=... \
  bash desplegar/verificar.sh          # con el certificado de prueba, agregar INSEGURO=1
```

Tiene que terminar en `RESULTADO: todo OK`. Hace un pedido real de 1 unidad: aparta stock 48 horas.

Falta probar a mano una compra desde el navegador:

1. Agregar algo al carrito.
2. Comprar con Mercado Pago.
3. Tocar **Pagar** en la página del simulador.
4. Ver los mails en Mailpit (paso 6).

## 6. Paneles por túnel SSH

```bash
ssh -L 3000:localhost:3000 -L 8025:localhost:8025 -L 9090:localhost:9090 usuario@DOMINIO
```

Mientras la sesión está abierta: Grafana en http://localhost:3000, mails y alertas en http://localhost:8025, etc. Los mails no salen a internet (ADR-014).

## 7. Reinicio de la VM

Todo tiene `restart: unless-stopped`, y Docker arranca con la VM. Comprobar una vez que Docker esté habilitado:

```bash
systemctl is-enabled docker     # tiene que decir enabled
```

Para probarlo:

1. Desasignar la VM desde Azure y volver a iniciarla.
2. Correr `docker compose ps`: todos `Up`, y los que tienen healthcheck, `(healthy)`. `catalogo-init` y `usuarios-init` quedan `Exited (0)`: son tareas de arranque.

## 8. Operación

| Qué | Cómo |
|---|---|
| Estado y logs | `docker compose ps`, `docker compose logs -f gateway` |
| Actualizar el código | `git pull && docker compose up -d --build` |
| Apagar sin perder datos | `docker compose down` (**nunca `-v`**: borra las bases y los certificados) |
| Dar de baja temporalmente | Desasignar la VM en Azure: deja de cobrar el cómputo; el disco y la IP siguen costando |
| Cambiar algo y subirlo | Ver la sección 9 |
| Limitar los logs de Docker | En `/etc/docker/daemon.json`: `{"log-driver":"json-file","log-opts":{"max-size":"10m","max-file":"3"}}` y `sudo systemctl restart docker`. Si no, crecen sin límite en el disco |

## 9. Cambiar algo y subirlo a la VM

Dos terminales distintas: la **PC** (prompt `PS F:\...>`) y la **VM** (prompt `azureuser@vm-fav:~/fav$`). Se entra a la VM con `ssh -i "$HOME\.ssh\<clave>.pem" azureuser@DOMINIO` y se sale con `exit`. Los comandos de `docker compose` se corren dentro de `~/fav`, porque ahí está el `.env`.

| Qué cambió | PC | VM (`cd ~/fav`) |
|---|---|---|
| Código | `git push` | `git pull && docker compose up -d --build` |
| Una variable del `.env` (por ejemplo `GIMNASIO_API_KEY`) | — | `nano .env` y `docker compose up -d` |
| Ver el resultado | — | `docker compose ps` y `docker compose logs -f <servicio>` |

- `up -d` recrea solo los servicios afectados. No hay que repetir `arrancar.sh`, ni las contraseñas, ni el certificado.
- Después de cambiar el `.env` de la VM, bajar una copia a la PC: `scp -i "$HOME\.ssh\<clave>.pem" azureuser@DOMINIO:~/fav/.env "$HOME\Documents\fav-vm.env"`.
