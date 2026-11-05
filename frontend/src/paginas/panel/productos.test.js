import { describe, expect, it } from 'vitest';
import { combinarVariantes, erroresPorCampo, nombreCampo, normalizarColor, tallesDe } from './productos.js';

describe('variantes del producto nuevo', () => {
  it('combina cada color con cada talle', () => {
    expect(combinarVariantes(['Blanco', 'Negro'], ['S', 'M'])).toEqual([
      { talle: 'S', color: 'Blanco' }, { talle: 'M', color: 'Blanco' }, { talle: 'S', color: 'Negro' }, { talle: 'M', color: 'Negro' },
    ]);
  });

  it('no repite un color escrito de otra forma (HU-03.4)', () => {
    expect(combinarVariantes(['Azul marino', 'azul-marino', ' AZUL MARINO '], ['M'])).toEqual([{ talle: 'M', color: 'Azul marino' }]);
  });

  it('ignora colores vacíos', () => {
    expect(combinarVariantes(['', '  ', 'Rojo'], ['U'])).toEqual([{ talle: 'U', color: 'Rojo' }]);
  });

  it('normaliza como Catálogo: sin acentos ni espacios', () => {
    expect(normalizarColor(' Marrón Café ')).toBe('marron-cafe');
  });
});

describe('sistemas de talles (RN-02)', () => {
  it('cada sistema tiene sus talles', () => {
    expect(tallesDe('NUMERICO_1_4')).toEqual(['1', '2', '3', '4']);
    expect(tallesDe('UNICO')).toEqual(['U']);
    expect(tallesDe('OTRO')).toEqual([]);
  });
});

describe('errores de Catálogo', () => {
  it('se agrupan por campo y tienen un nombre legible', () => {
    expect(erroresPorCampo([{ campo: 'nombre', mensaje: 'es obligatorio' }])).toEqual({ nombre: 'es obligatorio' });
    expect(nombreCampo('precioBase.monto')).toBe('Precio');
    expect(nombreCampo('fotos[0].url')).toBe('Foto 1');
  });
});
