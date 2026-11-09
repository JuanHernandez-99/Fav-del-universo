import { renderToStaticMarkup } from 'react-dom/server';
import { MemoryRouter } from 'react-router';
import { describe, expect, it } from 'vitest';
import Politica from './Politica.jsx';
import { enlaceTelefono, POLITICA } from './politica.js';

const pagina = (contenido) => renderToStaticMarkup(<MemoryRouter><Politica contenido={contenido} /></MemoryRouter>);

describe('política de cambios (HU-24)', () => {
  it('sin los datos del cliente lo avisa, y no inventa plazos ni contactos', () => {
    const html = pagina({ parrafos: [], mail: '', telefono: '' });
    expect(html).toContain('Estamos terminando de publicar la política');
    expect(html).toContain('Estamos terminando de publicar los datos de contacto');
    expect(html).not.toContain('mailto:');
    expect(html).not.toContain('tel:');
    expect(html).not.toMatch(/\d+ días/);
  });

  it('siempre explica lo que ya está definido (RN-20 y RN-21)', () => {
    const html = pagina({ parrafos: [], mail: '', telefono: '' });
    expect(html).toContain('se gestionan por mail o por teléfono');
    expect(html).toContain('la podés cancelar');
  });

  it('con los datos del cliente los muestra, sin avisos', () => {
    const html = pagina({ parrafos: ['Texto del cliente.', 'Segundo párrafo.'], mail: 'ventas@fav.example', telefono: '+54 11 1234-5678' });
    expect(html).toContain('Texto del cliente.');
    expect(html).toContain('Segundo párrafo.');
    expect(html).toContain('href="mailto:ventas@fav.example"');
    expect(html).toContain('href="tel:+541112345678"');
    expect(html).not.toContain('Estamos terminando');
  });

  it('con un solo dato de contacto muestra ese', () => {
    const html = pagina({ parrafos: [], mail: 'ventas@fav.example', telefono: '' });
    expect(html).toContain('mailto:');
    expect(html).not.toContain('tel:');
    expect(html).not.toContain('los datos de contacto');
  });

  it('el contenido publicado por defecto sigue pendiente hasta tener el del cliente', () => {
    expect(POLITICA).toEqual({ parrafos: [], mail: '', telefono: '' });
  });

  it('el enlace de teléfono queda sin espacios ni guiones', () => {
    expect(enlaceTelefono('+54 11 1234-5678')).toBe('tel:+541112345678');
  });
});
