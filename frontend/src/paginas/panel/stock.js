// Lógica de la pantalla de stock (HU-06 a HU-09), sin React. Las reglas las aplica Inventario.

import { TALLES } from '../../dominio.js';

export const MOTIVOS = [['PERDIDA', 'Pérdida'], ['FALLA', 'Falla'], ['CORRECCION_CONTEO', 'Corrección de conteo']]; // HU-08.4 [propuesta]
export const CANALES_STOCK = [['ONLINE', 'Online'], ['FISICO', 'Físico (local)']];

// Une las variantes de Catálogo con el stock de Inventario. Una variante sin fila en Inventario todavía no tuvo
// ingresos: se muestra en 0 (la parte D del backend la registraría al crearla).
export function filasDeStock(variantes, stocks) {
  const porId = Object.fromEntries(stocks.map((s) => [s.varianteId, s]));
  return [...variantes]
    .sort((a, b) => a.color.localeCompare(b.color) || TALLES.indexOf(a.talle) - TALLES.indexOf(b.talle))
    .map((v) => {
      const s = porId[v.varianteId];
      return {
        ...v,
        registrada: Boolean(s),
        online: s?.online ?? 0, reservado: s?.reservado ?? 0, disponibleOnline: s?.disponibleOnline ?? 0,
        fisico: s?.fisico ?? 0, stockACorregir: s?.stockACorregir ?? false,
      };
    });
}

// El pedido a Inventario según el tipo de movimiento. En el ajuste la cantidad lleva signo: negativa resta.
export function pedidoDe({ tipo, varianteId, canal, origen, cantidad, sentido, motivo, nota }) {
  const n = Number(cantidad);
  if (tipo === 'INGRESO') return { ruta: 'ingresos', cuerpo: { varianteId, canal, cantidad: n } };
  if (tipo === 'TRANSFERENCIA') return { ruta: 'transferencias', cuerpo: { varianteId, origen, cantidad: n } };
  return { ruta: 'ajustes', cuerpo: { varianteId, canal, cantidad: sentido === 'RESTAR' ? -n : n, motivo, nota: nota.trim() } };
}

export const cantidadValida = (texto) => /^\d+$/.test(String(texto).trim()) && Number(texto) > 0;

// Explica un rechazo de Inventario. faltantes trae cuánto había disponible en el canal de origen.
export function explicarRechazo(error, tipo) {
  if (error.codigo === 'STOCK_INSUFICIENTE') {
    const disponible = error.faltantes?.[0]?.disponible;
    if (tipo === 'TRANSFERENCIA') return `No alcanza: en el canal de origen hay ${disponible} disponibles (sin contar las reservadas, RN-10).`;
    return `No alcanza: hay ${disponible} disponibles. El online no puede quedar por debajo de lo reservado y el físico no puede quedar negativo (HU-08).`;
  }
  if (error.codigo === 'VARIANTE_INEXISTENTE') return 'Esa variante todavía no tiene stock en Inventario: primero cargá un ingreso.';
  if (error.codigo === 'PEDIDO_INVALIDO') return (error.errores ?? []).map((c) => `${c.campo}: ${c.mensaje}`).join('. ') || 'Revisá los datos.';
  return 'No pudimos registrar el movimiento en este momento. Probá de nuevo: no se va a aplicar dos veces.';
}
