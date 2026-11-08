-- Estados del contrato real del gimnasio (ADR-018) e id que genera el gimnasio. Se aplica en cada arranque:
-- todo es idempotente. El bloqueo evita que dos réplicas agreguen la columna a la vez.
DO GET_LOCK('inventario_reposiciones_003', 30);

-- De ENUM a texto: sumar un estado no requiere otra migración.
ALTER TABLE reposiciones MODIFY estado VARCHAR(30) NOT NULL;
UPDATE reposiciones SET estado = 'PENDIENTE_CONFIRMACION' WHERE estado = 'PENDIENTE';
UPDATE reposiciones SET estado = 'CONFIRMADA' WHERE estado = 'ENVIADA';

SET @sin_columna := (SELECT COUNT(*) = 0 FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'reposiciones' AND column_name = 'id_gimnasio');
SET @agregar := IF(@sin_columna, 'ALTER TABLE reposiciones ADD COLUMN id_gimnasio VARCHAR(64) NOT NULL DEFAULT ''''', 'DO 0');
PREPARE agregar_columna FROM @agregar;
EXECUTE agregar_columna;
DEALLOCATE PREPARE agregar_columna;

DO RELEASE_LOCK('inventario_reposiciones_003');
