import { describe, expect, it } from 'vitest';
import { aCampoFecha, cuerpoDe, deCampoFecha, estadoDe, vigenciaParaTerminar } from './promociones.js';

const AHORA = Date.parse('2026-11-15T15:00:00Z');
const promo = (desde, hasta) => ({ vigencia: { desde, hasta } });

describe('estado según la vigencia', () => {
  it.each([
    ['2026-11-01T00:00:00Z', '2026-11-30T00:00:00Z', 'VIGENTE'],
    ['2026-12-01T00:00:00Z', '2026-12-31T00:00:00Z', 'PROXIMA'],
    ['2026-10-01T00:00:00Z', '2026-10-31T00:00:00Z', 'TERMINADA'],
    ['2026-11-01T00:00:00Z', '2026-11-15T15:00:00Z', 'TERMINADA'],
  ])('del %s al %s: %s', (desde, hasta, estado) => {
    expect(estadoDe(promo(desde, hasta), AHORA)).toBe(estado);
  });
});

describe('terminar una promoción', () => {
  it('una vigente termina ahora y conserva su inicio', () => {
    const v = vigenciaParaTerminar(promo('2026-11-01T00:00:00Z', '2026-11-30T00:00:00Z'), AHORA);
    expect(v).toEqual({ desde: '2026-11-01T00:00:00Z', hasta: '2026-11-15T15:00:00.000Z' });
    expect(estadoDe({ vigencia: v }, AHORA)).toBe('TERMINADA');
  });

  it('una próxima queda toda en el pasado: no llega a aplicarse', () => {
    const v = vigenciaParaTerminar(promo('2026-12-01T00:00:00Z', '2026-12-31T00:00:00Z'), AHORA);
    expect(Date.parse(v.desde)).toBeLessThan(Date.parse(v.hasta)); // Catálogo exige desde < hasta (HU-05.2)
    expect(estadoDe({ vigencia: v }, AHORA)).toBe('TERMINADA');
  });
});

describe('fechas del formulario', () => {
  it('ida y vuelta en la hora del navegador', () => {
    const iso = '2026-11-30T23:59:00.000Z';
    expect(deCampoFecha(aCampoFecha(iso))).toBe(iso);
  });

  it('sin fecha', () => {
    expect(aCampoFecha('')).toBe('');
    expect(deCampoFecha('')).toBeNull();
  });
});

describe('cuerpo para Catálogo', () => {
  const base = { nombre: ' Verano ', tipo: 'PORCENTAJE', porcentaje: '20', medioPago: 'EFECTIVO', desde: '', hasta: '', categorias: ['remeras'], productos: [{ productoId: 'prod-jean' }] };

  it('medioPago solo en las de medio de pago (RN-05)', () => {
    expect(cuerpoDe({ ...base, alcanceTipo: 'TIENDA' }).medioPago).toBeNull();
    expect(cuerpoDe({ ...base, tipo: 'MEDIO_PAGO', alcanceTipo: 'TIENDA' }).medioPago).toBe('EFECTIVO');
  });

  it('los ids dependen del alcance', () => {
    expect(cuerpoDe({ ...base, alcanceTipo: 'TIENDA' }).alcance).toEqual({ tipo: 'TIENDA', ids: [] });
    expect(cuerpoDe({ ...base, alcanceTipo: 'CATEGORIA' }).alcance.ids).toEqual(['remeras']);
    expect(cuerpoDe({ ...base, alcanceTipo: 'PRODUCTO' }).alcance.ids).toEqual(['prod-jean']);
  });

  it('limpia el nombre y pasa el porcentaje a número', () => {
    const c = cuerpoDe({ ...base, alcanceTipo: 'TIENDA' });
    expect(c.nombre).toBe('Verano');
    expect(c.porcentaje).toBe(20);
  });
});
