// Lógica del alta y la edición de productos (HU-03), sin React. Las reglas las valida Catálogo: esto arma el pedido
// y ayuda a no mandar algo que se va a rechazar.

// RN-02: un solo sistema de talles por producto, con sus valores.
export const SISTEMAS = [
  { id: 'LETRAS', nombre: 'Letras (XS a XXL)', talles: ['XS', 'S', 'M', 'L', 'XL', 'XXL'] },
  { id: 'NUMERICO_PAR', nombre: 'Numérico (32 a 46)', talles: ['32', '34', '36', '38', '40', '42', '44', '46'] },
  { id: 'NUMERICO_1_4', nombre: 'Numérico (1 a 4)', talles: ['1', '2', '3', '4'] },
  { id: 'UNICO', nombre: 'Talle único', talles: ['U'] },
];
export const tallesDe = (sistema) => SISTEMAS.find((s) => s.id === sistema)?.talles ?? [];

// Como Catálogo arma el id de la variante: sin mayúsculas, acentos ni espacios. "Azul marino" y "azul-marino"
// serían la misma variante (DISENO, sección 14, punto 12).
export const normalizarColor = (color) =>
  color.trim().toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '').replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');

// Todas las combinaciones de colores y talles elegidos, sin repetir (HU-03.4).
export function combinarVariantes(colores, talles) {
  const vistos = new Set();
  const unicos = colores.map((c) => c.trim()).filter((c) => {
    const clave = normalizarColor(c);
    if (!clave || vistos.has(clave)) return false;
    vistos.add(clave);
    return true;
  });
  return unicos.flatMap((color) => talles.map((talle) => ({ talle, color })));
}

// Para mostrar los errores de Catálogo junto a cada campo: { "nombre": "es obligatorio", ... }.
export const erroresPorCampo = (errores = []) => Object.fromEntries(errores.map((e) => [e.campo, e.mensaje]));

const NOMBRES_CAMPO = { nombre: 'Nombre', categoria: 'Categoría', 'precioBase.monto': 'Precio', sistemaTalles: 'Sistema de talles', variantes: 'Variantes', version: 'Versión' };
export const nombreCampo = (campo) => NOMBRES_CAMPO[campo] ?? campo.replace(/^fotos\[(\d+)\]\.url$/, (_, i) => `Foto ${Number(i) + 1}`);
