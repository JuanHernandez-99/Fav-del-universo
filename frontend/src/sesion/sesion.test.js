import { describe, expect, it } from 'vitest';
import { borrarToken, guardarToken, leerSesion, rutaDeVuelta, sesionDesdeToken } from './sesion.js';

const AHORA = 1_800_000_000_000;
const enBase64Url = (texto) => Buffer.from(texto).toString('base64url');
const token = (datos) => `${enBase64Url('{"alg":"RS256"}')}.${enBase64Url(JSON.stringify(datos))}.firma`;
const vigente = token({ email: 'ana@example.com', role: 'comprador', exp: AHORA / 1000 + 3600 });
const vencido = token({ email: 'ana@example.com', role: 'comprador', exp: AHORA / 1000 - 1 });

const almacenFalso = () => {
  const datos = new Map();
  return { getItem: (k) => datos.get(k) ?? null, setItem: (k, v) => datos.set(k, v), removeItem: (k) => datos.delete(k), datos };
};

describe('leer el token', () => {
  it('obtiene email, rol y vencimiento', () => {
    expect(sesionDesdeToken(vigente, AHORA)).toMatchObject({ email: 'ana@example.com', rol: 'comprador', venceEn: AHORA + 3600_000 });
  });

  it('lee un email con acentos', () => {
    const conAcento = token({ email: 'josé@example.com', role: 'comprador', exp: AHORA / 1000 + 60 });
    expect(sesionDesdeToken(conAcento, AHORA).email).toBe('josé@example.com');
  });

  it('un token vencido no es una sesión', () => {
    expect(sesionDesdeToken(vencido, AHORA)).toBeNull();
  });

  it.each(['basura', 'a.b.c', '', token({ role: 'comprador' })])('un token ilegible (%s) no rompe la tienda', (malo) => {
    expect(sesionDesdeToken(malo, AHORA)).toBeNull();
  });
});

describe('guardar la sesión', () => {
  it('se conserva y se puede borrar', () => {
    const almacen = almacenFalso();
    guardarToken(vigente, almacen);
    expect(leerSesion(almacen, AHORA).email).toBe('ana@example.com');
    borrarToken(almacen);
    expect(leerSesion(almacen, AHORA)).toBeNull();
  });

  it('al leer un token vencido lo borra', () => {
    const almacen = almacenFalso();
    guardarToken(vencido, almacen);
    expect(leerSesion(almacen, AHORA)).toBeNull();
    expect(almacen.datos.size).toBe(0);
  });
});

describe('volver después de ingresar', () => {
  it('acepta una ruta de la tienda', () => {
    expect(rutaDeVuelta('/comprar')).toBe('/comprar');
    expect(rutaDeVuelta('/producto/prod-jean?talle=38')).toBe('/producto/prod-jean?talle=38');
  });

  it.each(['//otro.sitio', 'https://otro.sitio', '/\\otro.sitio', 'javascript:alert(1)', '', null])('no sale de la tienda con %s', (volver) => {
    expect(rutaDeVuelta(volver)).toBe('/');
  });
});
