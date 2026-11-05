import { describe, expect, it } from 'vitest';
import {
  agregar, calcularSubtotal, cambiarCantidad, contarUnidades, guardarCarrito, leerCarrito, quitar, revisar, sePuedeComprar,
} from './carrito.js';

const remeraM = { varianteId: 'var-remera-blanca-m', productoId: 'prod-remera', nombre: 'Remera', talle: 'M', color: 'Blanco', precio: 15999.9 };
const jean = { varianteId: 'var-jean-celeste-38', productoId: 'prod-jean', nombre: 'Jean', talle: '38', color: 'Celeste', precio: 42000 };

const almacenFalso = (inicial) => {
  const datos = new Map(inicial ? [['fav-carrito-v1', inicial]] : []);
  return { getItem: (k) => datos.get(k) ?? null, setItem: (k, v) => datos.set(k, v) };
};

describe('armar el carrito', () => {
  it('agregar la misma variante suma unidades, no otra línea', () => {
    const items = agregar(agregar([], remeraM, 2), remeraM, 1);
    expect(items).toHaveLength(1);
    expect(items[0].cantidad).toBe(3);
  });

  it('dos talles del mismo producto son dos líneas', () => {
    const items = agregar(agregar([], remeraM), { ...remeraM, varianteId: 'var-remera-blanca-l', talle: 'L' });
    expect(items).toHaveLength(2);
  });

  it('bajar a cero quita la línea', () => {
    const items = cambiarCantidad(agregar([], remeraM), remeraM.varianteId, 0);
    expect(items).toEqual([]);
  });

  it('quita solo la variante indicada', () => {
    const items = quitar(agregar(agregar([], remeraM), jean), remeraM.varianteId);
    expect(items.map((i) => i.varianteId)).toEqual([jean.varianteId]);
  });

  it('suma unidades y calcula el subtotal en centavos, sin errores de coma flotante', () => {
    const items = agregar(agregar([], { ...remeraM, precio: 0.1 }, 3), jean, 2);
    expect(contarUnidades(items)).toBe(5);
    expect(calcularSubtotal(items)).toBe(84000.3);
  });
});

describe('guardar en el navegador (HU-10.4)', () => {
  it('se conserva al volver a leer', () => {
    const almacen = almacenFalso();
    guardarCarrito(agregar([], remeraM, 2), almacen);
    expect(leerCarrito(almacen)[0].cantidad).toBe(2);
  });

  it.each([['no es JSON'], ['{"a":1}'], ['[{"varianteId":"x","cantidad":-1}]']])('un dato inválido (%s) no rompe la tienda', (guardado) => {
    expect(leerCarrito(almacenFalso(guardado))).toEqual([]);
  });
});

describe('revisar antes de comprar (HU-10.3)', () => {
  const remeraPublicada = { precio: 15999.9, variantes: [{ varianteId: remeraM.varianteId }] };
  const items = [{ ...remeraM, cantidad: 1 }];

  it('con stock y publicado se puede comprar', () => {
    const revisados = revisar(items, { 'prod-remera': remeraPublicada }, { [remeraM.varianteId]: true });
    expect(revisados[0].problema).toBeUndefined();
    expect(sePuedeComprar(revisados)).toBe(true);
  });

  it('sin stock se avisa y no se puede comprar', () => {
    const revisados = revisar(items, { 'prod-remera': remeraPublicada }, { [remeraM.varianteId]: false });
    expect(revisados[0].problema).toBe('SIN_STOCK');
    expect(sePuedeComprar(revisados)).toBe(false);
  });

  it('una variante que Inventario no informó cuenta como sin stock', () => {
    expect(revisar(items, { 'prod-remera': remeraPublicada }, {})[0].problema).toBe('SIN_STOCK');
  });

  it('despublicado: se avisa aunque haya stock', () => {
    const revisados = revisar(items, { 'prod-remera': null }, { [remeraM.varianteId]: true });
    expect(revisados[0].problema).toBe('NO_PUBLICADO');
  });

  it('si Inventario no respondió no se afirma que no hay: es otro aviso', () => {
    const revisados = revisar(items, { 'prod-remera': remeraPublicada }, null);
    expect(revisados[0].problema).toBe('SIN_DATOS');
    expect(sePuedeComprar(revisados)).toBe(false);
  });

  it('muestra el precio actual y marca que cambió (RN-22)', () => {
    const revisados = revisar(items, { 'prod-remera': { ...remeraPublicada, precio: 12000 } }, { [remeraM.varianteId]: true });
    expect(revisados[0].precio).toBe(12000);
    expect(revisados[0].precioCambio).toBe(true);
  });

  it('un carrito vacío no se puede comprar', () => {
    expect(sePuedeComprar([])).toBe(false);
  });
});
