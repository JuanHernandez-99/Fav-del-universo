// Mock de catalogo-pedidos-v1 (1.1.0).
//
// Prism responde todas las operaciones con los ejemplos del contrato y valida los pedidos.
// Este servidor está adelante de Prism y agrega lo único que Prism no hace: enviar el webhook
// pedidoActualizado FIRMADO (Prism envía el callback sin los headers X-FAV-Firma ni X-FAV-Evento-Id).
//
// Si un POST /v1/pedidos trae una notificacionUrl válida y Prism responde 201, se envía el aviso a esa
// URL a los DEMORA_AVISO_MS. La URL que recibe Prism se reemplaza por un sumidero interno, para que el
// callback sin firmar de Prism no le llegue al grupo.
//
// Headers solo del mock (opcionales) en el POST /v1/pedidos:
//   X-Mock-Aviso: PAGADO (por defecto) | VENCIDO | A_VERIFICAR | NINGUNO
//   X-Mock-Aviso-Duplicado: true   → envía el mismo aviso dos veces, para probar la deduplicación
//
// Variables: PUERTO (8090), PRISM_URL (http://localhost:4010), SUMIDERO_URL,
//            SECRETO_AVISOS (secreto-de-prueba), DEMORA_AVISO_MS (2000).
import http from 'node:http';
import { randomUUID } from 'node:crypto';
import { enviarAviso, nuevoEvento } from './avisos.mjs';

const PUERTO = Number(process.env.PUERTO ?? 8090);
const PRISM_URL = process.env.PRISM_URL ?? 'http://localhost:4010';
const SUMIDERO_URL = process.env.SUMIDERO_URL ?? `http://localhost:${PUERTO}/_mock/sumidero`;
const SECRETO = process.env.SECRETO_AVISOS ?? 'secreto-de-prueba';
const DEMORA_MS = Number(process.env.DEMORA_AVISO_MS ?? 2000);
const ESTADOS = ['PAGADO', 'VENCIDO', 'A_VERIFICAR'];
const SIN_REENVIAR = new Set(['host', 'connection', 'content-length', 'transfer-encoding', 'keep-alive']);

const log = (msg) => console.log(`[mock] ${new Date().toISOString()} ${msg}`);

function leerCuerpo(req) {
  return new Promise((resolve, reject) => {
    const partes = [];
    req.on('data', (p) => partes.push(p));
    req.on('end', () => resolve(Buffer.concat(partes)));
    req.on('error', reject);
  });
}

function urlValida(u) {
  return typeof u === 'string' && u.length <= 500 && /^https?:\/\//.test(u) && URL.canParse(u);
}

function problema(res, status, codigo, title) {
  res.writeHead(status, { 'Content-Type': 'application/problem+json' });
  res.end(JSON.stringify({ type: `https://fav.example/errores/${codigo.toLowerCase().replaceAll('_', '-')}`, title, status, codigo }));
}

http.createServer(async (req, res) => {
  try {
    const ruta = new URL(req.url, 'http://mock').pathname;
    if (ruta === '/_mock/sumidero') { // callback sin firmar de Prism: se descarta
      res.writeHead(204).end();
      return;
    }

    let cuerpo = await leerCuerpo(req);
    let destino = null;
    let referenciaExterna;
    const esPedido = req.method === 'POST' && ruta === '/v1/pedidos';
    if (esPedido && cuerpo.length > 0) {
      try {
        const pedido = JSON.parse(cuerpo.toString('utf8'));
        referenciaExterna = pedido.referenciaExterna;
        if (urlValida(pedido.notificacionUrl)) {
          destino = pedido.notificacionUrl;
          pedido.notificacionUrl = SUMIDERO_URL;
          cuerpo = Buffer.from(JSON.stringify(pedido));
        }
      } catch { /* JSON inválido: Prism responde 400 PEDIDO_INVALIDO */ }
    }

    const headers = Object.fromEntries(Object.entries(req.headers).filter(([k]) => !SIN_REENVIAR.has(k)));
    let resp;
    try {
      resp = await fetch(PRISM_URL + req.url, {
        method: req.method,
        headers,
        body: ['GET', 'HEAD'].includes(req.method) ? undefined : cuerpo,
        signal: AbortSignal.timeout(10000),
      });
    } catch {
      problema(res, 503, 'NO_DISPONIBLE', 'Mock no disponible (Prism todavía arranca)');
      return;
    }
    const respCuerpo = Buffer.from(await resp.arrayBuffer());
    const respHeaders = {};
    resp.headers.forEach((v, k) => { if (!SIN_REENVIAR.has(k) && k !== 'content-encoding') respHeaders[k] = v; });
    // Prism inventa el valor del header; el mock devuelve el recibido o uno nuevo, como FAV.
    respHeaders['x-correlation-id'] = req.headers['x-correlation-id'] ?? randomUUID();
    res.writeHead(resp.status, respHeaders);
    res.end(respCuerpo);

    const estado = String(req.headers['x-mock-aviso'] ?? 'PAGADO').toUpperCase();
    if (!esPedido || resp.status !== 201 || !destino || !ESTADOS.includes(estado)) return;

    const { pedidoId } = JSON.parse(respCuerpo.toString('utf8'));
    const evento = nuevoEvento({ pedidoId, referenciaExterna, estado });
    const duplicar = String(req.headers['x-mock-aviso-duplicado'] ?? '') === 'true';
    const opciones = { secreto: SECRETO, correlationId: respHeaders['x-correlation-id'], log };
    setTimeout(async () => {
      const r = await enviarAviso(destino, evento, opciones);
      if (duplicar && r.entregado) await enviarAviso(destino, evento, opciones);
    }, DEMORA_MS);
  } catch (err) {
    log(`error inesperado: ${err.message}`);
    if (!res.headersSent) problema(res, 500, 'ERROR_INTERNO', 'Error interno del mock');
  }
}).listen(PUERTO, '0.0.0.0', () => log(`escuchando en :${PUERTO} → Prism en ${PRISM_URL}`));
