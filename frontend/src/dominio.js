// Datos fijos del negocio: categorías (RN-17) y talles (RN-02).

export const CATEGORIAS = [
  ['tops', 'Tops'], ['bodys', 'Bodys'], ['remeras', 'Remeras'], ['buzos', 'Buzos'], ['camperas', 'Camperas'],
  ['jeans', 'Jeans'], ['pantalones', 'Pantalones'], ['polleras', 'Polleras'], ['shorts', 'Shorts'],
  ['vestidos', 'Vestidos'], ['conjuntos', 'Conjuntos'], ['abanicos', 'Abanicos'], ['accesorios', 'Accesorios'],
];

const NOMBRES_CATEGORIA = Object.fromEntries(CATEGORIAS);
export const nombreCategoria = (id) => NOMBRES_CATEGORIA[id] ?? id;

export const TALLES = ['XS', 'S', 'M', 'L', 'XL', 'XXL', '32', '34', '36', '38', '40', '42', '44', '46', '1', '2', '3', '4', 'U'];

export const ORDENES = [
  ['nuevos', 'Novedades'], ['precio_asc', 'Menor precio'], ['precio_desc', 'Mayor precio'], ['nombre', 'Nombre'],
];

// Al buscar por texto, por defecto se ordena por relevancia (ADR-006). Sin texto no tiene sentido.
export const ordenesPara = (hayTexto) => (hayTexto ? [['relevancia', 'Relevancia'], ...ORDENES] : ORDENES);

// Ordena los talles de una prenda como se leen en una etiqueta, no alfabéticamente.
export const ordenarTalles = (talles) => [...new Set(talles)].sort((a, b) => TALLES.indexOf(a) - TALLES.indexOf(b));

// Sin centavos si el monto es entero ($ 15.000); con los dos si los tiene ($ 9.500,50, no $ 9.500,5).
const pesosEnteros = new Intl.NumberFormat('es-AR', { style: 'currency', currency: 'ARS', maximumFractionDigits: 0 });
const pesosConCentavos = new Intl.NumberFormat('es-AR', { style: 'currency', currency: 'ARS', minimumFractionDigits: 2, maximumFractionDigits: 2 });
export const formatearPrecio = (monto) => (Number.isInteger(monto) ? pesosEnteros : pesosConCentavos).format(monto);

const COLORES = {
  blanco: '#f4efe6', blanca: '#f4efe6', crema: '#efe3cc', beige: '#dcc8a8', negro: '#2b2730', negra: '#2b2730',
  gris: '#9b98a0', celeste: '#a9cbe0', azul: '#34508c', 'azul marino': '#222e52', rojo: '#b8352c', roja: '#b8352c',
  bordo: '#6d1f2f', rosa: '#e8b4b8', fucsia: '#c7367f', violeta: '#6e4a9e', lila: '#bfa6d8', verde: '#4f7a54',
  oliva: '#6d6f3c', amarillo: '#ecc34d', amarilla: '#ecc34d', naranja: '#e0803a', marron: '#6f4a33', 'marrón': '#6f4a33',
};

// El color es texto libre (DISENO, punto 16): lo que no se reconoce toma un tono estable a partir del nombre.
export function colorDeMuestra(nombre = '') {
  const clave = nombre.trim().toLowerCase();
  if (COLORES[clave]) return COLORES[clave];
  let tono = 0;
  for (const letra of clave) tono = (tono * 31 + letra.charCodeAt(0)) % 360;
  return `hsl(${tono} 42% 62%)`;
}

export function esColorClaro(nombre) {
  const claros = ['blanco', 'blanca', 'crema', 'beige', 'celeste', 'rosa', 'lila', 'amarillo', 'amarilla'];
  return claros.includes(nombre.trim().toLowerCase());
}

export const coloresDe = (producto) => [...new Set(producto.variantes.map((v) => v.color))];
