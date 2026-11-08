// Lógica de la pantalla del gimnasio (ADR-018), sin React. Las reglas las aplica Inventario.

export const ESTADOS_REPOSICION = {
  PENDIENTE_CONFIRMACION: { nombre: 'Pendiente', tono: 'pendiente', ayuda: 'Se está enviando o el gimnasio no respondió. Se reintenta sola, sin duplicar la entrega.' },
  CONFIRMADA: { nombre: 'Confirmada', tono: 'bien', ayuda: 'El gimnasio la recibió.' },
  RECHAZADA: { nombre: 'Rechazada', tono: 'mal', ayuda: 'El gimnasio no la registró. Las unidades volvieron al stock físico.' },
  REQUIERE_REVISION: { nombre: 'A revisar', tono: 'mal', ayuda: 'El gimnasio informó un conflicto con otra entrega. Hay que revisarla con ellos: las unidades no volvieron al stock.' },
  BLOQUEADA_CONFIGURACION: { nombre: 'Bloqueada', tono: 'mal', ayuda: 'El gimnasio no aceptó nuestra credencial. Se reintenta cada 5 minutos y se destraba sola al corregirla.' },
};

export const estadoReposicion = (estado) => ESTADOS_REPOSICION[estado] ?? { nombre: estado, tono: 'neutro', ayuda: '' };

// Agrega nombre, color y talle a cada fila. Una variante que Catálogo no conoce se muestra con su id.
export function conDetalle(filas, detalles) {
  const porId = Object.fromEntries(detalles.map((d) => [d.varianteId, d]));
  return filas.map((f) => ({ ...f, detalle: porId[f.varianteId] }));
}

export function explicarRechazoReposicion(error) {
  if (error.codigo === 'STOCK_INSUFICIENTE') return `No alcanza: en el local hay ${error.faltantes?.[0]?.disponible ?? 0} unidades. Las reposiciones salen del stock físico.`;
  if (error.codigo === 'VARIANTE_INEXISTENTE') return 'Esa variante todavía no tiene stock en Inventario: primero cargá un ingreso.';
  if (error.codigo === 'PEDIDO_INVALIDO') return (error.errores ?? []).map((c) => `${c.campo}: ${c.mensaje}`).join('. ') || 'Revisá los datos.';
  return 'No pudimos registrar la reposición en este momento. Probá de nuevo: no se va a aplicar dos veces.';
}
