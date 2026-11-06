import { useRef, useState } from 'react';
import { Link, Navigate, useNavigate } from 'react-router';
import { comprar } from '../api.js';
import { useCarrito } from '../carrito/contexto.jsx';
import { calcularSubtotal, sePuedeComprar } from '../carrito/carrito.js';
import { useRevision } from '../carrito/useRevision.js';
import { claveDelPedido, explicarError, guardarVenta, MEDIOS, olvidarClave } from '../compra/compra.js';
import { formatearPrecio } from '../dominio.js';
import { useSesion } from '../sesion/contexto.jsx';

export default function Comprar() {
  const { items, vaciar } = useCarrito();
  const { sesion, cerrar } = useSesion();
  const navegar = useNavigate();
  const { revisados, revisando, recargar } = useRevision(items);
  const [email, setEmail] = useState('');
  const [medioPago, setMedioPago] = useState('MERCADO_PAGO');
  const [enviando, setEnviando] = useState(false);
  const [error, setError] = useState(null);
  const [errorEmail, setErrorEmail] = useState('');
  const terminada = useRef(false); // al vaciar el carrito no hay que redirigir como si estuviera vacío

  if (items.length === 0 && !terminada.current) return <Navigate to="/carrito" replace />;

  const listo = !revisando && sePuedeComprar(revisados);
  const hayProblemas = !revisando && !sePuedeComprar(revisados);

  const enviar = async (e) => {
    e.preventDefault();
    setError(null);
    setErrorEmail('');
    if (!sesion && !email.trim()) { // HU-11.2
      setErrorEmail('Necesitamos tu email para avisarte de la compra.');
      return;
    }
    const pedido = {
      email: sesion ? sesion.email : email.trim(),
      medioPago,
      items: items.map(({ varianteId, cantidad }) => ({ varianteId, cantidad })),
    };
    setEnviando(true);
    try {
      const venta = await comprar(pedido, claveDelPedido(pedido), sesion?.token);
      guardarVenta(venta);
      olvidarClave();
      terminada.current = true;
      vaciar();
      navegar(`/compra/${venta.ventaId}`, { replace: true });
    } catch (falla) {
      const email_ = falla.errores?.find((c) => c.campo === 'email');
      if (email_) {
        setErrorEmail(email_.mensaje[0].toUpperCase() + email_.mensaje.slice(1));
      } else {
        const explicacion = explicarError(falla);
        setError(explicacion);
        if (explicacion.sesionVencida) cerrar();
        if (explicacion.volverAlCarrito) recargar();
      }
      setEnviando(false);
    }
  };

  return (
    <div className="carrito compra">
      <h1>Comprar</h1>
      <div className="carrito-columnas">
        <form id="formulario-compra" className="compra-formulario" onSubmit={enviar} noValidate>
          {error && (
            <div className="acceso-error" role="alert">
              {error.mensaje}{' '}
              {error.volverAlCarrito && <Link to="/carrito">Revisar el carrito</Link>}
            </div>
          )}

          <section>
            <h2><span aria-hidden="true">1</span> Tus datos</h2>
            {sesion ? (
              <p className="compra-cuenta">Comprás con tu cuenta: <strong>{sesion.email}</strong>. Ahí te llegan los avisos.</p>
            ) : (
              <>
                <label className="acceso-campo">
                  <span>Email</span>
                  <input type="email" name="email" autoComplete="email" required value={email} aria-invalid={Boolean(errorEmail)}
                    onChange={(e) => setEmail(e.target.value)} />
                  {errorEmail
                    ? <small role="alert">{errorEmail}</small>
                    : <small className="acceso-ayuda">Sin cuenta, el estado de tu compra te llega por mail.</small>}
                </label>
                <p className="compra-cuenta">¿Tenés cuenta? <Link to={`/ingresar?volver=${encodeURIComponent('/comprar')}`}>Ingresá</Link> para ver tus compras después.</p>
              </>
            )}
          </section>

          <section>
            <h2><span aria-hidden="true">2</span> Medio de pago</h2>
            <div className="compra-medios" role="radiogroup" aria-label="Medio de pago">
              {Object.entries(MEDIOS).map(([id, medio]) => (
                <label key={id} className={`compra-medio${medioPago === id ? ' elegido' : ''}`}>
                  <input type="radio" name="medioPago" value={id} checked={medioPago === id} onChange={() => setMedioPago(id)} />
                  <span><strong>{medio.nombre}</strong><small>{medio.detalle}</small></span>
                </label>
              ))}
            </div>
          </section>
          <p className="compra-entrega">La entrega se acuerda con el vendedor después de la compra.</p>
        </form>

        <aside className="carrito-resumen" aria-label="Resumen">
          <h2>Tu pedido</h2>
          <ul className="compra-lineas">
            {revisados.map((i) => (
              <li key={i.varianteId}>
                <span>{i.cantidad} × {i.nombre}<small>{i.color} · Talle {i.talle === 'U' ? 'único' : i.talle}</small></span>
                <span>{formatearPrecio((Math.round(i.precio * 100) * i.cantidad) / 100)}</span>
              </li>
            ))}
          </ul>
          <dl><div><dt>Subtotal</dt><dd>{formatearPrecio(calcularSubtotal(revisados))}</dd></div></dl>
          <p className="carrito-resumen-nota">El total final lo calculamos al confirmar, según el medio de pago.</p>
          {hayProblemas && (
            <p className="carrito-aviso" role="alert">Hay prendas para revisar. <Link to="/carrito">Volver al carrito</Link></p>
          )}
          <button type="submit" form="formulario-compra" className="boton boton-ancho" disabled={!listo || enviando}>
            {enviando ? 'Reservando tus prendas…' : revisando ? 'Revisando el stock…' : 'Confirmar compra'}
          </button>
          <Link className="carrito-seguir" to="/carrito">Volver al carrito</Link>
        </aside>
      </div>
    </div>
  );
}
