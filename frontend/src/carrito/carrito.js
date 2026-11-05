// Lógica del carrito, sin React. Vive en el navegador y no reserva stock (HU-10, ADR-001).

const CLAVE = 'fav-carrito-v1';

export function leerCarrito(almacen = localStorage) {
  try {
    const guardado = JSON.parse(almacen.getItem(CLAVE));
    if (!Array.isArray(guardado)) return [];
    return guardado.filter((i) => i?.varianteId && Number.isInteger(i.cantidad) && i.cantidad > 0);
  } catch {
    return [];
  }
}

export function guardarCarrito(items, almacen = localStorage) {
  try {
    almacen.setItem(CLAVE, JSON.stringify(items));
  } catch {
    // Almacenamiento lleno o bloqueado: el carrito sigue en memoria hasta recargar.
  }
}

export function agregar(items, nuevo, cantidad = 1) {
  const existente = items.find((i) => i.varianteId === nuevo.varianteId);
  if (existente) return cambiarCantidad(items, nuevo.varianteId, existente.cantidad + cantidad);
  return [...items, { ...nuevo, cantidad }];
}

export function cambiarCantidad(items, varianteId, cantidad) {
  if (cantidad < 1) return quitar(items, varianteId);
  return items.map((i) => (i.varianteId === varianteId ? { ...i, cantidad } : i));
}

export function quitar(items, varianteId) {
  return items.filter((i) => i.varianteId !== varianteId);
}

export const contarUnidades = (items) => items.reduce((suma, i) => suma + i.cantidad, 0);

export const calcularSubtotal = (items) =>
  items.reduce((suma, i) => suma + Math.round(i.precio * 100) * i.cantidad, 0) / 100;

// Cruza el carrito con lo que responden Catálogo e Inventario ahora (HU-10.3).
// productos: { productoId: producto | null si no está publicado }. disponibilidad: null si Inventario no respondió.
// El precio guardado se reemplaza por el actual: el que vale es el que calcula el servidor (RN-22).
export function revisar(items, productos, disponibilidad) {
  return items.map((item) => {
    const producto = productos[item.productoId];
    if (producto === null) return { ...item, problema: 'NO_PUBLICADO' };
    const precio = producto?.precio ?? item.precio;
    const precioCambio = producto != null && producto.precio !== item.precio;
    const conPrecio = { ...item, precio, precioCambio };
    if (producto && !producto.variantes.some((v) => v.varianteId === item.varianteId)) {
      return { ...conPrecio, problema: 'NO_PUBLICADO' };
    }
    if (disponibilidad === null) return { ...conPrecio, problema: 'SIN_DATOS' };
    if (disponibilidad[item.varianteId] !== true) return { ...conPrecio, problema: 'SIN_STOCK' };
    return conPrecio;
  });
}

export const sePuedeComprar = (revisados) => revisados.length > 0 && revisados.every((i) => !i.problema);
