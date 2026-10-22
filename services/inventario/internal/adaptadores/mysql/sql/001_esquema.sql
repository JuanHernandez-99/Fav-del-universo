-- Esquema de Inventario (ADR-003). Idempotente: se aplica en cada arranque.

CREATE TABLE IF NOT EXISTS stock (
  variante_id     VARCHAR(100) NOT NULL PRIMARY KEY,
  online          INT          NOT NULL DEFAULT 0,
  fisico          INT          NOT NULL DEFAULT 0,
  reservado       INT          NOT NULL DEFAULT 0,
  actualizado_en  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  -- El online y lo reservado nunca quedan negativos. El físico sí puede (RN-25).
  CONSTRAINT stock_online_no_negativo CHECK (online >= 0),
  CONSTRAINT stock_reservado_valido   CHECK (reservado >= 0 AND reservado <= online)
);

CREATE TABLE IF NOT EXISTS reservas (
  id                  CHAR(36)     NOT NULL PRIMARY KEY,
  clave_idempotencia  VARCHAR(100) NOT NULL,
  huella              CHAR(64)     NOT NULL,
  referencia          VARCHAR(100) NOT NULL DEFAULT '',
  estado              ENUM('ACTIVA','CONFIRMADA','LIBERADA','VENCIDA') NOT NULL,
  vence_en            DATETIME     NOT NULL,
  creada_en           DATETIME     NOT NULL,
  actualizada_en      DATETIME     NOT NULL,
  UNIQUE KEY reservas_clave (clave_idempotencia),
  KEY reservas_por_vencer (estado, vence_en)
);

CREATE TABLE IF NOT EXISTS reserva_items (
  reserva_id   CHAR(36)     NOT NULL,
  variante_id  VARCHAR(100) NOT NULL,
  cantidad     INT          NOT NULL,
  PRIMARY KEY (reserva_id, variante_id),
  CONSTRAINT reserva_items_reserva FOREIGN KEY (reserva_id) REFERENCES reservas (id)
);

-- Registro de todo lo que mueve stock: quién, cuándo, cuánto, origen y destino (RN-10).
CREATE TABLE IF NOT EXISTS movimientos (
  id           BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
  variante_id  VARCHAR(100) NOT NULL,
  tipo         VARCHAR(30)  NOT NULL,
  canal        VARCHAR(10)  NOT NULL,
  cantidad     INT          NOT NULL,
  referencia   VARCHAR(100) NOT NULL DEFAULT '',
  usuario      VARCHAR(100) NOT NULL DEFAULT '',
  creado_en    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY movimientos_variante (variante_id, creado_en)
);

-- Claves de idempotencia de ingresos y transferencias, con el resultado para devolverlo en un reintento.
CREATE TABLE IF NOT EXISTS operaciones (
  clave_idempotencia  VARCHAR(100) NOT NULL PRIMARY KEY,
  tipo                VARCHAR(30)  NOT NULL,
  huella              CHAR(64)     NOT NULL,
  variante_id         VARCHAR(100) NOT NULL,
  creada_en           DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Reposiciones al gimnasio pendientes de enviar (ADR-009).
CREATE TABLE IF NOT EXISTS reposiciones (
  id                  CHAR(36)     NOT NULL PRIMARY KEY,
  clave_idempotencia  VARCHAR(100) NOT NULL,
  variante_id         VARCHAR(100) NOT NULL,
  cantidad            INT          NOT NULL,
  estado              ENUM('PENDIENTE','ENVIADA','RECHAZADA') NOT NULL,
  intentos            INT          NOT NULL DEFAULT 0,
  ultimo_error        VARCHAR(500) NOT NULL DEFAULT '',
  proximo_intento     DATETIME     NOT NULL,
  usuario             VARCHAR(100) NOT NULL DEFAULT '',
  creada_en           DATETIME     NOT NULL,
  actualizada_en      DATETIME     NOT NULL,
  UNIQUE KEY reposiciones_clave (clave_idempotencia),
  KEY reposiciones_pendientes (estado, proximo_intento)
);

-- Eventos a publicar (transactional outbox, ADR-005). En esta entrega se guardan;
-- el publicador hacia RabbitMQ se suma en la entrega siguiente.
CREATE TABLE IF NOT EXISTS outbox (
  id              CHAR(36)     NOT NULL PRIMARY KEY,
  tipo            VARCHAR(50)  NOT NULL,
  datos           JSON         NOT NULL,
  correlation_id  VARCHAR(100) NOT NULL DEFAULT '',
  ocurrido_en     DATETIME     NOT NULL,
  publicado_en    DATETIME     NULL,
  KEY outbox_sin_publicar (publicado_en)
);
