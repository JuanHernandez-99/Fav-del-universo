// Carga prendas de ejemplo con stock, para ver la tienda con datos: node frontend/pruebas/datos-demo.mjs
// Son datos inventados para desarrollar y demostrar. No hace falta correrlo para usar el sistema.
// Se puede repetir: una prenda que ya existe (por su nombre) no se vuelve a crear.
import { randomUUID } from 'node:crypto';

const GW = process.env.GATEWAY_URL ?? 'http://localhost:8080';
const EMAIL = process.env.ADMIN_EMAIL ?? 'admin@fav.example';
const CONTRASENA = process.env.ADMIN_PASSWORD ?? 'cambiar-admin-local';

async function http(metodo, ruta, { token, cuerpo, clave } = {}) {
  const r = await fetch(GW + ruta, {
    method: metodo,
    headers: {
      ...(cuerpo ? { 'Content-Type': 'application/json' } : {}),
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(clave ? { 'Idempotency-Key': clave } : {}),
    },
    body: cuerpo ? JSON.stringify(cuerpo) : undefined,
  });
  const texto = await r.text();
  let datos; try { datos = JSON.parse(texto); } catch { datos = texto; }
  if (!r.ok) throw new Error(`${metodo} ${ruta}: ${r.status} ${texto}`);
  return datos;
}

const combinar = (talles, colores) => colores.flatMap((color) => talles.map((talle) => ({ talle, color })));
const LETRAS = 'LETRAS', NUMERICO = 'NUMERICO_PAR', UNICO = 'UNICO';

// agotadas: índices de variantes que quedan sin stock, para ver cómo se muestra.
const PRENDAS = [
  { nombre: 'Remera Saturno', categoria: 'remeras', sistema: LETRAS, precio: 18900, descripcion: 'Algodón peinado, corte recto y escote redondo.', variantes: combinar(['S', 'M', 'L', 'XL'], ['Blanco', 'Negro', 'Lila']), agotadas: [3] },
  { nombre: 'Musculosa Nébula', categoria: 'tops', sistema: LETRAS, precio: 14500, descripcion: 'Liviana y de tiras anchas.', variantes: combinar(['XS', 'S', 'M'], ['Rosa', 'Negro']) },
  { nombre: 'Body Órbita', categoria: 'bodys', sistema: LETRAS, precio: 21500, descripcion: 'Morley elástico con cierre a presión.', variantes: combinar(['S', 'M', 'L'], ['Negro', 'Bordo']) },
  { nombre: 'Buzo Andrómeda', categoria: 'buzos', sistema: LETRAS, precio: 39900, descripcion: 'Frisa suave, con capucha y bolsillo canguro.', variantes: combinar(['S', 'M', 'L'], ['Gris', 'Violeta']) },
  { nombre: 'Campera Cometa', categoria: 'camperas', sistema: LETRAS, precio: 74900, descripcion: 'Rompeviento con cierre metálico.', variantes: combinar(['S', 'M', 'L'], ['Negro', 'Oliva']), agotadas: [0, 1, 2, 3, 4, 5] },
  { nombre: 'Jean Eclipse', categoria: 'jeans', sistema: NUMERICO, precio: 58900, descripcion: 'Tiro alto y pierna recta, en denim rígido.', variantes: combinar(['36', '38', '40', '42'], ['Celeste', 'Negro']) },
  { nombre: 'Pantalón Marea', categoria: 'pantalones', sistema: NUMERICO, precio: 49900, descripcion: 'Sastrero de pierna ancha, con pinzas.', variantes: combinar(['36', '38', '40'], ['Beige', 'Negro']) },
  { nombre: 'Pollera Aurora', categoria: 'polleras', sistema: LETRAS, precio: 32900, descripcion: 'Midi plisada con cintura elástica.', variantes: combinar(['S', 'M', 'L'], ['Verde', 'Crema']) },
  { nombre: 'Short Galaxia', categoria: 'shorts', sistema: NUMERICO, precio: 26900, descripcion: 'Denim gastado con ruedo desflecado.', variantes: combinar(['36', '38', '40'], ['Celeste']) },
  { nombre: 'Vestido Luna', categoria: 'vestidos', sistema: LETRAS, precio: 62900, descripcion: 'Largo, de lino, con tajo lateral.', variantes: combinar(['S', 'M', 'L'], ['Crema', 'Azul marino']) },
  { nombre: 'Conjunto Estela', categoria: 'conjuntos', sistema: LETRAS, precio: 68900, descripcion: 'Top y pantalón de hilo.', variantes: combinar(['S', 'M'], ['Lila', 'Crema']) },
  { nombre: 'Abanico Constelación', categoria: 'abanicos', sistema: UNICO, precio: 9900, descripcion: 'De madera y tela estampada.', variantes: combinar(['U'], ['Azul marino', 'Rojo']) },
  { nombre: 'Riñonera Polaris', categoria: 'accesorios', sistema: UNICO, precio: 23900, descripcion: 'Ecocuero con correa regulable.', variantes: combinar(['U'], ['Negro', 'Marrón']) },
];
const PROMOCIONES = [
  { nombre: 'Semana de remeras', porcentaje: 20, alcance: { tipo: 'CATEGORIA', ids: ['remeras'] } },
  { nombre: 'Semana de vestidos', porcentaje: 15, alcance: { tipo: 'CATEGORIA', ids: ['vestidos'] } },
];

const { token } = await http('POST', '/usuarios/v1/sesiones', { cuerpo: { email: EMAIL, contrasena: CONTRASENA } });

for (const prenda of PRENDAS) {
  const { items } = await http('GET', `/catalogo/v1/admin/productos?categoria=${prenda.categoria}&tamano=100`, { token });
  if (items.some((p) => p.nombre === prenda.nombre)) {
    console.log(`Ya estaba: ${prenda.nombre}`);
    continue;
  }
  const creado = await http('POST', '/catalogo/v1/admin/productos', { token, cuerpo: {
    nombre: prenda.nombre, descripcion: prenda.descripcion, categoria: prenda.categoria, sistemaTalles: prenda.sistema,
    precioBase: { monto: prenda.precio, moneda: 'ARS' }, variantes: prenda.variantes,
  } });
  for (const [i, v] of creado.variantes.entries()) {
    if (prenda.agotadas?.includes(i)) continue;
    await http('POST', '/inventario/v1/ingresos', { token, clave: randomUUID(), cuerpo: { varianteId: v.varianteId, canal: 'ONLINE', cantidad: 5 } });
  }
  await http('POST', `/catalogo/v1/admin/productos/${creado.productoId}/publicar`, { token });
  console.log(`Creada: ${prenda.nombre}`);
}

const existentes = (await http('GET', '/catalogo/v1/promociones', { token })).items;
const desde = new Date(Date.now() - 36e5).toISOString(), hasta = new Date(Date.now() + 30 * 864e5).toISOString();
for (const p of PROMOCIONES) {
  if (existentes.some((e) => e.nombre === p.nombre)) continue;
  await http('POST', '/catalogo/v1/promociones', { token, cuerpo: { ...p, tipo: 'PORCENTAJE', vigencia: { desde, hasta } } });
  console.log(`Promoción: ${p.nombre}`);
}
console.log('Listo.');
