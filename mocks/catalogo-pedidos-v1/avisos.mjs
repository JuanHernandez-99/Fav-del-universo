// Envío de avisos pedidoActualizado como lo hace FAV: firmados, con eventoId fijo entre
// reintentos y reintento solo ante fallas del receptor (no ante 401).
// En el mock los reintentos son rápidos (2 s y 5 s); en FAV, 1, 5, 15 y 60 min.
import { randomUUID } from 'node:crypto';
import { firmar } from './firma.mjs';

const ESPERAS_MS = [0, 2000, 5000];
const TIMEOUT_MS = 5000;

export function nuevoEvento({ pedidoId, referenciaExterna, estado }) {
  const pedido = { pedidoId, estado };
  if (referenciaExterna) pedido.referenciaExterna = referenciaExterna;
  return {
    eventoId: randomUUID(),
    tipo: 'pedido.actualizado',
    ocurridoEn: new Date().toISOString(),
    pedido,
  };
}

// opciones: secreto, correlationId, firmaInvalida (para probar el rechazo),
// atrasoSegundos (para probar la frescura), log (función).
export async function enviarAviso(url, evento, opciones = {}) {
  const { secreto = 'secreto-de-prueba', correlationId = randomUUID(), log = console.log } = opciones;
  const cuerpo = JSON.stringify(evento);
  for (let intento = 0; intento < ESPERAS_MS.length; intento++) {
    if (ESPERAS_MS[intento] > 0) await new Promise((r) => setTimeout(r, ESPERAS_MS[intento]));
    const t = Math.floor(Date.now() / 1000) - (opciones.atrasoSegundos ?? 0);
    const firma = opciones.firmaInvalida ? firmar('otro-secreto', cuerpo, t) : firmar(secreto, cuerpo, t);
    try {
      const resp = await fetch(url, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'X-FAV-Firma': firma,
          'X-FAV-Evento-Id': evento.eventoId,
          'X-Correlation-Id': correlationId,
          'User-Agent': 'FAV-Avisos/1.1 (mock)',
        },
        body: cuerpo,
        signal: AbortSignal.timeout(TIMEOUT_MS),
      });
      log(`aviso ${evento.eventoId} (${evento.pedido.estado}) → ${url}: intento ${intento + 1}, respuesta ${resp.status}`);
      if (resp.ok) return { entregado: true, status: resp.status };
      if (resp.status === 401) return { entregado: false, status: 401 }; // firma rechazada: no se reintenta
    } catch (err) {
      log(`aviso ${evento.eventoId} → ${url}: intento ${intento + 1} falló (${err.cause?.code ?? err.name})`);
    }
  }
  log(`aviso ${evento.eventoId}: agotados los intentos; el estado sigue disponible con GET /v1/pedidos/{pedidoId}`);
  return { entregado: false };
}
