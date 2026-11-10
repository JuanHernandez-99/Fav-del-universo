// Prueba de la validación del token en el gateway (ADR-016). El script de njs no tiene otros tests.
// Necesita el sistema levantado y Node 20 o más nuevo. Desde la raíz del repositorio:
//
//   docker compose up -d --build
//   node gateway/pruebas/token.mjs
//
// Lee la clave privada del volumen de Docker solo para fabricar tokens de prueba, y no la guarda.
import { createHmac, createPrivateKey, createSign, generateKeyPairSync, randomUUID } from 'node:crypto';
import { execSync } from 'node:child_process';

const GW = process.env.GATEWAY_URL ?? 'http://localhost:8080';
const STOCK = '/inventario/v1/stock?variantes=var-jean-mom-celeste-38';
let ok = 0, mal = 0;
const check = (nombre, condicion, detalle = '') => {
  condicion ? ok++ : mal++;
  console.log(`${condicion ? 'OK   ' : 'FALLA'} ${nombre}${!condicion && detalle !== '' ? ' -> ' + JSON.stringify(detalle) : ''}`);
};
const dc = (comando) => execSync(`docker compose ${comando}`, { stdio: ['ignore', 'pipe', 'pipe'] }).toString();
const espera = (ms) => new Promise((resolver) => setTimeout(resolver, ms));

async function pedir(metodo, ruta, { token, headers = {}, body } = {}) {
  const r = await fetch(GW + ruta, {
    method: metodo,
    headers: { ...(token ? { Authorization: `Bearer ${token}` } : {}), ...(body ? { 'Content-Type': 'application/json' } : {}), ...headers },
    body: body ? JSON.stringify(body) : undefined,
  });
  const texto = await r.text();
  let json;
  try { json = JSON.parse(texto); } catch { json = texto; }
  return { status: r.status, json, headers: r.headers };
}

const b64 = (valor) => Buffer.from(typeof valor === 'string' ? valor : JSON.stringify(valor)).toString('base64url');
const firmarRSA = (cabecera, claims, clave) => {
  const contenido = `${b64(cabecera)}.${b64(claims)}`;
  return `${contenido}.${createSign('RSA-SHA256').update(contenido).sign(clave, 'base64url')}`;
};
const leerVolumen = (volumen, ruta) => dc(`run --rm --no-deps --entrypoint cat -v ${volumen} usuarios-init ${ruta}`);

const clavePrivada = createPrivateKey(leerVolumen('jwt-privada:/claves/privada', '/claves/privada/jwt_private.pem'));
const clavePublica = leerVolumen('jwt-publica:/claves/publica', '/claves/publica/jwt_public.pem');
const ahora = Math.floor(Date.now() / 1000);
const claimsAdmin = { sub: randomUUID(), email: 'admin@fav.example', role: 'administrador', iat: ahora, exp: ahora + 600 };
const RS256 = { alg: 'RS256', typ: 'JWT' };

console.log('--- Control: un token bien firmado entra (si no, los rechazos de abajo no prueban nada) ---');
const propio = await pedir('GET', STOCK, { token: firmarRSA(RS256, claimsAdmin, clavePrivada) });
check('firmado con la clave real: 200', propio.status === 200, propio);

console.log('\n--- Tokens falsos: todos 401 ---');
const otraClave = generateKeyPairSync('rsa', { modulusLength: 2048 }).privateKey;
const real = firmarRSA(RS256, { ...claimsAdmin, role: 'comprador' }, clavePrivada);
const [cabeceraReal, , firmaReal] = real.split('.');
const contenidoHS = `${b64({ alg: 'HS256', typ: 'JWT' })}.${b64(claimsAdmin)}`;
const falsos = {
  'sin firma (alg none)': `${b64({ alg: 'none', typ: 'JWT' })}.${b64(claimsAdmin)}.`,
  'HS256 usando la clave pública como secreto': `${contenidoHS}.${createHmac('sha256', clavePublica).update(contenidoHS).digest('base64url')}`,
  'firmado con otra clave': firmarRSA(RS256, claimsAdmin, otraClave),
  'comprador que se cambia el rol a administrador': `${cabeceraReal}.${b64(claimsAdmin)}.${firmaReal}`,
  'vencido': firmarRSA(RS256, { ...claimsAdmin, exp: ahora - 1 }, clavePrivada),
  'sin vencimiento': firmarRSA(RS256, { sub: claimsAdmin.sub, email: claimsAdmin.email, role: 'administrador', iat: ahora }, clavePrivada),
  'sin usuario': firmarRSA(RS256, { email: claimsAdmin.email, role: 'administrador', exp: ahora + 600 }, clavePrivada),
  'rol desconocido': firmarRSA(RS256, { ...claimsAdmin, role: 'superusuario' }, clavePrivada),
  'dos partes': 'abc.def',
  'basura': 'esto-no-es-un-token',
  'JSON roto': `${b64('{no es json')}.${b64('{no es json')}.abc`,
  'demasiado largo': `${b64(RS256)}.${b64({ ...claimsAdmin, relleno: 'x'.repeat(3000) })}.abc`,
};
for (const [nombre, token] of Object.entries(falsos)) {
  const r = await pedir('GET', STOCK, { token });
  check(nombre, r.status === 401 && r.json.codigo === 'NO_AUTENTICADO', { status: r.status, json: r.json });
}
const sinBearer = await pedir('GET', STOCK, { headers: { Authorization: firmarRSA(RS256, claimsAdmin, clavePrivada) } });
check('sin la palabra Bearer', sinBearer.status === 401, sinBearer.status);
const basic = await pedir('GET', STOCK, { headers: { Authorization: 'Basic YWRtaW46YWRtaW4=' } });
check('otro esquema de autenticación', basic.status === 401, basic.status);

console.log('\n--- Formato de los errores ---');
const sinToken = await pedir('GET', STOCK);
check('sin token: 401 con Problem Details', sinToken.status === 401 && sinToken.json.codigo === 'NO_AUTENTICADO' && sinToken.json.type, sinToken);
check('indica el esquema', sinToken.headers.get('www-authenticate') === 'Bearer');
check('devuelve el correlation id', !!sinToken.headers.get('x-correlation-id'));
const vieja = await pedir('GET', STOCK, { headers: { 'X-Admin-Key': 'clave-admin-local' } });
check('la clave vieja del panel ya no sirve', vieja.status === 401, vieja.status);
const falsoRol = await pedir('GET', STOCK, { headers: { 'X-User-Role': 'administrador', 'X-User-Id': 'alguien' } });
check('falsificar X-User-Role no abre el panel', falsoRol.status === 401, falsoRol.status);

console.log('\n--- Usuarios reales ---');
const nueva = () => ({ email: `ana-${randomUUID().slice(0, 8)}@example.com`, contrasena: 'remera-blanca' });
async function ingresar(credenciales) {
  await pedir('POST', '/usuarios/v1/registro', { body: credenciales });
  return (await pedir('POST', '/usuarios/v1/sesiones', { body: credenciales })).json.token;
}
const ana = nueva(), bea = nueva();
const tokenAna = await ingresar(ana), tokenBea = await ingresar(bea);
const tokenAdmin = (await pedir('POST', '/usuarios/v1/sesiones', { body: { email: process.env.ADMIN_EMAIL ?? 'admin@fav.example', contrasena: process.env.ADMIN_PASSWORD ?? 'cambiar-admin-local' } })).json.token;

console.log('\n--- Roles (HU-19) ---');
for (const ruta of [STOCK, '/catalogo/v1/admin/productos', '/ventas/v1/admin/ventas']) {
  const comprador = await pedir('GET', ruta, { token: tokenAna });
  check(`comprador en ${ruta.split('?')[0]}: 403`, comprador.status === 403 && comprador.json.codigo === 'NO_AUTORIZADO', comprador);
  const admin = await pedir('GET', ruta, { token: tokenAdmin });
  check(`administrador en ${ruta.split('?')[0]}: 200`, admin.status === 200, admin.status);
}

console.log('\n--- Compras con cuenta y como invitado ---');
const REMERA = 'var-remera-basica-blanca-m';
await pedir('POST', '/inventario/v1/ingresos', { token: tokenAdmin, headers: { 'Idempotency-Key': randomUUID() }, body: { varianteId: REMERA, canal: 'ONLINE', cantidad: 10 } });
const comprar = (opciones) => pedir('POST', '/ventas/v1/compras', { ...opciones, headers: { 'Idempotency-Key': randomUUID(), ...opciones.headers }, body: { email: 'otro@example.com', medioPago: 'TRANSFERENCIA', items: [{ varianteId: REMERA, cantidad: 1 }] } });

const compraAna = await comprar({ token: tokenAna });
check('la compra con cuenta usa el email del token (RN-11)', compraAna.status === 201 && compraAna.json.email === ana.email, compraAna);
const invitado = await comprar({});
check('un invitado compra sin token, con su email', invitado.status === 201 && invitado.json.email === 'otro@example.com', invitado);
const vencido = await comprar({ token: falsos['vencido'] });
check('con un token vencido no compra como invitado: 401', vencido.status === 401, vencido.status);

const falsificado = await comprar({ headers: { 'X-User-Id': 'id-de-ana', 'X-User-Email': 'ana-falsa@example.com' } });
check('sin token, los X-User-* del cliente se ignoran', falsificado.status === 201 && falsificado.json.email === 'otro@example.com', falsificado);
const conDosIdentidades = await comprar({ token: tokenBea, headers: { 'X-User-Email': ana.email } });
check('con token, gana la identidad del token', conDosIdentidades.status === 201 && conDosIdentidades.json.email === bea.email, conDosIdentidades);

console.log('\n--- Mis compras (HU-14) ---');
const sinSesion = await pedir('GET', '/ventas/v1/mis-compras');
check('sin sesión: 401', sinSesion.status === 401, sinSesion);
const deAna = await pedir('GET', '/ventas/v1/mis-compras', { token: tokenAna });
check('ve sus compras, y solo esas', deAna.status === 200 && deAna.json.items.length === 1 && deAna.json.items[0].ventaId === compraAna.json.ventaId, deAna);
const ajena = await pedir('GET', `/ventas/v1/mis-compras/${compraAna.json.ventaId}`, { token: tokenBea });
check('una compra ajena: 404', ajena.status === 404, ajena.status);
const propia = await pedir('GET', `/ventas/v1/mis-compras/${compraAna.json.ventaId}`, { token: tokenAna });
check('la propia: 200', propia.status === 200, propia.status);
const cancelarAjena = await pedir('POST', `/ventas/v1/compras/${compraAna.json.ventaId}/cancelar`, { token: tokenBea });
check('cancelar una compra ajena: 404', cancelarAjena.status === 404, cancelarAjena.status);
const cancelarSinToken = await pedir('POST', `/ventas/v1/compras/${compraAna.json.ventaId}/cancelar`);
check('cancelar una compra con cuenta sin sesión: 404', cancelarSinToken.status === 404, cancelarSinToken.status);
const cancelarPropia = await pedir('POST', `/ventas/v1/compras/${compraAna.json.ventaId}/cancelar`, { token: tokenAna });
check('cancelar la propia: 200', cancelarPropia.status === 200 && cancelarPropia.json.estado === 'CANCELADA', cancelarPropia);

console.log('\n--- Rutas que no piden token ---');
const productos = await pedir('GET', '/v1/productos', { headers: { 'X-API-Key': process.env.CAPACIDAD_API_KEY ?? 'clave-grupo-proveedores' } });
check('la capacidad publicada sigue con su clave', productos.status === 200, productos.status);
const aviso = await pedir('POST', '/ventas/v1/pagos/aviso', { headers: { 'X-Signature': 'ts=1,v1=abc', 'X-Request-Id': 'x' }, body: { type: 'payment', data: { id: 1 } } });
check('el aviso de pago llega a Ventas, que rechaza la firma', aviso.status === 401 && aviso.json.detail?.includes('firma'), aviso);

console.log('\n--- Con Usuarios caído (ADR-001) ---');
dc('stop usuarios');
const conCaida = await pedir('GET', STOCK, { token: tokenAdmin });
check('la sesión iniciada sigue valiendo: el gateway no llama a Usuarios', conCaida.status === 200, conCaida.status);
const compraConCaida = await comprar({ token: tokenAna });
check('un comprador con sesión puede comprar', compraConCaida.status === 201 && compraConCaida.json.email === ana.email, compraConCaida);
dc('start usuarios');
await espera(3000);

console.log(`\nResultado: ${ok} OK, ${mal} fallas`);
process.exit(mal ? 1 : 0);
