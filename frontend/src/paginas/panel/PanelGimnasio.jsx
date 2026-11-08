import { useCallback, useEffect, useState } from 'react';
import { consultarStockGimnasio, detallesDeVariantes, ErrorApi, listarReposiciones, moverStock, obtenerProductoAdmin } from '../../api.js';
import { claveDelPedido, olvidarClave } from '../../compra/compra.js';
import { useSesion } from '../../sesion/contexto.jsx';
import { cantidadValida } from './stock.js';
import { Buscar } from './PanelStock.jsx';
import { conDetalle, estadoReposicion, explicarRechazoReposicion } from './gimnasio.js';

const talleLegible = (t) => (t === 'U' ? 'Único' : t);
const fecha = (iso) => new Date(iso).toLocaleString('es-AR', { dateStyle: 'short', timeStyle: 'short' });

// Sin Catálogo se muestran los ids: el dato del gimnasio sigue siendo útil.
async function agregarDetalle(token, filas) {
  try {
    return conDetalle(filas, await detallesDeVariantes(token, [...new Set(filas.map((f) => f.varianteId))]));
  } catch {
    return conDetalle(filas, []);
  }
}

function NombreVariante({ fila }) {
  if (!fila.detalle) return <small>{fila.varianteId}</small>;
  return <><strong>{fila.detalle.nombre}</strong> <small>· {fila.detalle.color} · {talleLegible(fila.detalle.talle)}</small></>;
}

// Un error nunca se muestra como stock en cero ni como una lista vacía.
function StockEnGimnasio({ token, version, fallo }) {
  const [filas, setFilas] = useState(null);
  const [noDisponible, setNoDisponible] = useState(false);

  const cargar = useCallback(async () => {
    setNoDisponible(false);
    try {
      const { items } = await consultarStockGimnasio(token);
      setFilas(await agregarDetalle(token, items));
    } catch (e) {
      if (!fallo(e)) { setFilas(null); setNoDisponible(true); }
    }
  }, [token, fallo]);

  useEffect(() => { cargar(); }, [cargar, version]);

  return (
    <section className="gimnasio-seccion">
      <h2>Stock en el gimnasio</h2>
      {noDisponible && <p className="aviso" role="alert">Stock del gimnasio no disponible en este momento. <button type="button" className="boton-texto" onClick={cargar}>Reintentar</button></p>}
      {!noDisponible && !filas && <div className="mis-compras-esqueleto" aria-busy="true" aria-label="Cargando el stock del gimnasio" />}
      {filas?.length === 0 && <p className="panel-vacio">El gimnasio todavía no recibió ninguna variante.</p>}
      {filas?.length > 0 && (
        <div className="panel-tabla" role="table" aria-label="Stock en el gimnasio">
          <div className="panel-fila gimnasio-stock-fila panel-fila-titulos" role="row">
            {['Variante', 'Disponibles', 'Vendidas'].map((t) => <span key={t} role="columnheader">{t}</span>)}
          </div>
          {filas.map((f) => (
            <div key={f.varianteId} className="panel-fila gimnasio-stock-fila" role="row">
              <span role="cell"><NombreVariante fila={f} /></span>
              <span role="cell" data-titulo="Disponibles">{f.cantidad}</span>
              <span role="cell" data-titulo="Vendidas">{f.vendidas}</span>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

function NuevaReposicion({ token, fallo, alEnviar }) {
  const [productoId, setProductoId] = useState(null);
  const [producto, setProducto] = useState(null);
  const [varianteId, setVarianteId] = useState('');
  const [cantidad, setCantidad] = useState('');
  const [error, setError] = useState('');
  const [enviando, setEnviando] = useState(false);

  useEffect(() => {
    if (!productoId) return undefined;
    let vigente = true;
    setProducto(null);
    setError('');
    obtenerProductoAdmin(productoId, token)
      .then((p) => { if (vigente) { setProducto(p); setVarianteId(p.variantes[0]?.varianteId ?? ''); } })
      .catch((e) => { if (vigente && !fallo(e)) setError('No pudimos cargar el producto. Probá en un rato.'); });
    return () => { vigente = false; };
  }, [productoId, token, fallo]);

  const enviar = async (e) => {
    e.preventDefault();
    if (!varianteId) { setError('Elegí una variante.'); return; }
    if (!cantidadValida(cantidad)) { setError('La cantidad tiene que ser un número entero mayor a 0.'); return; }
    const cuerpo = { varianteId, cantidad: Number(cantidad) };
    setEnviando(true);
    setError('');
    try {
      await moverStock(token, 'reposiciones', cuerpo, claveDelPedido({ tipo: 'REPOSICION', items: [], ...cuerpo }));
      olvidarClave();
      setCantidad('');
      alEnviar();
    } catch (falla) {
      if (!fallo(falla)) setError(explicarRechazoReposicion(falla));
    } finally {
      setEnviando(false);
    }
  };

  return (
    <section className="gimnasio-seccion">
      <h2>Enviar una reposición</h2>
      <p className="local-fisico">Descuenta del stock físico ahora y se le informa al gimnasio en segundo plano: si no responde, se reintenta sola.</p>
      <Buscar token={token} alElegir={setProductoId} />
      <form className="stock-movimiento" onSubmit={enviar} noValidate>
        {producto && <p><strong>{producto.nombre}</strong></p>}
        <div className="stock-campos">
          <label className="acceso-campo">
            <span>Variante</span>
            <select value={varianteId} onChange={(ev) => setVarianteId(ev.target.value)} disabled={!producto}>
              {!producto && <option value="">Buscá un producto</option>}
              {producto?.variantes.map((v) => <option key={v.varianteId} value={v.varianteId}>{v.color} · {talleLegible(v.talle)}</option>)}
            </select>
          </label>
          <label className="acceso-campo">
            <span>Cantidad</span>
            <input inputMode="numeric" value={cantidad} onChange={(ev) => { setError(''); setCantidad(ev.target.value); }} placeholder="Unidades" />
          </label>
        </div>
        {error && <p className="acceso-error" role="alert">{error}</p>}
        <button type="submit" className="boton" disabled={enviando || !producto}>{enviando ? 'Enviando…' : 'Enviar al gimnasio'}</button>
      </form>
    </section>
  );
}

function Reposiciones({ token, version, fallo }) {
  const [filas, setFilas] = useState(null);
  const [error, setError] = useState(false);

  const cargar = useCallback(async () => {
    setError(false);
    try {
      const { items } = await listarReposiciones(token);
      setFilas(await agregarDetalle(token, items));
    } catch (e) {
      if (!fallo(e)) setError(true);
    }
  }, [token, fallo]);

  useEffect(() => { cargar(); }, [cargar, version]);

  return (
    <section className="gimnasio-seccion">
      <h2>Reposiciones <button type="button" className="boton-texto" onClick={cargar}>Actualizar</button></h2>
      {error && <p className="aviso" role="alert">No pudimos cargar las reposiciones. <button type="button" className="boton-texto" onClick={cargar}>Reintentar</button></p>}
      {!error && !filas && <div className="mis-compras-esqueleto" aria-busy="true" aria-label="Cargando las reposiciones" />}
      {!error && filas?.length === 0 && <p className="panel-vacio">Todavía no se envió ninguna reposición.</p>}
      {!error && filas?.length > 0 && (
        <div className="panel-tabla" role="table" aria-label="Reposiciones al gimnasio">
          <div className="panel-fila gimnasio-rep-fila panel-fila-titulos" role="row">
            {['Variante', 'Cantidad', 'Fecha', 'Estado'].map((t) => <span key={t} role="columnheader">{t}</span>)}
          </div>
          {filas.map((f) => {
            const estado = estadoReposicion(f.estado);
            return (
              <div key={f.reposicionId} className="panel-fila gimnasio-rep-fila" role="row">
                <span role="cell"><NombreVariante fila={f} /></span>
                <span role="cell" data-titulo="Cantidad">{f.cantidad}</span>
                <span role="cell" data-titulo="Fecha">{fecha(f.creadaEn)}</span>
                <span role="cell" data-titulo="Estado">
                  <span className={`resultado-estado ${estado.tono}`} title={estado.ayuda}>{estado.nombre}</span>
                  {f.estado !== 'CONFIRMADA' && estado.ayuda && <small className="gimnasio-ayuda">{estado.ayuda}</small>}
                </span>
              </div>
            );
          })}
        </div>
      )}
    </section>
  );
}

export default function PanelGimnasio() {
  const { sesion, cerrar } = useSesion();
  const [version, setVersion] = useState(0);
  const [mensaje, setMensaje] = useState('');

  const fallo = useCallback((e) => {
    if (e instanceof ErrorApi && [401, 403].includes(e.estado)) { cerrar(); return true; }
    return false;
  }, [cerrar]);

  return (
    <div className="stock">
      <h1 className="promo-titulo">Gimnasio</h1>
      <p className="local-bajada">Ropa de FAV en consignación en la tienda del gimnasio. Si el gimnasio no responde, el resto de la tienda sigue funcionando.</p>
      <StockEnGimnasio token={sesion.token} version={version} fallo={fallo} />
      {mensaje && <p className="panel-mensaje local-registrada" role="status">{mensaje}</p>}
      <NuevaReposicion token={sesion.token} fallo={fallo} alEnviar={() => { setMensaje('Reposición registrada. Las unidades ya salieron del stock físico.'); setVersion((v) => v + 1); }} />
      <Reposiciones token={sesion.token} version={version} fallo={fallo} />
    </div>
  );
}
