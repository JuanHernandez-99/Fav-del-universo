import { expect, it } from 'vitest';
import { formatearPrecio } from './dominio.js';

// El espacio entre $ y el número depende de la versión de Intl: se compara sin él.
const sinEspacios = (texto) => texto.replace(/\s/g, '');

it.each([[15000, '$15.000'], [9500.5, '$9.500,50'], [15999.9, '$15.999,90'], [0.1, '$0,10'], [0, '$0']])('%s se muestra %s', (monto, esperado) => {
  expect(sinEspacios(formatearPrecio(monto))).toBe(esperado);
});
