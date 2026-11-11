-- Track id de la venta (ADR-020): el del pedido que la creó. Sus tareas y mails lo copian, así el pago, la
-- confirmación en Inventario y los mails se ven en los logs con el mismo id que la compra.
-- Se aplica en cada arranque: todo es idempotente. El bloqueo evita que dos réplicas agreguen la columna a la vez.
DO GET_LOCK('ventas_track_id_002', 30);

SET @agregar := IF((SELECT COUNT(*) = 0 FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'ventas' AND column_name = 'track_id'),
  'ALTER TABLE ventas ADD COLUMN track_id VARCHAR(100) NOT NULL DEFAULT ''''', 'DO 0');
PREPARE agregar_columna FROM @agregar;
EXECUTE agregar_columna;
DEALLOCATE PREPARE agregar_columna;

SET @agregar := IF((SELECT COUNT(*) = 0 FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'tareas' AND column_name = 'track_id'),
  'ALTER TABLE tareas ADD COLUMN track_id VARCHAR(100) NOT NULL DEFAULT ''''', 'DO 0');
PREPARE agregar_columna FROM @agregar;
EXECUTE agregar_columna;
DEALLOCATE PREPARE agregar_columna;

SET @agregar := IF((SELECT COUNT(*) = 0 FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'mails' AND column_name = 'track_id'),
  'ALTER TABLE mails ADD COLUMN track_id VARCHAR(100) NOT NULL DEFAULT ''''', 'DO 0');
PREPARE agregar_columna FROM @agregar;
EXECUTE agregar_columna;
DEALLOCATE PREPARE agregar_columna;

DO RELEASE_LOCK('ventas_track_id_002');
