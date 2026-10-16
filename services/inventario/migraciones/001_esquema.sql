-- Esquema de la base de Inventario (MySQL 8).
-- Se ejecuta solo la primera vez que arranca el contenedor de la base
-- (docker-entrypoint-initdb.d). Para recrearla: docker compose down -v.

CREATE TABLE IF NOT EXISTS stock (
  variante_id    VARCHAR(64) NOT NULL,
  canal          VARCHAR(10) NOT NULL,           -- ONLINE o FISICO
  cantidad       INT         NOT NULL DEFAULT 0, -- el físico puede quedar negativo (RN-25)
  reservado      INT         NOT NULL DEFAULT 0, -- unidades en reservas activas
  actualizado_en DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (variante_id, canal),
  CONSTRAINT chk_stock_canal CHECK (canal IN ('ONLINE', 'FISICO')),
  CONSTRAINT chk_stock_reservado CHECK (reservado >= 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS reservas (
  id             CHAR(36)     NOT NULL,
  estado         VARCHAR(12)  NOT NULL,          -- ACTIVA, CONFIRMADA, LIBERADA, VENCIDA
  referencia     VARCHAR(100) NOT NULL DEFAULT '',
  correlation_id VARCHAR(100) NOT NULL DEFAULT '', -- del pedido que la creó; viaja en ReservaVencida
  creada_en      DATETIME(3)  NOT NULL,
  vence_en       DATETIME(3)  NOT NULL,
  actualizada_en DATETIME(3)  NOT NULL,
  PRIMARY KEY (id),
  KEY idx_reservas_vencimiento (estado, vence_en),
  CONSTRAINT chk_reservas_estado CHECK (estado IN ('ACTIVA', 'CONFIRMADA', 'LIBERADA', 'VENCIDA'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS reserva_items (
  reserva_id  CHAR(36)    NOT NULL,
  variante_id VARCHAR(64) NOT NULL,
  cantidad    INT         NOT NULL,
  PRIMARY KEY (reserva_id, variante_id),
  CONSTRAINT fk_items_reserva FOREIGN KEY (reserva_id) REFERENCES reservas (id),
  CONSTRAINT chk_items_cantidad CHECK (cantidad > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Una clave de idempotencia por reserva: un reintento con la misma clave
-- devuelve la reserva original en lugar de crear otra.
CREATE TABLE IF NOT EXISTS claves_idempotencia (
  clave      VARCHAR(100) NOT NULL,
  huella     CHAR(64)     NOT NULL, -- resumen del pedido, para detectar la misma clave con otro pedido
  reserva_id CHAR(36)     NOT NULL,
  creada_en  DATETIME(3)  NOT NULL,
  PRIMARY KEY (clave)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Transactional outbox: el evento se guarda en la misma transacción que el cambio
-- y un proceso aparte lo publica en RabbitMQ (ADR-005).
CREATE TABLE IF NOT EXISTS outbox (
  id             CHAR(36)     NOT NULL,
  tipo           VARCHAR(60)  NOT NULL,
  correlation_id VARCHAR(100) NOT NULL DEFAULT '',
  payload        JSON         NOT NULL, -- el sobre completo del evento, tal como se publica
  creado_en      DATETIME(3)  NOT NULL,
  publicado_en   DATETIME(3)  NULL,
  PRIMARY KEY (id),
  KEY idx_outbox_pendientes (publicado_en, creado_en)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
