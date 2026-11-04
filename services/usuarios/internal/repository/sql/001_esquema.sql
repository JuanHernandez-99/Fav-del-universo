-- Esquema de Usuarios (ADR-003). Idempotente: se aplica en cada arranque.

CREATE TABLE IF NOT EXISTS usuarios (
  id          CHAR(36)     NOT NULL PRIMARY KEY,
  email       VARCHAR(254) NOT NULL,
  hash        VARCHAR(100) NOT NULL, -- bcrypt: nunca la contraseña (HU-18.2)
  rol         ENUM('comprador','administrador') NOT NULL,
  creado_en   DATETIME     NOT NULL,
  UNIQUE KEY usuarios_email (email) -- la base garantiza el email único (HU-18.1)
);
