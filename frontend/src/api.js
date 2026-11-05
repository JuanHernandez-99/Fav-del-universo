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
