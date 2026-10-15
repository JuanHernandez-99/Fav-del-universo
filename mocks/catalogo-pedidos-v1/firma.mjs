// Firma de los avisos (webhooks) de FAV, según el contrato catalogo-pedidos-v1 (1.1.0).
//
//   X-FAV-Firma: t=<segundos Unix>,v1=<HMAC-SHA256 en hex de "<t>.<cuerpo crudo>">
//
// La clave del HMAC es el secreto de avisos del grupo. En el mock: "secreto-de-prueba".
// verificar() es la misma lógica que tiene que implementar el receptor, en cualquier lenguaje.
import { createHmac, timingSafeEqual } from 'node:crypto';

export const TOLERANCIA_SEGUNDOS = 5 * 60;

export function firmar(secreto, cuerpo, t = Math.floor(Date.now() / 1000)) {
  const v1 = createHmac('sha256', secreto).update(`${t}.${cuerpo}`).digest('hex');
  return `t=${t},v1=${v1}`;
}

// Devuelve { ok: true } o { ok: false, motivo }.
export function verificar(secreto, cuerpo, header, ahora = Math.floor(Date.now() / 1000)) {
  const partes = Object.fromEntries(
    String(header ?? '').split(',').map((p) => p.split('=', 2)).filter((p) => p.length === 2),
  );
  const t = Number(partes.t);
  if (!Number.isInteger(t) || !/^[0-9a-f]{64}$/.test(partes.v1 ?? '')) {
    return { ok: false, motivo: 'header X-FAV-Firma mal formado' };
  }
  if (Math.abs(ahora - t) > TOLERANCIA_SEGUNDOS) {
    return { ok: false, motivo: 'aviso vencido (t con más de 5 minutos de diferencia)' };
  }
  const esperada = Buffer.from(firmar(secreto, cuerpo, t).split('v1=')[1], 'hex');
  const recibida = Buffer.from(partes.v1, 'hex');
  if (!timingSafeEqual(esperada, recibida)) {
    return { ok: false, motivo: 'firma inválida' };
  }
  return { ok: true };
}
