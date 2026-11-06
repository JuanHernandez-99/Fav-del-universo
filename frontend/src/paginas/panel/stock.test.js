import { describe, expect, it } from 'vitest';
import { cantidadValida, explicarRechazo, filasDeStock, pedidoDe } from './stock.js';

describe('stock por variante (HU-09.1)', () => {
  const variantes = [
    { varianteId: 'v-negro-l', talle: 'L', color: 'Negro' }, { varianteId: 'v-blanco-m', talle: 'M', color: 'Blanco' },
    { varianteId: 'v-blanco-s', talle: 'S', color: 'Blanco' },
  ];

  it('une Catálogo e Inventario y ordena por color y talle', () => {
    const filas = filasDeStock(variantes, [{ varianteId: 'v-blanco-m', online: 5, reservado: 2, disponibleOnline: 3, fisico: -1, stockACorregir: true }]);
    expect(filas.map((f) => f.varianteId)).toEqual(['v-blanco-s', 'v-blanco-m', 'v-negro-l']);
    expect(filas[1]).toMatchObject({ registrada: true, online: 5, reservado: 2, disponibleOnline: 3, fisico: -1, stockACorregir: true });
  });

  it('una variante sin ingresos figura en 0 y sin registrar', () => {
    expect(filasDeStock(variantes, [])[0]).toMatchObject({ registrada: false, online: 0, fisico: 0, stockACorregir: false });
  });
});

describe('pedido a Inventario', () => {
  const base = { varianteId: 'v', canal: 'FISICO', origen: 'ONLINE', cantidad: '3', sentido: 'RESTAR', motivo: 'PERDIDA', nota: ' manchada ' };

  it('ingreso (HU-06)', () => {
    expect(pedidoDe({ ...base, tipo: 'INGRESO' })).toEqual({ ruta: 'ingresos', cuerpo: { varianteId: 'v', canal: 'FISICO', cantidad: 3 } });
  });

  it('transferencia (HU-07)', () => {
    expect(pedidoDe({ ...base, tipo: 'TRANSFERENCIA' })).toEqual({ ruta: 'transferencias', cuerpo: { varianteId: 'v', origen: 'ONLINE', cantidad: 3 } });
  });

  it('ajuste que resta: cantidad negativa (HU-08)', () => {
    expect(pedidoDe({ ...base, tipo: 'AJUSTE' }).cuerpo).toEqual({ varianteId: 'v', canal: 'FISICO', cantidad: -3, motivo: 'PERDIDA', nota: 'manchada' });
    expect(pedidoDe({ ...base, tipo: 'AJUSTE', sentido: 'SUMAR' }).cuerpo.cantidad).toBe(3);
  });

  it.each([['3', true], ['0', false], ['-2', false], ['1.5', false], ['', false]])('cantidad "%s" válida: %s', (t, ok) => {
    expect(cantidadValida(t)).toBe(ok);
  });
});

describe('rechazos de Inventario', () => {
  it('sin stock suficiente dice cuánto había', () => {
    expect(explicarRechazo({ codigo: 'STOCK_INSUFICIENTE', faltantes: [{ disponible: 2 }] }, 'TRANSFERENCIA')).toContain('hay 2 disponibles');
  });

  it('variante sin stock: pide un ingreso primero', () => {
    expect(explicarRechazo({ codigo: 'VARIANTE_INEXISTENTE' }, 'AJUSTE')).toContain('ingreso');
  });
});
