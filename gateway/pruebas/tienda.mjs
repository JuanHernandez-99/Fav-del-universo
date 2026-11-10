// Prueba las rutas públicas de la tienda contra el sistema levantado: node gateway/pruebas/tienda.mjs
import { randomUUID } from 'node:crypto';
import { execSync } from 'node:child_process';
import { request } from 'node:http';

const GW = 'http://localhost:8080';
const TIENDA = `${GW}/catalogo/v1/tienda/productos`;
let ok = 0, mal = 0;
const check = (n, c, e = '') => { c ? ok++ : mal++; console.log(`${c ? 'OK   ' : 'FALLA'} ${n}${!c && e !== '' ? ' -> ' + JSON.stringify(e) : ''}`); };
const dc = (cmd) => execSync(`docker compose ${cmd}`, { stdio: ['ignore', 'pipe', 'pipe'] }).toString();
async function http(m, url, { headers = {}, body } = {}) {
  const r = await fetch(url, { method: m, headers: { ...(body ? { 'Content-Type': 'application/json' } : {}), ...headers }, body: body ? JSON.stringify(body) : undefined });
  const t = await r.text(); let json; try { json = JSON.parse(t); } catch { json = t; }
  return { status: r.status, json, headers: r.headers };
}
// fetch resuelve los ../ antes de enviar: acá viajan tal cual, para probar cómo los trata el gateway.
const sinNormalizar = (method, path) => new Promise((ok, mal) => {
  request({ host: 'localhost', port: 8080, method, path }, (r) => { r.resume(); ok(r.statusCode); }).on('error', mal).end();
});
// Reintenta cada 500 ms; por defecto, hasta 30 s.
const esperar = async (cond, intentos = 60) => { for (let i = 0; i < intentos; i++) { try { if (await cond()) return true; } catch {} await new Promise((f) => setTimeout(f, 500)); } return false; };

const sesion = await http('POST', `${GW}/usuarios/v1/sesiones`, { body: { email: 'admin@fav.example', contrasena: 'cambiar-admin-local' } });
const ADMIN = { Authorization: `Bearer ${sesion.json.token}` };
const disponibilidad = async (...ids) => (await http('GET', `${GW}/inventario/v1/disponibilidad?variantes=${ids.join(',')}`)).json.items;
const haceUnaHora = new Date(Date.now() - 36e5).toISOString(), enUnaHora = new Date(Date.now() + 36e5).toISOString();

console.log('--- Productos, sin token ---');
const lista = await http('GET', TIENDA);
check('lista sin token: 200 con página', lista.status === 200 && lista.json.items.length > 0 && lista.json.pagina === 1 && lista.json.total >= lista.json.items.length, lista.status);
check('devuelve el correlation id', !!lista.headers.get('x-correlation-id'));
const permitidos = new Set(['productoId', 'nombre', 'descripcion', 'categoria', 'precio', 'precioBase', 'promocion', 'fotos', 'variantes']);
check('sin datos internos ni stock', lista.json.items.every((p) => Object.keys(p).every((c) => permitidos.has(c)) && Array.isArray(p.fotos)), lista.json.items[0]);

const sufijo = randomUUID().slice(0, 8);
const nombre = `Buzo tienda ${sufijo}`;
const creado = await http('POST', `${GW}/catalogo/v1/admin/productos`, { headers: ADMIN, body: {
  nombre, categoria: 'buzos', sistemaTalles: 'LETRAS', precioBase: { monto: 10000, moneda: 'ARS' },
  variantes: [{ talle: 'M', color: 'Gris' }, { talle: 'L', color: 'Gris' }],
  fotos: [{ url: 'https://fav.example/buzo.jpg', color: null }, { url: 'https://fav.example/buzo-gris.jpg', color: 'Gris' }],
} });
check('crea un buzo no publicado', creado.status === 201, creado);
const id = creado.json.productoId, [varM, varL] = creado.json.variantes.map((v) => v.varianteId);
check('no publicado: no aparece en el listado', (await http('GET', `${TIENDA}?tamano=100&categoria=buzos`)).json.items.every((p) => p.productoId !== id));
check('no publicado: la ficha da 404', (await http('GET', `${TIENDA}/${id}`)).status === 404);
check('publicado=false: 400', (await http('GET', `${TIENDA}?publicado=false`)).status === 400);
check('parámetro desconocido: 400', (await http('GET', `${TIENDA}?categorias=buzos`)).status === 400);
await http('POST', `${GW}/catalogo/v1/admin/productos/${id}/publicar`, { headers: ADMIN });

const ficha = await http('GET', `${TIENDA}/${id}`);
check('publicado: ficha con fotos por color (HU-03.5)', ficha.status === 200 && ficha.json.fotos.length === 2 && ficha.json.fotos[1].color === 'Gris', ficha);
check('sin promoción: mismo precio y sin promoción', ficha.json.precio === 10000 && ficha.json.precioBase === 10000 && !ficha.json.promocion, ficha.json);
// El índice se actualiza por un evento, no al publicar: se espera hasta el retraso máximo de ADR-006 (10 s).
const desdePublicar = Date.now();
const encontrado = await esperar(async () => (await http('GET', `${TIENDA}?q=${sufijo}`)).json.items.some((p) => p.productoId === id), 20);
check(`la búsqueda por texto lo encuentra en menos de 10 s (tardó ${((Date.now() - desdePublicar) / 1000).toFixed(1)} s)`, encontrado);

const filtrado = await http('GET', `${TIENDA}?categoria=buzos&talle=m&color=Gris&orden=precio_asc&tamano=100`);
check('filtros: lo incluye y todos cumplen', filtrado.json.items.some((p) => p.productoId === id) && filtrado.json.items.every((p) => p.categoria === 'buzos'), filtrado.json.total);
check('filtro sin coincidencias: lista vacía', (await http('GET', `${TIENDA}?categoria=abanicos&color=Inexistente`)).json.items.length === 0);

const vistos = [];
let totalPaginado = 0;
for (let pagina = 1, paginas = 1; pagina <= paginas; pagina++) {
  const p = await http('GET', `${TIENDA}?tamano=1&orden=nombre&pagina=${pagina}`);
  paginas = p.json.paginas; totalPaginado = p.json.total; vistos.push(...p.json.items.map((x) => x.productoId));
}
check('paginar no repite ni saltea (HU-02.4)', new Set(vistos).size === vistos.length && vistos.length === totalPaginado && vistos.includes(id), { vistos: vistos.length, totalPaginado });

console.log('\n--- Promoción (HU-01.4) ---');
const cuerpoPromo = (desde, hasta) => ({ nombre: `Buzo ${sufijo}`, tipo: 'PORCENTAJE', porcentaje: 25, vigencia: { desde, hasta }, alcance: { tipo: 'PRODUCTO', ids: [id] } });
const promo = await http('POST', `${GW}/catalogo/v1/promociones`, { headers: ADMIN, body: cuerpoPromo(haceUnaHora, enUnaHora) });
const conPromo = (await http('GET', `${TIENDA}/${id}`)).json;
check('muestra original y con promoción', promo.status === 201 && conPromo.precio === 7500 && conPromo.precioBase === 10000 && conPromo.promocion?.porcentaje === 25, conPromo);
await http('PUT', `${GW}/catalogo/v1/promociones/${promo.json.promocionId}`, { headers: ADMIN, body: cuerpoPromo('2020-01-01T00:00:00Z', '2020-01-02T00:00:00Z') });
check('cancelada: vuelve al precio base', (await http('GET', `${TIENDA}/${id}`)).json.precio === 10000);

console.log('\n--- Unidades disponibles, sin token ---');
const inicial = await http('GET', `${GW}/inventario/v1/disponibilidad?variantes=${varM},${varL}`);
check('sin token y sin stock: no hay', inicial.status === 200 && inicial.json.items.every((d) => d.disponible === 0), inicial);
check('solo informa las unidades online', inicial.json.items.every((d) => Object.keys(d).sort().join() === 'disponible,varianteId'), inicial.json);
await http('POST', `${GW}/inventario/v1/ingresos`, { headers: { ...ADMIN, 'Idempotency-Key': randomUUID() }, body: { varianteId: varM, canal: 'FISICO', cantidad: 5 } });
check('stock solo en el local: no hay online', (await disponibilidad(varM))[0].disponible === 0);
await http('POST', `${GW}/inventario/v1/ingresos`, { headers: { ...ADMIN, 'Idempotency-Key': randomUUID() }, body: { varianteId: varM, canal: 'ONLINE', cantidad: 1 } });
const [m, l] = await disponibilidad(varM, varL);
check('con 1 online: 1, y el otro talle 0', m.disponible === 1 && l.disponible === 0, [m, l]);
const compra = await http('POST', `${GW}/ventas/v1/compras`, { headers: { 'Idempotency-Key': randomUUID() }, body: { email: 'ana@example.com', medioPago: 'TRANSFERENCIA', items: [{ varianteId: varM, cantidad: 1 }] } });
check('la compra reserva la última', compra.status === 201, compra);
check('reservada: baja a 0, al instante', (await disponibilidad(varM))[0].disponible === 0);
check('sin ids: 400', (await http('GET', `${GW}/inventario/v1/disponibilidad`)).status === 400);

console.log('\n--- Lo demás sigue protegido ---');
for (const [metodo, ruta] of [
  ['GET', `/inventario/v1/stock?variantes=${varM}`], ['POST', '/inventario/v1/ingresos'], ['GET', '/catalogo/v1/admin/productos'],
  ['POST', `/catalogo/v1/admin/productos/${id}/publicar`], ['GET', '/catalogo/v1/promociones'], ['GET', `/catalogo/v1/variantes?variantes=${varM}`],
  ['GET', `/catalogo/v1/tienda/productos/${id}/variantes`], ['POST', `/catalogo/v1/tienda/productos/${id}/publicar`],
  ['GET', '/catalogo/v1/tienda/productos/../../admin/productos'], ['GET', '/catalogo/v1/tienda/productos/..%2f..%2fadmin%2fproductos'],
  ['GET', '/inventario/v1/disponibilidad/../stock?variantes=x'], ['GET', '/inventario/v1/disponibilidad%2f..%2fstock?variantes=x'],
]) {
  const codigo = await sinNormalizar(metodo, ruta);
  check(`${metodo} ${ruta.replace(id, '{id}')}: 401`, codigo === 401, codigo);
}
check('POST a la tienda: 405', (await http('POST', TIENDA, { body: {} })).status === 405);
check('POST a disponibilidad: 405', (await http('POST', `${GW}/inventario/v1/disponibilidad?variantes=x`, { body: {} })).status === 405);

console.log('\n--- Caídas ---');
dc('stop inventario');
const sinInv = await http('GET', `${GW}/inventario/v1/disponibilidad?variantes=${varM}`);
check('Inventario caído: no responde 0', sinInv.status >= 500, sinInv.status);
console.log(`      (código ${sinInv.status}, tipo ${sinInv.headers.get('content-type')})`);
check('Inventario caído: los productos se siguen viendo', (await http('GET', TIENDA)).status === 200);
dc('start inventario');
check('Inventario vuelve', await esperar(async () => (await http('GET', `${GW}/inventario/v1/disponibilidad?variantes=${varM}`)).status === 200));
await new Promise((f) => setTimeout(f, 12000)); // tras reiniciar todas las réplicas, ver "Negativas" en ADR-012
dc('stop catalogo');
const sinCat = await http('GET', TIENDA);
check('Catálogo caído: error en el listado', sinCat.status >= 500, sinCat.status);
console.log(`      (código ${sinCat.status}, tipo ${sinCat.headers.get('content-type')})`);
const dispSinCat = await http('GET', `${GW}/inventario/v1/disponibilidad?variantes=${varM}`);
check('Catálogo caído: la disponibilidad responde', dispSinCat.status === 200, dispSinCat.status);
dc('start catalogo');
check('Catálogo vuelve', await esperar(async () => (await http('GET', TIENDA)).status === 200));

await http('DELETE', `${GW}/catalogo/v1/admin/productos/${id}`, { headers: ADMIN });
check('despublicado: deja de verse', (await http('GET', `${TIENDA}/${id}`)).status === 404);

console.log(`\nResultado: ${ok} OK, ${mal} fallas`);
process.exit(mal ? 1 : 0);
