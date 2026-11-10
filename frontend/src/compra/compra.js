// Lógica de la compra, sin React.

const CLAVE_INTENTO = 'fav-compra-intento-v1';
const PREFIJO_VENTA = 'fav-compra-';

// crypto.randomUUID solo existe en HTTPS o localhost; la clave de idempotencia hace falta en cualquier caso.
function generarClave() {
  if (crypto.randomUUID) return crypto.randomUUID();
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), (b) => b.toString(16).padStart(2, '0')).join('');
}

// Todo el contenido del pedido, con las prendas en un orden fijo: el orden en que se eligieron no lo cambia.
const huella = ({ items, ...resto }) => JSON.stringify([
  Object.entries(resto).sort(([a], [b]) => a.localeCompare(b)),
  items.map((i) => [i.varianteId, i.cantidad]).sort(),
]);

// Devuelve la clave de idempotencia de este pedido: una compra o una venta del local. Reintentar el mismo pedido,
// por ejemplo tras un error de red, usa la misma clave y no crea otra venta (HU-11.8). Si cambió algo, usa una
// nueva: Ventas rechaza la misma clave con otro contenido.
export function claveDelPedido(pedido, almacen = sessionStorage, generar = generarClave) {
  const actual = huella(pedido);
  try {
    const guardado = JSON.parse(almacen.getItem(CLAVE_INTENTO));
    if (guardado?.huella === actual && guardado.clave) return guardado.clave;
  } catch {
    // Dato inválido: se genera una clave nueva.
  }
  const clave = generar();
  try {
    almacen.setItem(CLAVE_INTENTO, JSON.stringify({ huella: actual, clave }));
  } catch {
    // Sin almacenamiento, el reintento dentro de esta página igual usa lo que devuelva esta función de nuevo.
  }
  return clave;
}

export function olvidarClave(almacen = sessionStorage) {
  try {
    almacen.removeItem(CLAVE_INTENTO);
  } catch {
    // Nada que borrar.
  }
}

// La venta queda en la pestaña: el invitado no tiene otra forma de volver a ver los datos para pagar (RN-24).
export function guardarVenta(venta, almacen = sessionStorage) {
  try {
    almacen.setItem(PREFIJO_VENTA + venta.ventaId, JSON.stringify(venta));
  } catch {
    // Almacenamiento bloqueado: la pantalla sigue con lo que tiene en memoria.
  }
}

export function leerVenta(id, almacen = sessionStorage) {
  try {
    return JSON.parse(almacen.getItem(PREFIJO_VENTA + id));
  } catch {
    return null;
  }
}

// Al cerrar la sesión no queda nada de la compra en la pestaña: la computadora puede ser compartida.
export function olvidarVentas(almacen = sessionStorage) {
  try {
    const claves = Array.from({ length: almacen.length }, (_, i) => almacen.key(i));
    for (const clave of claves) {
      if (clave.startsWith(PREFIJO_VENTA)) almacen.removeItem(clave);
    }
  } catch {
    // Nada que borrar.
  }
}

export const MEDIOS = {
  MERCADO_PAGO: { nombre: 'Mercado Pago', detalle: 'Pagás ahora. Te reservamos las prendas por 20 minutos.' }, // RN-08
  TRANSFERENCIA: { nombre: 'Transferencia bancaria', detalle: 'Te reservamos las prendas hasta 48 horas, hasta que confirmemos el pago.' }, // RN-09
};

export const ESTADOS = {
  PENDIENTE_DE_PAGO: { nombre: 'Pendiente de pago', tono: 'pendiente' },
  PAGADA: { nombre: 'Pagada', tono: 'bien' },
  ENTREGADA_AL_CORREO: { nombre: 'Entregada al correo', tono: 'bien' },
  ENTREGADA: { nombre: 'Entregada', tono: 'bien' },
  CANCELADA: { nombre: 'Cancelada', tono: 'mal' },
  VENCIDA: { nombre: 'Vencida', tono: 'mal' },
  A_VERIFICAR: { nombre: 'A verificar', tono: 'pendiente' },
};

// El mensaje para el comprador y qué hacer después. "reintentable": el mismo pedido puede funcionar más tarde.
export function explicarError(error) {
  switch (error.codigo) {
    case 'STOCK_INSUFICIENTE':
      return { mensaje: 'Mientras comprabas se agotaron unidades de alguna prenda. No se reservó nada.', volverAlCarrito: true };
    case 'VARIANTE_INEXISTENTE':
      return { mensaje: 'Alguna prenda ya no está en la tienda. No se reservó nada.', volverAlCarrito: true };
    case 'PAGO_NO_DISPONIBLE':
      return { mensaje: 'Mercado Pago no responde en este momento. Probá de nuevo en un rato o elegí transferencia.', reintentable: true };
    case 'NO_AUTENTICADO':
      return { mensaje: 'Tu sesión venció. Ingresá de nuevo o comprá sin cuenta.', sesionVencida: true };
    default:
  }
  return { mensaje: 'No pudimos procesar la compra. No se te cobró nada: probá de nuevo en un momento.', reintentable: true };
}
