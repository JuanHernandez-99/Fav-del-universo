import { Link } from 'react-router';
import { useCarrito } from '../carrito/contexto.jsx';
import { calcularSubtotal, sePuedeComprar } from '../carrito/carrito.js';
import { useRevision } from '../carrito/useRevision.js';
import { formatearPrecio } from '../dominio.js';
import FotoPrenda from '../componentes/FotoPrenda.jsx';

const AVISOS = {
  NO_PUBLICADO: 'Esta prenda ya no está en la tienda. Quitala para poder comprar.',
  SIN_STOCK: 'Se quedó sin stock. Quitala para poder comprar.',
  SIN_DATOS: 'Ahora no podemos consultar si hay stock.',
};

const textoDisponible = (n) => (n === 1 ? 'Queda 1 unidad.' : `Quedan ${n} unidades.`);

export default function Carrito() {
  const { items, cambiarCantidad, quitar } = useCarrito();
  const { revisados, revisando } = useRevision(items);

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
          {listo
            ? <Link to="/comprar" className="boton boton-ancho">Comprar</Link>
            : <button type="button" className="boton boton-ancho" disabled>{revisando ? 'Revisando el stock…' : 'Revisá tu carrito'}</button>}
          <Link className="carrito-seguir" to="/">Seguir comprando</Link>
        </aside>
      </div>
    </div>
  );
}
