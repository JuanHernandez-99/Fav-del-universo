-- Esquema de Ventas (ADR-003). Idempotente: se aplica en cada arranque. Los montos van en centavos.

CREATE TABLE IF NOT EXISTS ventas (
  id                  CHAR(36)     NOT NULL PRIMARY KEY,
  clave_idempotencia  VARCHAR(120) NOT NULL,
  huella              CHAR(64)     NOT NULL,
  canal               ENUM('ONLINE','LOCAL','GRUPO_EXTERNO') NOT NULL,
  estado              ENUM('PENDIENTE_DE_PAGO','PAGADA','ENTREGADA_AL_CORREO','ENTREGADA','CANCELADA','VENCIDA','A_VERIFICAR','REGISTRADA') NOT NULL,
  medio               ENUM('MERCADO_PAGO','TRANSFERENCIA','EFECTIVO') NOT NULL,
  comprador_id        VARCHAR(100) NOT NULL DEFAULT '',
  email               VARCHAR(254) NOT NULL DEFAULT '',
  referencia_externa  VARCHAR(100) NOT NULL DEFAULT '',
  reserva_id          VARCHAR(36)  NOT NULL DEFAULT '',
  total               BIGINT       NOT NULL,
  url_pago            VARCHAR(500) NOT NULL DEFAULT '',
  pago_aprobado_en    DATETIME     NULL,
  vence_en            DATETIME     NULL,
  creada_en           DATETIME     NOT NULL,
  actualizada_en      DATETIME     NOT NULL,
  UNIQUE KEY ventas_clave (clave_idempotencia),
  KEY ventas_por_estado (estado, creada_en),
  KEY ventas_por_comprador (comprador_id, creada_en)
);

-- Copia de los datos de la prenda al vender: la venta conserva su precio aunque el catálogo cambie (HU-05.1).
CREATE TABLE IF NOT EXISTS venta_items (
  venta_id     CHAR(36)     NOT NULL,
  variante_id  VARCHAR(100) NOT NULL,
  nombre       VARCHAR(200) NOT NULL DEFAULT '',
  talle        VARCHAR(20)  NOT NULL DEFAULT '',
  color        VARCHAR(50)  NOT NULL DEFAULT '',
  cantidad     INT          NOT NULL,
  precio       BIGINT       NOT NULL,
  PRIMARY KEY (venta_id, variante_id),
  CONSTRAINT venta_items_venta FOREIGN KEY (venta_id) REFERENCES ventas (id)
);

-- Pagos ya procesados: el id único hace que un aviso repetido no tenga efecto (HU-11.6).
CREATE TABLE IF NOT EXISTS pagos (
  pago_id      VARCHAR(100) NOT NULL PRIMARY KEY,
  venta_id     CHAR(36)     NOT NULL,
  creado_en    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Lo que Ventas le debe a Inventario: se guarda con el cambio de la venta (ADR-005, punto 4).
CREATE TABLE IF NOT EXISTS tareas (
  id               CHAR(36)     NOT NULL PRIMARY KEY,
  venta_id         CHAR(36)     NOT NULL,
  tipo             ENUM('CONFIRMAR','LIBERAR','DESCONTAR') NOT NULL,
  estado           ENUM('PENDIENTE','COMPLETADA') NOT NULL DEFAULT 'PENDIENTE',
  intentos         INT          NOT NULL DEFAULT 0,
  ultimo_error     VARCHAR(500) NOT NULL DEFAULT '',
  proximo_intento  DATETIME     NOT NULL,
  creada_en        DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY tareas_pendientes (estado, proximo_intento)
);

-- Registro de mails: se guardan con el cambio de estado y se envían después (ADR-005, punto 6).
CREATE TABLE IF NOT EXISTS mails (
  id               CHAR(36)     NOT NULL PRIMARY KEY,
  venta_id         CHAR(36)     NOT NULL,
  tipo             VARCHAR(30)  NOT NULL,
  destinatario     VARCHAR(254) NOT NULL,
  asunto           VARCHAR(200) NOT NULL,
  cuerpo           TEXT         NOT NULL,
  estado           ENUM('PENDIENTE','ENVIADO','FALLIDO') NOT NULL DEFAULT 'PENDIENTE',
  intentos         INT          NOT NULL DEFAULT 0,
  ultimo_error     VARCHAR(500) NOT NULL DEFAULT '',
  proximo_intento  DATETIME     NOT NULL,
  creado_en        DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY mails_pendientes (estado, proximo_intento)
);

-- Mensajes ya consumidos: se registran en la misma transacción que su efecto (ADR-005, punto 5).
CREATE TABLE IF NOT EXISTS mensajes_procesados (
  mensaje_id    VARCHAR(100) NOT NULL PRIMARY KEY,
  procesado_en  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
);
