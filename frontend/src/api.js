// Único lugar que habla con el gateway. Las rutas son relativas: tienda y API comparten origen (ADR-017).

export class ErrorApi extends Error {
  constructor(estado, cuerpo) {
    super(cuerpo?.detail || cuerpo?.title || 'No pudimos completar el pedido.');
    this.estado = estado;
    this.codigo = cuerpo?.codigo;
    this.errores = cuerpo?.errores;
  }
}

async function pedir(ruta, opciones) {
  let respuesta;
  try {
    respuesta = await fetch(ruta, opciones);
  } catch {
    throw new ErrorApi(0);
  }
  // Con un servicio caído el gateway puede responder HTML: no se asume JSON.
  const cuerpo = await respuesta.json().catch(() => null);
  if (!respuesta.ok) throw new ErrorApi(respuesta.status, cuerpo);
  return cuerpo;
}

export function listarProductos(filtros, opciones) {
  const parametros = new URLSearchParams();
  for (const [nombre, valor] of Object.entries(filtros)) {
    if (valor !== '' && valor != null) parametros.set(nombre, valor);
  }
  return pedir(`/catalogo/v1/tienda/productos?${parametros}`, opciones);
}

export function obtenerProducto(id, opciones) {
  return pedir(`/catalogo/v1/tienda/productos/${encodeURIComponent(id)}`, opciones);
}

function enviarCredenciales(ruta, email, contrasena) {
  return pedir(ruta, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email, contrasena }),
  });
}

export const registrarUsuario = (email, contrasena) => enviarCredenciales('/usuarios/v1/registro', email, contrasena);
export const iniciarSesion = (email, contrasena) => enviarCredenciales('/usuarios/v1/sesiones', email, contrasena);

const conToken = (token) => (token ? { Authorization: `Bearer ${token}` } : {});

export function comprar(pedido, clave, token) {
  return pedir('/ventas/v1/compras', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'Idempotency-Key': clave, ...conToken(token) },
    body: JSON.stringify(pedido),
  });
}

export function cancelarCompra(id, token) {
  return pedir(`/ventas/v1/compras/${encodeURIComponent(id)}/cancelar`, { method: 'POST', headers: conToken(token) });
}

export function listarMisCompras(token, { limite, desde }, opciones) {
  return pedir(`/ventas/v1/mis-compras?limite=${limite}&desde=${desde}`, { ...opciones, headers: conToken(token) });
}

export function obtenerMiCompra(id, token) {
  return pedir(`/ventas/v1/mis-compras/${encodeURIComponent(id)}`, { headers: conToken(token) });
}

// ---------- Panel de administración ----------

export function listarVentasAdmin(token, { estado, limite, desde }, opciones) {
  const parametros = new URLSearchParams({ limite, desde });
  if (estado) parametros.set('estado', estado);
  return pedir(`/ventas/v1/admin/ventas?${parametros}`, { ...opciones, headers: conToken(token) });
}

export const obtenerVentaAdmin = (id, token) =>
  pedir(`/ventas/v1/admin/ventas/${encodeURIComponent(id)}`, { headers: conToken(token) });

export const confirmarTransferencia = (id, token) =>
  pedir(`/ventas/v1/admin/ventas/${encodeURIComponent(id)}/confirmar-transferencia`, { method: 'POST', headers: conToken(token) });

export const avanzarVenta = (id, token) =>
  pedir(`/ventas/v1/admin/ventas/${encodeURIComponent(id)}/avanzar`, { method: 'POST', headers: conToken(token) });

export function listarProductosAdmin(token, filtros, opciones) {
  const parametros = new URLSearchParams();
  for (const [nombre, valor] of Object.entries(filtros)) if (valor !== '' && valor != null) parametros.set(nombre, valor);
  return pedir(`/catalogo/v1/admin/productos?${parametros}`, { ...opciones, headers: conToken(token) });
}

const rutaProducto = (id) => `/catalogo/v1/admin/productos/${encodeURIComponent(id)}`;
const enviarJSON = (metodo, ruta, token, cuerpo) =>
  pedir(ruta, { method: metodo, headers: { 'Content-Type': 'application/json', ...conToken(token) }, body: JSON.stringify(cuerpo) });

export const obtenerProductoAdmin = (id, token) => pedir(rutaProducto(id), { headers: conToken(token) });
export const crearProducto = (token, producto) => enviarJSON('POST', '/catalogo/v1/admin/productos', token, producto);
export const actualizarProducto = (token, id, datos) => enviarJSON('PUT', rutaProducto(id), token, datos);
export const publicarProducto = (token, id) => pedir(`${rutaProducto(id)}/publicar`, { method: 'POST', headers: conToken(token) });
export const despublicarProducto = (token, id) => pedir(rutaProducto(id), { method: 'DELETE', headers: conToken(token) });
export const agregarVarianteAdmin = (token, id, variante) => enviarJSON('POST', `${rutaProducto(id)}/variantes`, token, variante);

export const buscarProductosAdmin = (token, q, opciones) =>
  pedir(`/catalogo/v1/admin/productos?${new URLSearchParams({ q, tamano: 8 })}`, { ...opciones, headers: conToken(token) });

export const consultarStockAdmin = (token, ids, opciones) =>
  pedir(`/inventario/v1/stock?variantes=${ids.map(encodeURIComponent).join(',')}`, { ...opciones, headers: conToken(token) });

export const sugerirMontoLocal = (token, medioPago, variantes, opciones) =>
  pedir(`/ventas/v1/admin/ventas-local/sugerencia?${new URLSearchParams({ medioPago, variantes })}`, { ...opciones, headers: conToken(token) });

export function registrarVentaLocal(token, pedido, clave) {
  return pedir('/ventas/v1/admin/ventas-local', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'Idempotency-Key': clave, ...conToken(token) },
    body: JSON.stringify(pedido),
  });
}

const MAXIMO_POR_CONSULTA = 200; // límite de la ruta de Inventario

// Devuelve { varianteId: unidades disponibles online } con lo que responde Inventario (HU-01.5).
export async function consultarDisponibilidad(ids, opciones) {
  const unicos = [...new Set(ids)];
  const tandas = [];
  for (let i = 0; i < unicos.length; i += MAXIMO_POR_CONSULTA) {
    tandas.push(unicos.slice(i, i + MAXIMO_POR_CONSULTA));
  }
  const respuestas = await Promise.all(tandas.map((tanda) => {
    const lista = tanda.map(encodeURIComponent).join(',');
    return pedir(`/inventario/v1/disponibilidad?variantes=${lista}`, opciones);
  }));
  const disponibilidad = {};
  for (const { items } of respuestas) {
    for (const { varianteId, disponible } of items) disponibilidad[varianteId] = disponible;
  }
  return disponibilidad;
}
