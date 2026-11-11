// Comprueba los errores que genera el gateway, contra el sistema levantado: node gateway/pruebas/errores.mjs
// Detiene y vuelve a levantar cada servicio. Al final puede quedar activo un minuto el límite de intentos de login.
import { randomUUID } from 'node:crypto';
import { execSync } from 'node:child_process';

const GW = 'http://localhost:8080';
const GRUPO = { 'X-API-Key': process.env.CAPACIDAD_API_KEY ?? 'clave-grupo-proveedores' };
const JSON_ = { 'Content-Type': 'application/json' };
let ok = 0, mal = 0;
const check = (n, c, e = '') => { c ? ok++ : mal++; console.log(`${c ? 'OK   ' : 'FALLA'} ${n}${!c && e !== '' ? ' -> ' + JSON.stringify(e) : ''}`); };
const dc = (cmd) => execSync(`docker compose ${cmd}`, { stdio: ['ignore', 'pipe', 'pipe'] });
const espera = (ms) => new Promise((f) => setTimeout(f, ms));

async function pedir(metodo, ruta, { headers = {}, body } = {}) {
  const r = await fetch(GW + ruta, { method: metodo, headers: { ...(body ? JSON_ : {}), ...headers }, body: body ? JSON.stringify(body) : undefined });
  const texto = await r.text();
  let json = null; try { json = JSON.parse(texto); } catch {}
  return { status: r.status, tipo: r.headers.get('content-type'), reintentar: r.headers.get('retry-after'), correlacion: r.headers.get('x-correlation-id'), json, texto };
}
const volver = async (ruta, opciones) => {
  for (let i = 0; i < 80; i++) { if ((await pedir('GET', ruta, opciones).catch(() => ({}))).status === 200) return true; await espera(500); }
  return false;
};

function esNoDisponible(nombre, r) {
  check(`${nombre}: 503 NO_DISPONIBLE en JSON`, r.status === 503 && r.json?.codigo === 'NO_DISPONIBLE' && r.tipo?.includes('application/problem+json'), { status: r.status, tipo: r.tipo, texto: r.texto.slice(0, 80) });
  check(`${nombre}: con Retry-After y correlation id`, r.reintentar === '1' && !!r.correlacion, { reintentar: r.reintentar, correlacion: r.correlacion });
}

const sesion = await pedir('POST', '/usuarios/v1/sesiones', { body: { email: 'admin@fav.example', contrasena: 'cambiar-admin-local' } });
const ADMIN = { Authorization: `Bearer ${sesion.json.token}` };
const pedido = { items: [{ varianteId: 'var-jean-mom-celeste-38', cantidad: 1 }] };

console.log('--- Inventario caído ---');
dc('stop inventario');
// Las primeras dos llamadas prueban cada réplica; las siguientes ya no tienen a quién llamar. Tienen que dar lo mismo.
for (const intento of ['al caer', 'con las réplicas descartadas']) {
  esNoDisponible(`tienda, ${intento}`, await pedir('GET', '/inventario/v1/disponibilidad?variantes=x'));
}
esNoDisponible('panel, ruta con token', await pedir('GET', '/inventario/v1/stock?variantes=x', { headers: ADMIN }));
check('el token se sigue validando primero', (await pedir('GET', '/inventario/v1/stock?variantes=x')).status === 401);
dc('start inventario');
check('Inventario vuelve', await volver('/inventario/v1/disponibilidad?variantes=x'));

console.log('\n--- Catálogo caído ---');
dc('stop catalogo');
esNoDisponible('GET /v1/productos, del contrato', await pedir('GET', '/v1/productos', { headers: GRUPO }));
esNoDisponible('tienda', await pedir('GET', '/catalogo/v1/tienda/productos'));
esNoDisponible('panel', await pedir('GET', '/catalogo/v1/admin/productos', { headers: ADMIN }));
dc('start catalogo');
check('Catálogo vuelve', await volver('/catalogo/v1/tienda/productos?tamano=1'));

console.log('\n--- Ventas caído ---');
dc('stop ventas');
esNoDisponible('POST /v1/pedidos, del contrato', await pedir('POST', '/v1/pedidos', { headers: { ...GRUPO, 'Idempotency-Key': randomUUID() }, body: pedido }));
esNoDisponible('GET /v1/pedidos/{id}, del contrato', await pedir('GET', `/v1/pedidos/${randomUUID()}`, { headers: GRUPO }));
esNoDisponible('compra', await pedir('POST', '/ventas/v1/compras', { headers: { 'Idempotency-Key': randomUUID() }, body: { email: 'ana@example.com', medioPago: 'TRANSFERENCIA', ...pedido } }));
esNoDisponible('panel', await pedir('GET', '/ventas/v1/admin/ventas', { headers: ADMIN }));
dc('start ventas');
check('Ventas vuelve', await volver('/ventas/v1/admin/ventas', { headers: ADMIN }));

console.log('\n--- Usuarios caído ---');
dc('stop usuarios');
esNoDisponible('inicio de sesión', await pedir('POST', '/usuarios/v1/sesiones', { body: { email: 'ana@example.com', contrasena: 'remera-blanca' } }));
check('con Usuarios caído, el token sigue valiendo (ADR-016)', (await pedir('GET', '/inventario/v1/stock?variantes=x', { headers: ADMIN })).status === 200);
dc('start usuarios');
await espera(4000);

console.log('\n--- Frontend caído ---');
dc('stop frontend');
const pagina = await pedir('GET', '/');
check('la página responde 503 en HTML, para una persona', pagina.status === 503 && pagina.tipo?.includes('text/html') && pagina.texto.includes('no está disponible'), { status: pagina.status, tipo: pagina.tipo });
check('la API sigue', (await pedir('GET', '/catalogo/v1/tienda/productos?tamano=1')).status === 200);
dc('start frontend');
check('Frontend vuelve', await volver('/'));

console.log('\n--- Los errores de los servicios pasan tal cual ---');
dc('stop gimnasio-mock');
const gimnasio = await pedir('GET', '/inventario/v1/consignacion', { headers: ADMIN });
check('503 GIMNASIO_NO_DISPONIBLE no se pisa', gimnasio.status === 503 && gimnasio.json?.codigo === 'GIMNASIO_NO_DISPONIBLE' && gimnasio.reintentar === '30', { status: gimnasio.status, codigo: gimnasio.json?.codigo, reintentar: gimnasio.reintentar });
dc('start gimnasio-mock');
const inexistente = await pedir('GET', '/catalogo/v1/tienda/productos/no-existe');
check('404 PRODUCTO_NO_ENCONTRADO no se pisa', inexistente.status === 404 && inexistente.json?.codigo === 'PRODUCTO_NO_ENCONTRADO', inexistente.status);
const sinClave = await pedir('GET', '/v1/productos');
check('401 del contrato igual que antes', sinClave.status === 401 && sinClave.json?.codigo === 'NO_AUTENTICADO', sinClave.status);

console.log('\n--- Límite de pedidos ---');
const tanda = await Promise.all(Array.from({ length: 80 }, () => pedir('GET', '/v1/productos', { headers: GRUPO })));
const frenado = tanda.find((r) => r.status === 429);
check('al superar el límite por grupo responde 429', !!frenado, tanda.map((r) => r.status).slice(0, 5));
check('429 LIMITE_EXCEDIDO en JSON', frenado?.json?.codigo === 'LIMITE_EXCEDIDO' && frenado?.tipo?.includes('application/problem+json'), frenado && { tipo: frenado.tipo, texto: frenado.texto.slice(0, 80) });
check('con Retry-After y correlation id', frenado?.reintentar === '2' && !!frenado?.correlacion, frenado && { r: frenado.reintentar, c: frenado.correlacion });
const intentos = await Promise.all(Array.from({ length: 60 }, () => pedir('POST', '/usuarios/v1/sesiones', { body: { email: 'x@example.com', contrasena: 'zzzzzzzz' } })));
const frenadoLogin = intentos.find((r) => r.status === 429);
check('el límite de login también responde en JSON', frenadoLogin?.json?.codigo === 'LIMITE_EXCEDIDO', intentos.map((r) => r.status).slice(0, 5));

console.log(`\nResultado: ${ok} OK, ${mal} fallas`);
process.exit(mal ? 1 : 0);
