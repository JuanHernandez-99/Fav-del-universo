import { describe, expect, it } from 'vitest';
import { claveDelPedido, explicarError, guardarVenta, leerVenta, olvidarClave, olvidarVentas } from './compra.js';

const almacenFalso = () => {
  const datos = new Map();
  return {
    getItem: (k) => datos.get(k) ?? null, setItem: (k, v) => datos.set(k, v), removeItem: (k) => datos.delete(k),
    get length() { return datos.size; }, key: (i) => [...datos.keys()][i],
  };
};
const contador = () => { let n = 0; return () => `clave-${++n}`; };
const pedido = {
  email: 'ana@example.com', medioPago: 'TRANSFERENCIA',
  items: [{ varianteId: 'var-remera-blanca-m', cantidad: 2 }, { varianteId: 'var-jean-celeste-38', cantidad: 1 }],
};

describe('clave de idempotencia (HU-11.8)', () => {
  it('reintentar el mismo pedido usa la misma clave', () => {
    const almacen = almacenFalso(), generar = contador();
    expect(claveDelPedido(pedido, almacen, generar)).toBe('clave-1');
    expect(claveDelPedido(pedido, almacen, generar)).toBe('clave-1');
  });

  it('el orden de las prendas no cambia el pedido', () => {
    const almacen = almacenFalso(), generar = contador();
    const clave = claveDelPedido(pedido, almacen, generar);
    expect(claveDelPedido({ ...pedido, items: [...pedido.items].reverse() }, almacen, generar)).toBe(clave);
  });

  it.each([
    ['otro medio de pago', { ...pedido, medioPago: 'MERCADO_PAGO' }],
    ['otro email', { ...pedido, email: 'otra@example.com' }],
    ['otra cantidad', { ...pedido, items: [{ varianteId: 'var-remera-blanca-m', cantidad: 3 }] }],
  ])('con %s usa otra clave: Ventas rechaza la misma con otro contenido', (_, cambiado) => {
    const almacen = almacenFalso(), generar = contador();
    const clave = claveDelPedido(pedido, almacen, generar);
    expect(claveDelPedido(cambiado, almacen, generar)).not.toBe(clave);
  });

  it('después de una compra exitosa, la siguiente igual es otra venta', () => {
    const almacen = almacenFalso(), generar = contador();
    const clave = claveDelPedido(pedido, almacen, generar);
    olvidarClave(almacen);
    expect(claveDelPedido(pedido, almacen, generar)).not.toBe(clave);
  });

  it('funciona sin HTTPS, donde crypto.randomUUID no existe', () => {
    const original = crypto.randomUUID;
    try {
      crypto.randomUUID = undefined;
      expect(claveDelPedido(pedido, almacenFalso())).toMatch(/^[0-9a-f]{32}$/);
    } finally {
      crypto.randomUUID = original;
    }
  });
});

describe('guardar la venta en la pestaña', () => {
  it('se recupera por su id', () => {
    const almacen = almacenFalso();
    guardarVenta({ ventaId: 'v1', total: 100 }, almacen);
    expect(leerVenta('v1', almacen).total).toBe(100);
    expect(leerVenta('v2', almacen)).toBeNull();
  });

  it('al cerrar la sesión se borran todas las ventas, y solo las ventas', () => {
    const almacen = almacenFalso();
    guardarVenta({ ventaId: 'v1' }, almacen);
    guardarVenta({ ventaId: 'v2' }, almacen);
    almacen.setItem('otra-cosa', 'x');
    olvidarVentas(almacen);
    expect(leerVenta('v1', almacen)).toBeNull();
    expect(leerVenta('v2', almacen)).toBeNull();
    expect(almacen.getItem('otra-cosa')).toBe('x');
  });
});

describe('explicar un error', () => {
  it('sin stock manda a corregir el carrito', () => {
    expect(explicarError({ codigo: 'STOCK_INSUFICIENTE' }).volverAlCarrito).toBe(true);
  });

  it('un error de red se puede reintentar', () => {
    expect(explicarError({ estado: 0 }).reintentable).toBe(true);
  });

  it('un token vencido no se reintenta igual', () => {
    const r = explicarError({ codigo: 'NO_AUTENTICADO' });
    expect(r.sesionVencida).toBe(true);
    expect(r.reintentable).toBeUndefined();
  });
});
