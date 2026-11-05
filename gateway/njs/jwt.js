// Valida el token de Usuarios sin llamarlo (ADR-001): firma RS256 con la clave pública y vencimiento.
// nginx consulta estas funciones con auth_request antes de pasar el pedido al servicio. Informa la
// identidad en encabezados de respuesta, que nginx reenvía al servicio como X-User-* (ADR-016).
import fs from 'fs';

const RUTA_CLAVE = '/claves/publica/jwt_public.pem';
const LARGO_MAXIMO = 4096;
const ROLES = ['comprador', 'administrador'];
const ALGORITMO = { name: 'RSASSA-PKCS1-v1_5', hash: 'SHA-256' };

function tokenDe(r) {
    const encabezado = r.headersIn.Authorization;
    if (!encabezado) {
        return null;
    }
    return encabezado.startsWith('Bearer ') ? encabezado.slice(7) : '';
}

async function clavePublica() {
    const pem = await fs.promises.readFile(RUTA_CLAVE, 'utf8');
    const base64 = pem.replace(/-----[A-Z ]+-----/g, '').replace(/\s/g, '');
    return crypto.subtle.importKey('spki', Buffer.from(base64, 'base64'), ALGORITMO, false, ['verify']);
}

function deJSON(parte) {
    return JSON.parse(Buffer.from(parte, 'base64url').toString());
}

// claimsValidos devuelve los claims, o null si el token no sirve. El algoritmo se exige acá y no se toma
// del token: si no, un token con "alg":"none" o firmado con la clave pública como secreto pasaría.
async function claimsValidos(token) {
    const partes = token.split('.');
    if (partes.length !== 3 || token.length > LARGO_MAXIMO) {
        return null;
    }
    let cabecera, claims;
    try {
        cabecera = deJSON(partes[0]);
        claims = deJSON(partes[1]);
    } catch (e) {
        return null;
    }
    if (cabecera.alg !== 'RS256') {
        return null;
    }
    const firmado = Buffer.from(partes[0] + '.' + partes[1]);
    const firma = Buffer.from(partes[2], 'base64url');
    const clave = await clavePublica();
    const valida = await crypto.subtle.verify(ALGORITMO, clave, firma, firmado);
    if (!valida) {
        return null;
    }
    const ahora = Math.floor(Date.now() / 1000);
    if (typeof claims.exp !== 'number' || claims.exp <= ahora) {
        return null;
    }
    if (typeof claims.sub !== 'string' || !claims.sub || typeof claims.email !== 'string' || ROLES.indexOf(claims.role) < 0) {
        return null;
    }
    return claims;
}

function informarIdentidad(r, claims) {
    r.headersOut['X-User-Id'] = claims.sub;
    r.headersOut['X-User-Email'] = claims.email;
    r.headersOut['X-User-Role'] = claims.role;
}

// validar es común a las dos rutas. Una falla de lectura de la clave es de FAV, no del token: responde 500.
async function validar(r, exigeAdministrador) {
    const token = tokenDe(r);
    if (token === null) {
        // Sin token: un invitado puede comprar, y el panel lo rechaza.
        r.return(exigeAdministrador ? 401 : 204);
        return;
    }
    let claims;
    try {
        claims = await claimsValidos(token);
    } catch (e) {
        r.error('no se pudo validar el token: ' + e.message);
        r.return(500);
        return;
    }
    if (!claims) {
        // Un token vencido no se trata como invitado: la compra quedaría sin asociar a la cuenta.
        r.return(401);
        return;
    }
    if (exigeAdministrador && claims.role !== 'administrador') {
        r.return(403);
        return;
    }
    informarIdentidad(r, claims);
    r.return(204);
}

function opcional(r) {
    return validar(r, false);
}

function administrador(r) {
    return validar(r, true);
}

export default { opcional, administrador };
