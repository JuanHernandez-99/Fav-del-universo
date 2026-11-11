import { describe, expect, it } from 'vitest';
import { CATEGORIAS } from '../dominio.js';
import { esTonoOscuro, ILUSTRACIONES, ilustracionDe } from './ilustraciones.js';

describe('ilustraciones de prendas sin foto', () => {
  it('hay al menos dos por categoría y todas tienen una capa que toma el color', () => {
    for (const [categoria] of CATEGORIAS) {
      expect(ILUSTRACIONES[categoria].length).toBeGreaterThanOrEqual(2);
      for (const capas of ILUSTRACIONES[categoria]) expect(capas.some(([tipo]) => tipo === 'base')).toBe(true);
    }
  });

  it('la misma remera siempre usa la misma ilustración', () => {
    expect(ilustracionDe('remeras', 'prod-123')).toBe(ilustracionDe('remeras', 'prod-123'));
  });

  it('una categoría desconocida no rompe', () => {
    expect(ilustracionDe('sombreros', 'x')).toBeDefined();
  });

  it('negro es oscuro; crema y los colores generados, no', () => {
    expect(esTonoOscuro('#2b2730')).toBe(true);
    expect(esTonoOscuro('#efe3cc')).toBe(false);
    expect(esTonoOscuro('hsl(200 42% 62%)')).toBe(false);
  });
});
