import { describe, expect, it } from 'vitest';
import { fisicoDespues, leerMonto, lineasParaSugerencia } from './ventaLocal.js';

const lineas = [{ varianteId: 'var-remera-blanca-m', cantidad: 2 }, { varianteId: 'var-jean-celeste-38', cantidad: 1 }];

describe('stock físico después de la venta (RN-25)', () => {
  it('descuenta lo vendido', () => {
    expect(fisicoDespues(lineas, { 'var-remera-blanca-m': 5, 'var-jean-celeste-38': 1 })).toEqual({ 'var-remera-blanca-m': 3, 'var-jean-celeste-38': 0 });
  });

  it('puede quedar negativo: la venta no se bloquea (HU-17.6)', () => {
    expect(fisicoDespues(lineas, { 'var-remera-blanca-m': 1 })).toEqual({ 'var-remera-blanca-m': -1, 'var-jean-celeste-38': -1 });
  });

  it('sin datos de Inventario no afirma nada', () => {
    expect(fisicoDespues(lineas, null)).toEqual({});
  });
});

describe('monto cobrado', () => {
  it.each([['14000', 14000], ['15999,90', 15999.9], ['0', 0], [' 12.5 ', 12.5]])('acepta %s', (texto, valor) => {
    expect(leerMonto(texto)).toBe(valor);
  });

  it.each(['', '-5', '12.345', 'abc', '1e3'])('rechaza "%s"', (texto) => {
    expect(leerMonto(texto)).toBeNull();
  });
});

it('arma las líneas como las pide la sugerencia de Ventas', () => {
  expect(lineasParaSugerencia(lineas)).toBe('var-remera-blanca-m:2,var-jean-celeste-38:1');
});
