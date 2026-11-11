#!/bin/sh
# Usuarios de mysql-logs (ADR-020). Lo corre la imagen de MySQL solo al crear el volumen, después de 001.
# Los servicios solo insertan y Grafana solo lee: ninguno de los dos puede borrar ni cambiar logs.
# Las contraseñas llegan por variables de entorno (docker-compose.yml), nunca escritas acá.
set -e
mysql --protocol=socket -uroot -p"$MYSQL_ROOT_PASSWORD" <<SQL
CREATE USER IF NOT EXISTS 'logs_escritor'@'%' IDENTIFIED BY '${LOGS_MYSQL_ESCRITOR_PASSWORD}';
GRANT INSERT ON \`${MYSQL_DATABASE}\`.app_logs TO 'logs_escritor'@'%';
CREATE USER IF NOT EXISTS 'logs_lector'@'%' IDENTIFIED BY '${LOGS_MYSQL_LECTOR_PASSWORD}';
GRANT SELECT ON \`${MYSQL_DATABASE}\`.app_logs TO 'logs_lector'@'%';
SQL
