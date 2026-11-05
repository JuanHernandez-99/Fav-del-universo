// Lógica de la sesión, sin React. El token se guarda en sessionStorage: se borra al cerrar la pestaña (ADR-017).
// El frontend lee el token solo para mostrar el email y saber cuándo vence. Quien lo valida es el gateway (ADR-016).

const CLAVE = 'fav-sesion-v1';

function leerDatos(token) {
  const parte = token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/');
  const bytes = Uint8Array.from(atob(parte), (c) => c.charCodeAt(0));
  return JSON.parse(new TextDecoder().decode(bytes));
}

// Devuelve la sesión, o null si el token no se puede leer o ya venció.
export function sesionDesdeToken(token, ahora = Date.now()) {
  try {
    const { email, role, exp } = leerDatos(token);
    if (!email || !exp || exp * 1000 <= ahora) return null;
    return { token, email, rol: role, venceEn: exp * 1000 };
  } catch {
    return null;
  }
}

export function leerSesion(almacen = sessionStorage, ahora = Date.now()) {
  try {
    const token = almacen.getItem(CLAVE);
    if (!token) return null;
    const sesion = sesionDesdeToken(token, ahora);
    if (!sesion) almacen.removeItem(CLAVE);
    return sesion;
  } catch {
    return null;
  }
}

export function guardarToken(token, almacen = sessionStorage) {
  try {
    almacen.setItem(CLAVE, token);
  } catch {
    // Almacenamiento bloqueado: la sesión dura hasta recargar la página.
  }
}

export function borrarToken(almacen = sessionStorage) {
  try {
    almacen.removeItem(CLAVE);
  } catch {
    // Nada que borrar.
  }
}

// Adónde volver después de ingresar. Solo rutas de la tienda: "//otro.sitio" o una URL completa llevarían
// a otra página con un enlace armado por un tercero.
export function rutaDeVuelta(volver) {
  if (!volver || !volver.startsWith('/') || volver.startsWith('//') || volver.startsWith('/\\')) return '/';
  return volver;
}
