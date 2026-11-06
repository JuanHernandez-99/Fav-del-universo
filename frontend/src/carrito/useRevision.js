import { useCallback, useEffect, useMemo, useState } from 'react';
import { consultarDisponibilidad, ErrorApi, obtenerProducto } from '../api.js';
import { revisar } from './carrito.js';

// Consulta lo actual de cada producto: null si no está publicado, undefined si Catálogo no respondió.
async function leerProductos(ids, signal) {
  const pares = await Promise.all(ids.map(async (id) => {
    try {
      return [id, await obtenerProducto(id, { signal })];
    } catch (e) {
      return [id, e instanceof ErrorApi && e.estado === 404 ? null : undefined];
    }
  }));
  return Object.fromEntries(pares);
}

// Cruza el carrito con lo que responden Catálogo e Inventario. Se vuelve a consultar si cambian las prendas
// o con recargar(), no al tocar una cantidad.
export function useRevision(items) {
  const [productos, setProductos] = useState({});
  const [disponibilidad, setDisponibilidad] = useState(undefined);
  const [revisando, setRevisando] = useState(true);
  const [vuelta, setVuelta] = useState(0);

  const claveVariantes = items.map((i) => i.varianteId).sort().join(',');
  const claveProductos = [...new Set(items.map((i) => i.productoId))].sort().join(',');

  useEffect(() => {
    if (!claveVariantes) { setRevisando(false); return undefined; }
    const control = new AbortController();
    setRevisando(true);
    (async () => {
      const [leidos, disp] = await Promise.all([
        leerProductos(claveProductos.split(','), control.signal),
        consultarDisponibilidad(claveVariantes.split(','), { signal: control.signal }).catch(() => null),
      ]);
      if (control.signal.aborted) return;
      setProductos(leidos);
      setDisponibilidad(disp);
      setRevisando(false);
    })();
    return () => control.abort();
  }, [claveVariantes, claveProductos, vuelta]);

  const revisados = useMemo(() => {
    if (revisando) return items;
    const lineas = revisar(items, productos, disponibilidad ?? null);
    // Sin respuesta de Catálogo no se puede saber si sigue publicada ni cuánto sale.
    return lineas.map((l) => (productos[l.productoId] === undefined && !l.problema ? { ...l, problema: 'SIN_DATOS' } : l));
  }, [items, productos, disponibilidad, revisando]);

  const recargar = useCallback(() => setVuelta((n) => n + 1), []);

  return { revisados, revisando, recargar };
}
