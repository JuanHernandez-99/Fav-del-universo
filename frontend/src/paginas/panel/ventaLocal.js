// Lógica de la venta del local (HU-17), sin React.

export const MEDIOS_LOCAL = [['EFECTIVO', 'Efectivo'], ['TRANSFERENCIA', 'Transferencia'], ['MERCADO_PAGO', 'Mercado Pago']]; // RN-18
export const MAXIMO_LINEAS = 20; // el máximo de variantes por venta que acepta Ventas

// Cuánto stock físico queda en cada línea después de la venta. Puede quedar negativo: la venta se registra igual
// y la variante pasa a "stock a corregir" (RN-06, RN-25). fisico: { varianteId: unidades }, o null si no se sabe.
export function fisicoDespues(lineas, fisico) {
  if (!fisico) return {};
  return Object.fromEntries(lineas.map((l) => [l.varianteId, (fisico[l.varianteId] ?? 0) - l.cantidad]));
}

// El monto se escribe en pesos, con hasta 2 decimales (se acepta coma). Devuelve null si no es válido.
export function leerMonto(texto) {
  const limpio = String(texto).trim().replace(',', '.');
  if (!/^\d+(\.\d{1,2})?$/.test(limpio)) return null;
  return Number(limpio);
}

// "var-remera:2,var-jean:1", el formato de la sugerencia de Ventas.
export const lineasParaSugerencia = (lineas) => lineas.map((l) => `${l.varianteId}:${l.cantidad}`).join(',');
