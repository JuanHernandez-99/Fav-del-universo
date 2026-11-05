import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router';
import { buscarProductosAdmin, consultarStockAdmin, ErrorApi, registrarVentaLocal, sugerirMontoLocal } from '../../api.js';
import { agregar, cambiarCantidad, quitar } from '../../carrito/carrito.js';
import { claveDelPedido, olvidarClave } from '../../compra/compra.js';
import { formatearPrecio, nombreCategoria, TALLES } from '../../dominio.js';
import { useSesion } from '../../sesion/contexto.jsx';
import { fisicoDespues, leerMonto, lineasParaSugerencia, MAXIMO_LINEAS, MEDIOS_LOCAL } from './ventaLocal.js';

const ESPERA_BUSQUEDA = 300;

function Buscador({ token, alElegir, alFallar }) {
  const [texto, setTexto] = useState('');
  const [resultados, setResultados] = useState(null);

  useEffect(() => {
    const q = texto.trim();
    if (q.length < 2) { setResultados(null); return undefined; }
    const control = new AbortController();
    const temporizador = setTimeout(() => {
      buscarProductosAdmin(token, q, { signal: control.signal })
        .then((r) => setResultados(r.items))
        .catch((e) => { if (!control.signal.aborted) { setResultados([]); alFallar(e); } });
    }, ESPERA_BUSQUEDA);
    return () => { clearTimeout(temporizador); control.abort(); };
  }, [texto, token, alFallar]);

  return (
    <section className="local-buscador">
      <label className="acceso-campo">
        <span>Buscar la prenda</span>
        <input type="search" value={texto} onChange={(e) => setTexto(e.target.value)} placeholder="Nombre o descripción, por ejemplo “jean”" />
      </label>
      {resultados?.length === 0 && <p className="panel-vacio">No hay prendas con ese texto.</p>}
      {resultados?.length > 0 && (
        <ul className="local-resultados">
          {resultados.map((p) => (
            <li key={p.productoId}>
              <p className="local-producto">
                <strong>{p.nombre}</strong> <small>{nombreCategoria(p.categoria)}</small>
                {!p.publicado && <span className="resultado-estado neutro">No publicado</span>}
              </p>
              <div className="local-variantes">
                {[...p.variantes].sort((a, b) => a.color.localeCompare(b.color) || TALLES.indexOf(a.talle) - TALLES.indexOf(b.talle)).map((v) => (
                  <button key={v.varianteId} type="button" onClick={() => alElegir(p, v)}>
                    {v.color} · {v.talle === 'U' ? 'Único' : v.talle}
                  </button>
                ))}
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

export default function PanelVentaLocal() {
  const { sesion, cerrar } = useSesion();
  const token = sesion.token;
  const [lineas, setLineas] = useState([]);
  const [medio, setMedio] = useState('EFECTIVO');
  const [fisico, setFisico] = useState({});
  const [sugerido, setSugerido] = useState(null);
  const [montoTexto, setMontoTexto] = useState('');
  const [montoEditado, setMontoEditado] = useState(false);
  const [enviando, setEnviando] = useState(false);
  const [error, setError] = useState('');
  const [registrada, setRegistrada] = useState(null);

  const fallo = useMemo(() => (e) => {
    if (e instanceof ErrorApi && [401, 403].includes(e.estado)) cerrar();
  }, [cerrar]);

  // Stock físico de lo elegido, para avisar si queda en negativo. Si Inventario no responde, la venta sigue.
  const claveVariantes = lineas.map((l) => l.varianteId).sort().join(',');
  useEffect(() => {
    if (!claveVariantes) return undefined;
    const control = new AbortController();
    consultarStockAdmin(token, claveVariantes.split(','), { signal: control.signal })
      .then(({ items }) => setFisico(Object.fromEntries(items.map((s) => [s.varianteId, s.fisico]))))
      .catch((e) => { if (!control.signal.aborted) { setFisico(null); fallo(e); } });
    return () => control.abort();
  }, [claveVariantes, token, fallo]);

  // Monto sugerido con la promoción del medio de pago (RN-22, HU-17.3).
  const variantesSugerencia = lineasParaSugerencia(lineas);
  useEffect(() => {
    if (!variantesSugerencia) { setSugerido(null); return undefined; }
    const control = new AbortController();
    setSugerido(undefined);
    sugerirMontoLocal(token, medio, variantesSugerencia, { signal: control.signal })
      .then((r) => setSugerido(r.montoSugerido))
      .catch((e) => { if (!control.signal.aborted) { setSugerido(null); fallo(e); } });
    return () => control.abort();
  }, [medio, variantesSugerencia, token, fallo]);

  // Mientras no lo toque, el monto sigue al sugerido.
  useEffect(() => { if (!montoEditado && sugerido != null) setMontoTexto(String(sugerido)); }, [sugerido, montoEditado]);

  const despues = fisicoDespues(lineas, fisico);
  const monto = leerMonto(montoTexto);

  const elegir = (producto, variante) => {
    setRegistrada(null);
    setError('');
    setLineas((actuales) => (actuales.length >= MAXIMO_LINEAS && !actuales.some((l) => l.varianteId === variante.varianteId)
      ? actuales
      : agregar(actuales, { varianteId: variante.varianteId, nombre: producto.nombre, talle: variante.talle, color: variante.color })));
  };

  const registrar = async (e) => {
    e.preventDefault();
    setError('');
    if (monto === null) { setError('Escribí el monto cobrado en pesos, por ejemplo 14000 o 15999,90.'); return; }
    const pedido = { tipo: 'VENTA_LOCAL', medioPago: medio, monto, items: lineas.map(({ varianteId, cantidad }) => ({ varianteId, cantidad })) };
    setEnviando(true);
    try {
      const { tipo, ...cuerpo } = pedido;
      const venta = await registrarVentaLocal(token, cuerpo, claveDelPedido(pedido));
      olvidarClave();
      setRegistrada(venta);
      setLineas([]);
      setMontoTexto('');
      setMontoEditado(false);
      setFisico({});
    } catch (falla) {
      fallo(falla);
      setError(falla.errores?.length
        ? falla.errores.map((c) => `${c.campo}: ${c.mensaje}`).join('. ')
        : 'No pudimos registrar la venta en este momento. Probá de nuevo: no se va a registrar dos veces.');
    } finally {
      setEnviando(false);
    }
  };

  return (
    <div className="local">
      <h1>Venta del local</h1>
      <p className="local-bajada">Registrá lo que se vendió en el local y cuánto se cobró. Descuenta el stock físico (HU-17).</p>

      {registrada && (
        <p className="panel-mensaje local-registrada" role="status">
          Venta registrada por {formatearPrecio(registrada.total)}. <Link to={`/panel/ventas/${registrada.ventaId}`}>Ver la venta</Link>
        </p>
      )}

      <Buscador token={token} alElegir={elegir} alFallar={fallo} />

      <form onSubmit={registrar} className="local-formulario">
        <h2>Prendas vendidas</h2>
        {lineas.length === 0 ? <p className="panel-vacio">Todavía no agregaste prendas. Buscalas arriba.</p> : (
          <ul className="local-lineas">
            {lineas.map((l) => {
              const queda = despues[l.varianteId];
              return (
                <li key={l.varianteId}>
                  <div>
                    <strong>{l.nombre}</strong> <small>{l.color} · Talle {l.talle === 'U' ? 'único' : l.talle}</small>
                    {fisico && queda !== undefined && (
                      <p className={queda < 0 ? 'carrito-aviso' : 'local-fisico'}>
                        {queda < 0
                          ? `El stock físico registrado no alcanza: queda en ${queda} y la prenda pasa a "stock a corregir". La venta se registra igual.`
                          : `Stock físico después de la venta: ${queda}`}
                      </p>
                    )}
                  </div>
                  <div className="carrito-acciones">
                    <div className="cantidad cantidad-chica">
                      <button type="button" aria-label={`Quitar una unidad de ${l.nombre}`} disabled={l.cantidad <= 1} onClick={() => setLineas((a) => cambiarCantidad(a, l.varianteId, l.cantidad - 1))}>−</button>
                      <output aria-label="Cantidad">{l.cantidad}</output>
                      <button type="button" aria-label={`Sumar una unidad de ${l.nombre}`} onClick={() => setLineas((a) => cambiarCantidad(a, l.varianteId, l.cantidad + 1))}>+</button>
                    </div>
                    <button type="button" className="boton-texto" onClick={() => setLineas((a) => quitar(a, l.varianteId))}>Quitar</button>
                  </div>
                </li>
              );
            })}
          </ul>
        )}
        {fisico === null && lineas.length > 0 && <p className="aviso">No pudimos consultar el stock físico. La venta se puede registrar igual.</p>}

        <h2>Cobro</h2>
        <div className="compra-medios local-medios" role="radiogroup" aria-label="Medio de pago">
          {MEDIOS_LOCAL.map(([id, nombre]) => (
            <label key={id} className={`compra-medio${medio === id ? ' elegido' : ''}`}>
              <input type="radio" name="medioPago" value={id} checked={medio === id} onChange={() => setMedio(id)} />
              <span><strong>{nombre}</strong></span>
            </label>
          ))}
        </div>

        <label className="acceso-campo local-monto">
          <span>Monto cobrado</span>
          <input type="text" inputMode="decimal" value={montoTexto} placeholder="$"
            onChange={(e) => { setMontoTexto(e.target.value); setMontoEditado(true); }} />
          <small className="acceso-ayuda">
            {sugerido === undefined ? 'Calculando el monto sugerido…'
              : sugerido != null ? <>Sugerido con la promoción de este medio: <strong>{formatearPrecio(sugerido)}</strong>
                {montoEditado && monto !== sugerido && <> · <button type="button" className="boton-texto" onClick={() => setMontoEditado(false)}>Usar el sugerido</button></>}</>
                : lineas.length > 0 ? 'No pudimos calcular el sugerido: escribí lo que se cobró.' : 'Se sugiere al agregar prendas.'}
          </small>
        </label>

        {error && <p className="acceso-error" role="alert">{error}</p>}
        <button type="submit" className="boton" disabled={enviando || lineas.length === 0 || montoTexto.trim() === ''}>
          {enviando ? 'Registrando…' : 'Registrar la venta'}
        </button>
      </form>
    </div>
  );
}
