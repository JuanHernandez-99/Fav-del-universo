// Qué puede hacer el administrador con una venta. Las mismas reglas las aplica Ventas: esto solo decide
// qué botones se muestran.

export const CANALES = { ONLINE: 'Online', LOCAL: 'Local', GRUPO_EXTERNO: 'Grupo externo' };
export const MEDIOS_CORTOS = { MERCADO_PAGO: 'Mercado Pago', TRANSFERENCIA: 'Transferencia', EFECTIVO: 'Efectivo' };

// Una transferencia se confirma en compras online y pedidos de grupos externos (HU-15). Si la venta ya venció
// o se canceló, el pago llegó tarde y queda A verificar (HU-15.2, RN-14).
export function accionesDe(venta) {
  const acciones = [];
  const esperaTransferencia = venta.pago.medio === 'TRANSFERENCIA' && venta.canal !== 'LOCAL';
  if (esperaTransferencia && venta.estado === 'PENDIENTE_DE_PAGO') acciones.push('CONFIRMAR');
  if (esperaTransferencia && ['VENCIDA', 'CANCELADA'].includes(venta.estado)) acciones.push('CONFIRMAR_TARDE');
  if (venta.estado === 'PAGADA') acciones.push('AL_CORREO');
  if (venta.estado === 'ENTREGADA_AL_CORREO') acciones.push('ENTREGADA');
  return acciones;
}

export const FILTROS = [
  ['', 'Todas'], ['PENDIENTE_DE_PAGO', 'Pendientes de pago'], ['A_VERIFICAR', 'A verificar'], ['PAGADA', 'Pagadas'],
  ['ENTREGADA_AL_CORREO', 'En el correo'], ['ENTREGADA', 'Entregadas'], ['REGISTRADA', 'Del local'],
  ['CANCELADA', 'Canceladas'], ['VENCIDA', 'Vencidas'],
];
