import { Fragment, useCallback, useEffect, useState } from 'react';
import { Link, useSearchParams } from 'react-router';
import {
  buscarProductosAdmin, consultarStockAdmin, consultarStockEnTandas, detallesDeVariantes, ErrorApi, listarProductosAdmin,
  moverStock, obtenerProductoAdmin, stockACorregir,
} from '../../api.js';
import { claveDelPedido, olvidarClave } from '../../compra/compra.js';
import { nombreCategoria } from '../../dominio.js';
import { useSesion } from '../../sesion/contexto.jsx';
import {
  cantidadValida, CANALES_STOCK, columnasDe, explicarRechazo, filasDeStock, MOTIVOS, ordenarPorStock, pedidoDe, siguienteOrden,
  totalesDe, VISTAS_CANTIDADES,
} from './stock.js';

const talleLegible = (t) => (t === 'U' ? 'Único' : t);

export function Buscar({ token, alElegir }) {
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

function PorProducto({ token, productoId, varianteInicial, fallo, alVolver }) {
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
      <button type="button" className="boton-texto stock-volver" onClick={alVolver}>← Volver a la lista</button>
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

const TAMANO = 20;
const FILTROS_PUBLICADO = [['', 'Todos'], ['true', 'Publicados'], ['false', 'Sin publicar']];

// Una celda de cantidad. El físico negativo se marca para corregir (RN-25).
function Cantidad({ campo, titulo, fila }) {
  const marcar = campo === 'fisico' && fila.stockACorregir;
  return (
    <span role="cell" data-titulo={titulo} className={marcar ? 'stock-negativo' : ''}>
      {fila[campo]}{marcar && <span className="resultado-estado mal">A corregir</span>}
    </span>
  );
}

// Todos los productos con su stock: Catálogo da la página de productos y sus variantes; Inventario, el stock de esas variantes.
function Listado({ token, fallo, alMover }) {
  const [parametros, setParametros] = useSearchParams();
  const q = parametros.get('q') ?? '';
  const publicado = parametros.get('publicado') ?? '';
  const vista = parametros.get('cantidades') ?? 'todo';
  const pagina = Number(parametros.get('pagina') ?? 1);
  const columnas = columnasDe(vista);
  const campoOrden = parametros.get('orden');
  const orden = columnas.some(([campo]) => campo === campoOrden) ? { campo: campoOrden, sentido: parametros.get('sentido') === 'asc' ? 'asc' : 'desc' } : null;
  const [texto, setTexto] = useState(q);
  const [datos, setDatos] = useState(null);
  const [error, setError] = useState(false);
  const [reintento, setReintento] = useState(0);
  const [abiertos, setAbiertos] = useState(() => new Set());

  // Sin orden, Catálogo pagina y se pide el stock de esa página. Con orden, Catálogo no puede ordenar por stock (no lo
  // conoce): se traen todos los productos de los filtros y el orden y la paginación se hacen acá. Cambiar de columna,
  // de sentido o de página no vuelve a consultar.
  const paginaDelServidor = orden ? 'todas' : pagina;
  useEffect(() => {
    const control = new AbortController();
    const opciones = { signal: control.signal };
    setDatos(null);
    setError(false);
    (async () => {
      try {
        let productos;
        let r;
        if (paginaDelServidor === 'todas') {
          r = await listarProductosAdmin(token, { q, publicado, pagina: 1, tamano: 100 }, opciones);
          const resto = await Promise.all(Array.from({ length: Math.max(0, r.paginas - 1) }, (_, i) => listarProductosAdmin(token, { q, publicado, pagina: i + 2, tamano: 100 }, opciones)));
          productos = [r, ...resto].flatMap((p) => p.items);
        } else {
          r = await listarProductosAdmin(token, { q, publicado, pagina: paginaDelServidor, tamano: TAMANO }, opciones);
          productos = r.items;
        }
        const ids = productos.flatMap((p) => p.variantes.map((v) => v.varianteId));
        const stocks = ids.length ? await consultarStockEnTandas(token, ids, opciones) : [];
        if (control.signal.aborted) return;
        const items = productos.map((p) => {
          const filas = filasDeStock(p.variantes, stocks);
          return { ...p, filas, total: totalesDe(filas) };
        });
        setDatos({ items, total: r.total, paginas: paginaDelServidor === 'todas' ? Math.ceil(items.length / TAMANO) : r.paginas, completo: paginaDelServidor === 'todas' });
      } catch (e) {
        if (!control.signal.aborted && !fallo(e)) setError(true);
      }
    })();
    return () => control.abort();
  }, [token, q, publicado, paginaDelServidor, fallo, reintento]);

  const resultado = datos && (datos.completo && orden
    ? { ...datos, items: ordenarPorStock(datos.items, orden).slice((pagina - 1) * TAMANO, pagina * TAMANO) }
    : datos);

  const cambiar = (nombre, valor) => {
    const nuevos = new URLSearchParams(parametros);
    if (valor) nuevos.set(nombre, valor); else nuevos.delete(nombre);
    if (nombre !== 'pagina' && nombre !== 'cantidades') nuevos.delete('pagina');
    setParametros(nuevos);
  };
  const ordenarPor = (campo) => {
    const siguiente = siguienteOrden(orden, campo);
    const nuevos = new URLSearchParams(parametros);
    nuevos.delete('pagina');
    if (siguiente) { nuevos.set('orden', siguiente.campo); nuevos.set('sentido', siguiente.sentido); } else { nuevos.delete('orden'); nuevos.delete('sentido'); }
    setParametros(nuevos);
  };
  const alternar = (id) => setAbiertos((a) => {
    const nuevos = new Set(a);
    if (!nuevos.delete(id)) nuevos.add(id);
    return nuevos;
  });
  const ancho = { '--columnas': columnas.length };

  return (
    <>
      <div className="panel-buscar">
        <form role="search" onSubmit={(e) => { e.preventDefault(); cambiar('q', texto.trim()); }}>
          <input type="search" value={texto} onChange={(e) => setTexto(e.target.value)} placeholder="Buscar por nombre o descripción" aria-label="Buscar un producto en el stock" />
        </form>
      </div>
      <nav className="panel-filtros" aria-label="Publicación">
        {FILTROS_PUBLICADO.map(([id, nombre]) => (
          <button key={id} type="button" aria-pressed={publicado === id} onClick={() => cambiar('publicado', id)}>{nombre}</button>
        ))}
      </nav>
      <nav className="panel-filtros" aria-label="Cantidades a mostrar">
        {VISTAS_CANTIDADES.map(([id, nombre]) => (
          <button key={id} type="button" aria-pressed={vista === id} onClick={() => cambiar('cantidades', id === 'todo' ? '' : id)}>{nombre}</button>
        ))}
      </nav>

      {error && <p className="aviso" role="alert">No pudimos cargar el stock. Si Inventario o Catálogo no responden, probá en un rato. <button type="button" className="boton-texto" onClick={() => setReintento((n) => n + 1)}>Reintentar</button></p>}
      {!resultado && !error && <div className="mis-compras-esqueleto" aria-busy="true" aria-label="Cargando el stock" />}
      {resultado?.items.length === 0 && <p className="panel-vacio">No hay productos con esos filtros.</p>}

      {resultado?.items.length > 0 && (
        <>
          <p className="resultado-total">{resultado.total} {resultado.total === 1 ? 'producto' : 'productos'} · tocá uno para ver sus variantes</p>
          <div className="panel-tabla" role="table" aria-label="Stock por producto">
            <div className="panel-fila stock-fila panel-fila-titulos" role="row" style={ancho}>
              <span role="columnheader">Producto</span>
              {columnas.map(([campo, titulo]) => {
                const sentido = orden?.campo === campo ? orden.sentido : null;
                return (
                  <span key={campo} role="columnheader" aria-sort={sentido ? (sentido === 'desc' ? 'descending' : 'ascending') : 'none'}>
                    <button type="button" className="stock-ordenar" onClick={() => ordenarPor(campo)} title="Ordenar por esta columna">
                      {titulo} <span aria-hidden="true">{sentido === 'desc' ? '↓' : sentido === 'asc' ? '↑' : '↕'}</span>
                    </button>
                  </span>
                );
              })}
            </div>
            {resultado.items.map((p) => {
              const abierto = abiertos.has(p.productoId);
              const { total } = p;
              return (
                <Fragment key={p.productoId}>
                  <div className="panel-fila stock-fila stock-fila-producto" role="row" style={ancho}>
                    <span role="cell">
                      <button type="button" className="stock-desplegar" aria-expanded={abierto} onClick={() => alternar(p.productoId)}><strong>{p.nombre}</strong></button>
                      <small> · {nombreCategoria(p.categoria)} · {p.variantes.length} {p.variantes.length === 1 ? 'variante' : 'variantes'}</small>
                      {!p.publicado && <span className="resultado-estado neutro">Sin publicar</span>}
                    </span>
                    {columnas.map(([campo, titulo]) => <Cantidad key={campo} campo={campo} titulo={titulo} fila={total} />)}
                  </div>
                  {abierto && (
                    <>
                      {p.filas.length === 0 && <div className="stock-acciones" role="row"><span role="cell">Este producto no tiene variantes.</span></div>}
                      {p.filas.map((f) => (
                        <div key={f.varianteId} className="panel-fila stock-fila stock-variante" role="row" style={ancho}>
                          <span role="cell">{f.color} · {talleLegible(f.talle)}{!f.registrada && <small className="stock-sin-registrar"> sin ingresos</small>}</span>
                          {columnas.map(([campo, titulo]) => <Cantidad key={campo} campo={campo} titulo={titulo} fila={f} />)}
                        </div>
                      ))}
                      <div className="stock-acciones" role="row">
                        <span role="cell"><button type="button" className="boton-texto" onClick={() => alMover(p.productoId)}>Mover stock de {p.nombre}</button></span>
                      </div>
                    </>
                  )}
                </Fragment>
              );
            })}
          </div>
          {resultado.paginas > 1 && (
            <nav className="paginas" aria-label="Páginas">
              <button type="button" disabled={pagina <= 1} onClick={() => cambiar('pagina', String(pagina - 1))} aria-label="Página anterior">←</button>
              <span className="paginas-grupo">{pagina} de {resultado.paginas}</span>
              <button type="button" disabled={pagina >= resultado.paginas} onClick={() => cambiar('pagina', String(pagina + 1))} aria-label="Página siguiente">→</button>
            </nav>
          )}
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
      const detalles = await detallesDeVariantes(token, items.map((s) => s.varianteId));
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

  // Los filtros de la lista quedan en la URL mientras se mueve el stock de un producto: al volver, la lista sigue igual.
  const sinProducto = () => {
    const nuevos = new URLSearchParams(parametros);
    nuevos.delete('producto');
    nuevos.delete('variante');
    nuevos.delete('vista');
    return nuevos;
  };
  const mover = (p) => { const nuevos = sinProducto(); nuevos.set('producto', p); setParametros(nuevos); };

  return (
    <div className="stock">
      <h1 className="promo-titulo">Stock</h1>
      <nav className="panel-filtros" aria-label="Vista">
        <button type="button" aria-pressed={vista === 'producto'} onClick={() => setParametros(sinProducto())}>Todos los productos</button>
        <button type="button" aria-pressed={vista === 'a-corregir'} onClick={() => setParametros({ vista: 'a-corregir' })}>Stock a corregir</button>
      </nav>
      {vista === 'a-corregir'
        ? <ACorregir token={sesion.token} fallo={fallo} alCorregir={(p, v) => setParametros({ producto: p, variante: v })} />
        : productoId
          ? <PorProducto key={productoId} token={sesion.token} productoId={productoId} varianteInicial={parametros.get('variante')} fallo={fallo} alVolver={() => setParametros(sinProducto())} />
          : <Listado token={sesion.token} fallo={fallo} alMover={mover} />}
    </div>
  );
}
