// Lógica de las promociones (RN-05, HU-05), sin React. Las reglas las valida Catálogo.

export function estadoDe(promocion, ahora = Date.now()) {
  if (new Date(promocion.vigencia.hasta).getTime() <= ahora) return 'TERMINADA';
  if (new Date(promocion.vigencia.desde).getTime() > ahora) return 'PROXIMA';
  return 'VIGENTE';
}

// No hay DELETE: una promoción se termina acortando su vigencia y queda registrada (DISENO, 5.3).
// Si todavía no empezó, toda su vigencia queda en el pasado, para que no llegue a aplicarse.
export function vigenciaParaTerminar(promocion, ahora = Date.now()) {
  const hasta = new Date(ahora).toISOString();
  if (estadoDe(promocion, ahora) === 'PROXIMA') return { desde: new Date(ahora - 1000).toISOString(), hasta };
  return { desde: promocion.vigencia.desde, hasta };
}

// <input type="datetime-local"> trabaja en la hora del navegador, sin zona: "2026-11-30T23:59".
const dos = (n) => String(n).padStart(2, '0');
export function aCampoFecha(iso) {
  if (!iso) return '';
  const f = new Date(iso);
  return `${f.getFullYear()}-${dos(f.getMonth() + 1)}-${dos(f.getDate())}T${dos(f.getHours())}:${dos(f.getMinutes())}`;
}
export const deCampoFecha = (valor) => (valor ? new Date(valor).toISOString() : null);

// Arma el cuerpo que espera Catálogo. medioPago solo va en las de tipo MEDIO_PAGO; ids, vacío en TIENDA.
export function cuerpoDe({ nombre, tipo, porcentaje, medioPago, desde, hasta, alcanceTipo, categorias, productos }) {
  return {
    nombre: nombre.trim(),
    tipo,
    porcentaje: Number(porcentaje),
    medioPago: tipo === 'MEDIO_PAGO' ? medioPago : null,
    vigencia: { desde: deCampoFecha(desde), hasta: deCampoFecha(hasta) },
    alcance: { tipo: alcanceTipo, ids: alcanceTipo === 'CATEGORIA' ? categorias : alcanceTipo === 'PRODUCTO' ? productos.map((p) => p.productoId) : [] },
  };
}
