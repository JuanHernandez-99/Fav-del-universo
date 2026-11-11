-- Logs de los servicios (ADR-020): modelo de la cátedra (track_id, stage, level, message, created_at) más la
-- réplica que escribió la línea y sus atributos. Lo escriben los servicios con GORM y lo lee Grafana.
-- MySQL lo aplica solo la primera vez, al crear el volumen de mysql-logs.
CREATE TABLE IF NOT EXISTS app_logs (
  id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  track_id   VARCHAR(100)    NOT NULL DEFAULT '',
  stage      VARCHAR(30)     NOT NULL,
  instancia  VARCHAR(64)     NOT NULL DEFAULT '',
  level      VARCHAR(10)     NOT NULL,
  message    TEXT            NOT NULL,
  atributos  JSON            NULL,
  created_at DATETIME(3)     NOT NULL,
  PRIMARY KEY (id),
  INDEX idx_app_logs_track_id (track_id),
  INDEX idx_app_logs_created_at (created_at),
  INDEX idx_app_logs_stage_level (stage, level)
);
