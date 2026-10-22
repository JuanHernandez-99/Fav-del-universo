-- Stock inicial de las variantes del ejemplo del contrato catalogo-pedidos-v1, para probar el flujo
-- de punta a punta con el mock de Catálogo. INSERT IGNORE: no pisa el stock si ya existe.
-- Mientras no exista el evento VarianteCreada (Catálogo), las variantes se dan de alta con un ingreso.

INSERT IGNORE INTO stock (variante_id, online, fisico, reservado) VALUES
  ('var-remera-basica-blanca-m', 10, 5, 0),
  ('var-remera-basica-negra-l',   4, 2, 0),
  ('var-jean-mom-celeste-38',     1, 3, 0);
