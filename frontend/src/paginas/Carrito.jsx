import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router';
import { consultarDisponibilidad, ErrorApi, obtenerProducto } from '../api.js';
import { useCarrito } from '../carrito/contexto.jsx';
import { calcularSubtotal, revisar, sePuedeComprar } from '../carrito/carrito.js';
import { formatearPrecio } from '../dominio.js';
import FotoPrenda from '../componentes/FotoPrenda.jsx';

const AVISOS = {
  NO_PUBLICADO: 'Esta prenda ya no está en la tienda. Quitala para poder comprar.',
  SIN_STOCK: 'Se quedó sin stock. Quitala para poder comprar.',
  SIN_DATOS: 'Ahora no podemos consultar si hay stock.',
};

const textoDisponible = (n) => (n === 1 ? 'Queda 1 unidad.' : `Quedan ${n} unidades.`);

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

export default function Carrito() {
  const { items, cambiarCantidad, quitar } = useCarrito();
  const [productos, setProductos] = useState({});
  const [disponibilidad, setDisponibilidad] = useState(undefined);
  const [revisando, setRevisando] = useState(true);

  // Se vuelve a revisar solo si cambian las prendas, no al tocar una cantidad.
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
  }, [claveVariantes, claveProductos]);

  const revisados = useMemo(() => {
    if (revisando) return items;
    const lineas = revisar(items, productos, disponibilidad ?? null);
    // Sin respuesta de Catálogo no se puede saber si sigue publicada ni cuánto sale.
    return lineas.map((l) => (productos[l.productoId] === undefined && !l.problema ? { ...l, problema: 'SIN_DATOS' } : l));
  }, [items, productos, disponibilidad, revisando]);

  if (items.length === 0) {
    return (
      <div className="estado-vacio pagina-angosta">
        <svg className="estado-icono" viewBox="0 0 64 64" aria-hidden="true"><path d="M16 22h32l3 34H13l3-34Z" /><path d="M24 28v-9a8 8 0 0 1 16 0v9" /></svg>
        <h1>Tu carrito está vacío</h1>
        <p>Cuando elijas una prenda, va a aparecer acá.</p>
        <Link to="/" className="boton">Ir a la tienda</Link>
      </div>
    );
  }

  const subtotal = calcularSubtotal(revisados);
  const hayCambioDePrecio = revisados.some((i) => i.precioCambio);
  const listo = !revisando && sePuedeComprar(revisados);

  return (
    <div className="carrito">
      <h1>Tu carrito</h1>
      <div className="carrito-columnas">
        <ul className="carrito-lista">
          {revisados.map((item) => (
            <li key={item.varianteId} className={`carrito-linea${item.problema && item.problema !== 'SIN_DATOS' ? ' con-problema' : ''}`}>
              <Link to={`/producto/${item.productoId}`} className="carrito-foto" tabIndex={-1} aria-hidden="true">
                <FotoPrenda foto={item.foto} categoria={item.categoria} color={item.color} nombre="" />
              </Link>
              <div className="carrito-datos">
                <h2><Link to={`/producto/${item.productoId}`}>{item.nombre}</Link></h2>
                <p className="carrito-variante">{item.color} · Talle {item.talle === 'U' ? 'único' : item.talle}</p>
                {item.problema === 'NO_ALCANZA' ? (
                  <p className="carrito-aviso" role="alert">
                    {textoDisponible(item.disponible)}{' '}
                    <button type="button" className="boton-texto" onClick={() => cambiarCantidad(item.varianteId, item.disponible)}>Dejar {item.disponible}</button>
                  </p>
                ) : item.problema && <p className="carrito-aviso" role="alert">{AVISOS[item.problema]}</p>}
                {item.precioCambio && !item.problema && <p className="carrito-nota">El precio cambió desde que la agregaste.</p>}
                <div className="carrito-acciones">
                  <div className="cantidad cantidad-chica">
                    <button type="button" aria-label={`Quitar una unidad de ${item.nombre}`} disabled={item.cantidad <= 1} onClick={() => cambiarCantidad(item.varianteId, item.cantidad - 1)}>−</button>
                    <output aria-label="Cantidad">{item.cantidad}</output>
                    <button type="button" aria-label={`Sumar una unidad de ${item.nombre}`} disabled={item.disponible != null && item.cantidad >= item.disponible} onClick={() => cambiarCantidad(item.varianteId, item.cantidad + 1)}>+</button>
                  </div>
                  <button type="button" className="boton-texto" onClick={() => quitar(item.varianteId)}>Quitar</button>
                </div>
              </div>
              <p className="carrito-precio">{formatearPrecio((Math.round(item.precio * 100) * item.cantidad) / 100)}</p>
            </li>
          ))}
        </ul>

        <aside className="carrito-resumen" aria-label="Resumen">
          <h2>Resumen</h2>
          <dl>
            <div><dt>Subtotal</dt><dd>{formatearPrecio(subtotal)}</dd></div>
          </dl>
          <p className="carrito-resumen-nota">
            El total se calcula al comprar, según el medio de pago. El carrito no aparta las prendas: se reservan cuando comprás.
          </p>
          {hayCambioDePrecio && <p className="carrito-resumen-nota">Actualizamos los precios que cambiaron.</p>}
          <button type="button" className="boton boton-ancho" disabled>
            {revisando ? 'Revisando el stock…' : listo ? 'Comprar: próximamente' : 'Revisá tu carrito'}
          </button>
          <Link className="carrito-seguir" to="/">Seguir comprando</Link>
        </aside>
      </div>
    </div>
  );
}
