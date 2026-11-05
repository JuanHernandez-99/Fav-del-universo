import { useCallback, useEffect, useState } from 'react';
import { Link, useSearchParams } from 'react-router';
import {
  buscarProductosAdmin, consultarStockAdmin, consultarVariantesAdmin, ErrorApi, moverStock, obtenerProductoAdmin, stockACorregir,
} from '../../api.js';
import { claveDelPedido, olvidarClave } from '../../compra/compra.js';
import { nombreCategoria } from '../../dominio.js';
import { useSesion } from '../../sesion/contexto.jsx';
import { cantidadValida, CANALES_STOCK, explicarRechazo, filasDeStock, MOTIVOS, pedidoDe } from './stock.js';

const talleLegible = (t) => (t === 'U' ? 'Único' : t);

function Buscar({ token, alElegir }) {
  const [texto, setTexto] = useState('');
  const [resultados, setResultados] = useState([]);
  useEffect(() => {
    if (texto.trim().length < 2) { setResultados([]); return undefined; }
    const control = new AbortController();
    const t = setTimeout(() => buscarProductosAdmin(token, texto.trim(), { signal: control.signal }).then((r) => setResultados(r.items)).catch(() => {}), 300);
    return () => { clearTimeout(t); control.abort(); };
  }, [texto, token]);
  return (
    <div className="local-buscador">
      <label className="acceso-campo">
        <span>Buscar un producto</span>
        <input type="search" value={texto} onChange={(e) => setTexto(e.target.value)} placeholder="Nombre o descripción" />
      </label>
      {resultados.length > 0 && (
        <ul className="promo-resultados">
          {resultados.map((p) => (
            <li key={p.productoId}><button type="button" onClick={() => { alElegir(p.productoId); setTexto(''); }}>{p.nombre} <small>· {nombreCategoria(p.categoria)}{!p.publicado && ' · sin publicar'}</small></button></li>
          ))}
        </ul>
      )}
    </div>
  );
}

// Un formulario para los tres movimientos. Cada envío lleva su clave de idempotencia: un reintento no se aplica dos veces.
function Movimiento({ token, filas, inicial, alTerminar, fallo }) {
  const [datos, setDatos] = useState({ tipo: 'INGRESO', varianteId: filas[0]?.varianteId ?? '', canal: 'ONLINE', origen: 'FISICO', cantidad: '', sentido: 'RESTAR', motivo: 'CORRECCION_CONTEO', nota: '', ...inicial });
  const [error, setError] = useState('');
  const [enviando, setEnviando] = useState(false);
  useEffect(() => { if (inicial) setDatos((d) => ({ ...d, ...inicial })); }, [inicial]);
  const poner = (campo) => (e) => { setError(''); setDatos((d) => ({ ...d, [campo]: e.target.value })); };
  const fila = filas.find((f) => f.varianteId === datos.varianteId);

  const enviar = async (e) => {
    e.preventDefault();
    if (!cantidadValida(datos.cantidad)) { setError('La cantidad tiene que ser un número entero mayor a 0.'); return; }
    const { ruta, cuerpo } = pedidoDe(datos);
    setEnviando(true);
    setError('');
    try {
      await moverStock(token, ruta, cuerpo, claveDelPedido({ tipo: datos.tipo, items: [], ...cuerpo }));
      olvidarClave();
      setDatos((d) => ({ ...d, cantidad: '', nota: '' }));
      alTerminar(`Listo: ${datos.tipo === 'INGRESO' ? 'ingreso' : datos.tipo === 'TRANSFERENCIA' ? 'transferencia' : 'ajuste'} registrado en ${fila ? `${fila.color} · ${talleLegible(fila.talle)}` : 'la variante'}.`);
    } catch (falla) {
      if (!fallo(falla)) setError(explicarRechazo(falla, datos.tipo));
    } finally {
      setEnviando(false);
    }
  };

  return (
    <form className="stock-movimiento" onSubmit={enviar} noValidate>
      <h2>Registrar un movimiento</h2>
      <div className="panel-filtros" role="group" aria-label="Tipo de movimiento">
        {[['INGRESO', 'Ingreso'], ['TRANSFERENCIA', 'Transferencia'], ['AJUSTE', 'Ajuste']].map(([id, nombre]) => (
          <button key={id} type="button" aria-pressed={datos.tipo === id} onClick={() => { setError(''); setDatos((d) => ({ ...d, tipo: id })); }}>{nombre}</button>
        ))}
      </div>
      <p className="local-fisico">
        {datos.tipo === 'INGRESO' && 'Mercadería nueva que entra a un canal (HU-06).'}
        {datos.tipo === 'TRANSFERENCIA' && 'Mueve unidades entre la web y el local, en una sola operación (HU-07). Del online solo se mueve lo que no está reservado.'}
        {datos.tipo === 'AJUSTE' && 'Corrige el stock por una pérdida, una falla o un error de conteo (HU-08). Para corregir un físico negativo, sumá.'}
      </p>
      <div className="stock-campos">
        <label className="acceso-campo">
          <span>Variante</span>
          <select value={datos.varianteId} onChange={poner('varianteId')}>
            {filas.map((f) => <option key={f.varianteId} value={f.varianteId}>{f.color} · {talleLegible(f.talle)}</option>)}
          </select>
        </label>
        {datos.tipo === 'TRANSFERENCIA' ? (
          <label className="acceso-campo">
            <span>Dirección</span>
            <select value={datos.origen} onChange={poner('origen')}>
              <option value="FISICO">Del local a la web</option>
              <option value="ONLINE">De la web al local</option>
            </select>
          </label>
        ) : (
          <label className="acceso-campo">
            <span>Canal</span>
            <select value={datos.canal} onChange={poner('canal')}>{CANALES_STOCK.map(([id, n]) => <option key={id} value={id}>{n}</option>)}</select>
          </label>
        )}
        {datos.tipo === 'AJUSTE' && (
          <label className="acceso-campo">
            <span>Operación</span>
            <select value={datos.sentido} onChange={poner('sentido')}><option value="RESTAR">Restar</option><option value="SUMAR">Sumar</option></select>
          </label>
        )}
        <label className="acceso-campo">
          <span>Cantidad</span>
          <input inputMode="numeric" value={datos.cantidad} onChange={poner('cantidad')} placeholder="Unidades" />
        </label>
        {datos.tipo === 'AJUSTE' && (
          <>
            <label className="acceso-campo">
              <span>Motivo</span>
              <select value={datos.motivo} onChange={poner('motivo')}>{MOTIVOS.map(([id, n]) => <option key={id} value={id}>{n}</option>)}</select>
            </label>
            <label className="acceso-campo stock-nota">
              <span>Nota (opcional)</span>
              <input value={datos.nota} maxLength={200} onChange={poner('nota')} placeholder="Por ejemplo, prenda manchada" />
            </label>
          </>
        )}
      </div>
      {error && <p className="acceso-error" role="alert">{error}</p>}
      <button type="submit" className="boton" disabled={enviando || filas.length === 0}>{enviando ? 'Registrando…' : 'Registrar'}</button>
    </form>
  );
}

function PorProducto({ token, productoId, varianteInicial, fallo, alElegir }) {
  const [producto, setProducto] = useState(null);
  const [filas, setFilas] = useState(null);
  const [error, setError] = useState('');
  const [mensaje, setMensaje] = useState('');

  const cargar = useCallback(async () => {
    setError('');
    try {
      const p = await obtenerProductoAdmin(productoId, token);
      const { items } = await consultarStockAdmin(token, p.variantes.map((v) => v.varianteId));
      setProducto(p);
      setFilas(filasDeStock(p.variantes, items));
    } catch (e) {
      if (!fallo(e)) setError(e instanceof ErrorApi && e.estado === 404 ? 'No existe ese producto.' : 'No pudimos cargar el stock. Si Inventario o Catálogo no responden, probá en un rato.');
    }
  }, [productoId, token, fallo]);

  useEffect(() => { setProducto(null); setFilas(null); setMensaje(''); cargar(); }, [cargar]);

  const inicial = varianteInicial ? { varianteId: varianteInicial, tipo: 'AJUSTE', canal: 'FISICO', sentido: 'SUMAR' } : null;

  return (
    <>
      <Buscar token={token} alElegir={alElegir} />
      {error && <p className="aviso" role="alert">{error} <button type="button" className="boton-texto" onClick={cargar}>Reintentar</button></p>}
      {!producto && !error && <div className="mis-compras-esqueleto" aria-busy="true" aria-label="Cargando el stock" />}
      {producto && filas && (
        <>
          <h2 className="stock-producto">{producto.nombre} <Link to={`/panel/productos/${producto.productoId}`} className="boton-texto">Ver producto</Link></h2>
          <div className="panel-tabla" role="table" aria-label="Stock por variante">
            <div className="panel-fila stock-fila panel-fila-titulos" role="row">
              {['Variante', 'Online', 'Reservado', 'Disponible online', 'Físico'].map((t) => <span key={t} role="columnheader">{t}</span>)}
            </div>
            {filas.map((f) => (
              <div key={f.varianteId} className={`panel-fila stock-fila${f.varianteId === varianteInicial ? ' resaltada' : ''}`} role="row">
                <span role="cell"><strong>{f.color} · {talleLegible(f.talle)}</strong>{!f.registrada && <small className="stock-sin-registrar"> sin ingresos</small>}</span>
                <span role="cell" data-titulo="Online">{f.online}</span>
                <span role="cell" data-titulo="Reservado">{f.reservado}</span>
                <span role="cell" data-titulo="Disponible">{f.disponibleOnline}</span>
                <span role="cell" data-titulo="Físico" className={f.stockACorregir ? 'stock-negativo' : ''}>{f.fisico}{f.stockACorregir && <span className="resultado-estado mal">A corregir</span>}</span>
              </div>
            ))}
          </div>
          {mensaje && <p className="panel-mensaje local-registrada" role="status">{mensaje}</p>}
          <Movimiento token={token} filas={filas} inicial={inicial} fallo={fallo} alTerminar={async (m) => { await cargar(); setMensaje(m); }} />
        </>
      )}
    </>
  );
}

// HU-09.2: Inventario dice qué variantes tienen el físico negativo; Catálogo, de qué producto son.
function ACorregir({ token, fallo, alCorregir }) {
  const [filas, setFilas] = useState(null);
  const [hayMas, setHayMas] = useState(false);
  const [error, setError] = useState(false);

  const cargar = useCallback(async () => {
    setError(false);
    try {
      const { items, hayMas: mas } = await stockACorregir(token);
      const ids = items.map((s) => s.varianteId);
      let detalles = [];
      try {
        for (let i = 0; i < ids.length; i += 200) detalles.push(...(await consultarVariantesAdmin(token, ids.slice(i, i + 200))).items);
      } catch (e) {
        // Catálogo rechaza el lote entero si un id no existe: se reintenta sin esos, que se muestran sin nombre.
        if (!(e instanceof ErrorApi && e.codigo === 'VARIANTE_INEXISTENTE')) throw e;
        const conocidas = ids.filter((id) => !e.variantes.includes(id));
        detalles = conocidas.length ? (await consultarVariantesAdmin(token, conocidas)).items : [];
      }
      const porId = Object.fromEntries(detalles.map((d) => [d.varianteId, d]));
      setFilas(items.map((s) => ({ ...s, detalle: porId[s.varianteId] })));
      setHayMas(mas);
    } catch (e) {
      if (!fallo(e)) setError(true);
    }
  }, [token, fallo]);

  useEffect(() => { cargar(); }, [cargar]);

  if (error) return <p className="aviso" role="alert">No pudimos cargar la lista. <button type="button" className="boton-texto" onClick={cargar}>Reintentar</button></p>;
  if (!filas) return <div className="mis-compras-esqueleto" aria-busy="true" aria-label="Cargando" />;
  if (filas.length === 0) return <p className="panel-vacio">No hay variantes con el stock físico negativo.</p>;
  return (
    <>
      <p className="local-bajada">Una venta del local registrada sin stock físico deja la variante en negativo (RN-25). Se corrige con un ajuste que sume lo que realmente hay.</p>
      {hayMas && <p className="aviso">Se muestran las primeras 500.</p>}
      <div className="panel-tabla" role="table" aria-label="Stock a corregir">
        <div className="panel-fila acorregir-fila panel-fila-titulos" role="row">
          {['Producto', 'Variante', 'Físico', ''].map((t, i) => <span key={i} role="columnheader">{t}</span>)}
        </div>
        {filas.map((f) => (
          <div key={f.varianteId} className="panel-fila acorregir-fila" role="row">
            <span role="cell">{f.detalle ? <strong>{f.detalle.nombre}</strong> : <small>{f.varianteId} (no está en el catálogo)</small>}</span>
            <span role="cell">{f.detalle ? `${f.detalle.color} · ${talleLegible(f.detalle.talle)}` : '—'}</span>
            <span role="cell" className="stock-negativo">{f.fisico}</span>
            <span role="cell">{f.detalle && <button type="button" className="boton-texto" onClick={() => alCorregir(f.detalle.productoId, f.varianteId)}>Corregir</button>}</span>
          </div>
        ))}
      </div>
    </>
  );
}

export default function PanelStock() {
  const { sesion, cerrar } = useSesion();
  const [parametros, setParametros] = useSearchParams();
  const vista = parametros.get('vista') === 'a-corregir' ? 'a-corregir' : 'producto';
  const productoId = parametros.get('producto');

  const fallo = useCallback((e) => {
    if (e instanceof ErrorApi && [401, 403].includes(e.estado)) { cerrar(); return true; }
    return false;
  }, [cerrar]);

  return (
    <div className="stock">
      <h1 className="promo-titulo">Stock</h1>
      <nav className="panel-filtros" aria-label="Vista">
        <button type="button" aria-pressed={vista === 'producto'} onClick={() => setParametros(productoId ? { producto: productoId } : {})}>Por producto</button>
        <button type="button" aria-pressed={vista === 'a-corregir'} onClick={() => setParametros({ vista: 'a-corregir' })}>Stock a corregir</button>
      </nav>
      {vista === 'a-corregir'
        ? <ACorregir token={sesion.token} fallo={fallo} alCorregir={(p, v) => setParametros({ producto: p, variante: v })} />
        : productoId
          ? <PorProducto key={productoId} token={sesion.token} productoId={productoId} varianteInicial={parametros.get('variante')} fallo={fallo} alElegir={(p) => setParametros({ producto: p })} />
          : <><Buscar token={sesion.token} alElegir={(p) => setParametros({ producto: p })} /><p className="panel-vacio">Buscá un producto para ver y mover su stock.</p></>}
    </div>
  );
}
