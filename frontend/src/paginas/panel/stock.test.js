import { describe, expect, it } from 'vitest';
import {
  cantidadValida, columnasDe, explicarRechazo, filasDeStock, ordenarPorStock, pedidoDe, siguienteOrden, totalesDe,
} from './stock.js';

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

describe('lista de stock por producto', () => {
  it('suma las variantes y marca el producto si alguna tiene el físico negativo', () => {
    const filas = [
      { online: 5, reservado: 2, disponibleOnline: 3, fisico: 4, stockACorregir: false },
      { online: 1, reservado: 0, disponibleOnline: 1, fisico: -1, stockACorregir: true },
    ];
    expect(totalesDe(filas)).toEqual({ online: 6, reservado: 2, disponibleOnline: 4, fisico: 3, stockACorregir: true });
    expect(totalesDe([])).toMatchObject({ online: 0, fisico: 0, stockACorregir: false });
  });

  it.each([
    ['todo', ['online', 'reservado', 'disponibleOnline', 'fisico']],
    ['fisico', ['fisico']],
    ['online', ['online', 'disponibleOnline']],
    ['reservado', ['reservado']],
    ['cualquiera', ['online', 'reservado', 'disponibleOnline', 'fisico']],
  ])('la vista "%s" muestra %j', (vista, campos) => {
    expect(columnasDe(vista).map(([campo]) => campo)).toEqual(campos);
  });
});

describe('orden por columna', () => {
  it('desc, después asc, después sin orden; otra columna empieza en desc', () => {
    const primero = siguienteOrden(null, 'fisico');
    expect(primero).toEqual({ campo: 'fisico', sentido: 'desc' });
    const segundo = siguienteOrden(primero, 'fisico');
    expect(segundo).toEqual({ campo: 'fisico', sentido: 'asc' });
    expect(siguienteOrden(segundo, 'fisico')).toBeNull();
    expect(siguienteOrden(segundo, 'reservado')).toEqual({ campo: 'reservado', sentido: 'desc' });
  });

  it('ordena productos por el total y desempata por nombre', () => {
    const productos = [
      { nombre: 'Remera Luna', total: { disponibleOnline: 3 } },
      { nombre: 'Buzo Cometa', total: { disponibleOnline: 10 } },
      { nombre: 'Jean Órbita', total: { disponibleOnline: 3 } },
    ];
    const nombres = (o) => ordenarPorStock(productos, o).map((p) => p.nombre);
    expect(nombres({ campo: 'disponibleOnline', sentido: 'desc' })).toEqual(['Buzo Cometa', 'Jean Órbita', 'Remera Luna']);
    expect(nombres({ campo: 'disponibleOnline', sentido: 'asc' })).toEqual(['Jean Órbita', 'Remera Luna', 'Buzo Cometa']);
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
