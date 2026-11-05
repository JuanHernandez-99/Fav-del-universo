import { describe, expect, it } from 'vitest';
import { accionesDe } from './ventas.js';

const venta = (estado, medio = 'TRANSFERENCIA', canal = 'ONLINE') => ({ estado, canal, pago: { medio } });

describe('acciones del administrador', () => {
  it('una transferencia pendiente se puede confirmar (HU-15.1)', () => {
    expect(accionesDe(venta('PENDIENTE_DE_PAGO'))).toEqual(['CONFIRMAR']);
  });

  it('también la de un grupo externo (HU-26.4)', () => {
    expect(accionesDe(venta('PENDIENTE_DE_PAGO', 'TRANSFERENCIA', 'GRUPO_EXTERNO'))).toEqual(['CONFIRMAR']);
  });

  it('Mercado Pago no se confirma a mano', () => {
    expect(accionesDe(venta('PENDIENTE_DE_PAGO', 'MERCADO_PAGO'))).toEqual([]);
  });

  it.each(['VENCIDA', 'CANCELADA'])('una transferencia %s se puede confirmar, y queda a verificar (HU-15.2)', (estado) => {
    expect(accionesDe(venta(estado))).toEqual(['CONFIRMAR_TARDE']);
  });

  it('avanza paso a paso, sin saltar estados (HU-23)', () => {
    expect(accionesDe(venta('PAGADA'))).toEqual(['AL_CORREO']);
    expect(accionesDe(venta('ENTREGADA_AL_CORREO'))).toEqual(['ENTREGADA']);
    expect(accionesDe(venta('ENTREGADA'))).toEqual([]);
  });

  it.each(['A_VERIFICAR', 'REGISTRADA'])('%s no tiene acciones', (estado) => {
    expect(accionesDe(venta(estado, 'TRANSFERENCIA', estado === 'REGISTRADA' ? 'LOCAL' : 'ONLINE'))).toEqual([]);
  });
});
