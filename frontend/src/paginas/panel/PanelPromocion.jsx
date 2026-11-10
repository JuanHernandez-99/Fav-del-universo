import { useCallback, useEffect, useState } from 'react';
import { Link, useNavigate, useParams } from 'react-router';
import { actualizarPromocion, buscarProductosAdmin, crearPromocion, ErrorApi, listarPromociones, obtenerProductoAdmin } from '../../api.js';
import { CATEGORIAS } from '../../dominio.js';
import { useSesion } from '../../sesion/contexto.jsx';
import { erroresPorCampo } from './productos.js';
import { aCampoFecha, cuerpoDe } from './promociones.js';
import { MEDIOS_LOCAL } from './ventaLocal.js';

const vacia = { nombre: '', tipo: 'PORCENTAJE', porcentaje: '', medioPago: 'EFECTIVO', desde: '', hasta: '', alcanceTipo: 'TIENDA', categorias: [], productos: [] };
const conMayuscula = (t) => (t ? t[0].toUpperCase() + t.slice(1) : t);

function MensajeError({ texto }) {
  return texto ? <small className="campo-error" role="alert">{conMayuscula(texto)}</small> : null;
}

// Elegir productos para el alcance: se buscan en el catálogo de administración, publicados o no.
function ElegirProductos({ token, elegidos, setElegidos }) {
  const [texto, setTexto] = useState('');
  const [resultados, setResultados] = useState([]);
  useEffect(() => {
    if (texto.trim().length < 2) { setResultados([]); return undefined; }
    const control = new AbortController();
    const t = setTimeout(() => buscarProductosAdmin(token, texto.trim(), { signal: control.signal }).then((r) => setResultados(r.items)).catch(() => {}), 300);
    return () => { clearTimeout(t); control.abort(); };
  }, [texto, token]);
  const ids = new Set(elegidos.map((p) => p.productoId));
  return (
    <div className="promo-productos">
      {elegidos.length > 0 && (
        <ul className="producto-chips">
          {elegidos.map((p) => (
            <li key={p.productoId}>{p.nombre}<button type="button" aria-label={`Quitar ${p.nombre}`} onClick={() => setElegidos((a) => a.filter((x) => x.productoId !== p.productoId))}>×</button></li>
          ))}
        </ul>
      )}
      <input type="search" value={texto} onChange={(e) => setTexto(e.target.value)} placeholder="Buscar un producto para agregarlo" aria-label="Buscar un producto para la promoción" />
      {resultados.length > 0 && (
        <ul className="promo-resultados">
          {resultados.map((p) => (
            <li key={p.productoId}>
              <button type="button" disabled={ids.has(p.productoId)} onClick={() => { setElegidos((a) => [...a, { productoId: p.productoId, nombre: p.nombre }]); setTexto(''); }}>
                {p.nombre} {ids.has(p.productoId) && '· agregado'}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

export default function PanelPromocion() {
  const { id } = useParams();
  const esNueva = !id;
  const { sesion, cerrar } = useSesion();
  const token = sesion.token;
  const navegar = useNavigate();
  const [datos, setDatos] = useState(esNueva ? vacia : null);
  const [errores, setErrores] = useState({});
  const [aviso, setAviso] = useState('');
  const [guardando, setGuardando] = useState(false);

  const fallo = useCallback((e) => {
    if (e instanceof ErrorApi && [401, 403].includes(e.estado)) { cerrar(); return true; }
    return false;
  }, [cerrar]);

  // Catálogo no tiene una ruta para una sola promoción: se busca en el listado.
  useEffect(() => {
    if (esNueva) return;
    (async () => {
      try {
        const p = (await listarPromociones(token)).items.find((x) => x.promocionId === id);
        if (!p) { setAviso('No existe esa promoción.'); return; }
        const productos = p.alcance.tipo === 'PRODUCTO'
          ? await Promise.all(p.alcance.ids.map((pid) => obtenerProductoAdmin(pid, token).then((x) => ({ productoId: pid, nombre: x.nombre })).catch(() => ({ productoId: pid, nombre: pid }))))
          : [];
        setDatos({
          nombre: p.nombre, tipo: p.tipo, porcentaje: String(p.porcentaje), medioPago: p.medioPago ?? 'EFECTIVO',
          desde: aCampoFecha(p.vigencia.desde), hasta: aCampoFecha(p.vigencia.hasta),
          alcanceTipo: p.alcance.tipo, categorias: p.alcance.tipo === 'CATEGORIA' ? p.alcance.ids : [], productos,
        });
      } catch (e) {
        if (!fallo(e)) setAviso('No pudimos cargar la promoción. Recargá la página.');
      }
    })();
  }, [esNueva, id, token, fallo]);

  if (!datos) return aviso ? <p className="panel-vacio">{aviso} <Link to="/panel/promociones">Volver a promociones</Link></p> : <div className="mis-compras-esqueleto" aria-busy="true" />;

  // Al tocar un campo se borra su error: ya no corresponde a lo que está escrito.
  const ERRORES_DE = { desde: ['vigencia.desde'], hasta: ['vigencia.hasta'], alcanceTipo: ['alcance.ids', 'alcance.tipo'], categorias: ['alcance.ids'], productos: ['alcance.ids'] };
  const poner = (campo, valor) => {
    setDatos((d) => ({ ...d, [campo]: valor }));
    setErrores((e) => { const resto = { ...e }; for (const c of ERRORES_DE[campo] ?? [campo]) delete resto[c]; return resto; });
  };

  const guardar = async (e) => {
    e.preventDefault();
    setErrores({});
    setAviso('');
    setGuardando(true);
    try {
      const cuerpo = cuerpoDe(datos);
      if (esNueva) await crearPromocion(token, cuerpo); else await actualizarPromocion(token, id, cuerpo);
      navegar('/panel/promociones');
    } catch (falla) {
      if (fallo(falla)) return;
      setErrores(erroresPorCampo(falla.errores));
      setAviso(falla.codigo === 'PEDIDO_INVALIDO' ? 'Revisá los campos marcados.' : 'No pudimos guardar en este momento. Probá de nuevo.');
      setGuardando(false);
    }
  };

  return (
    <article className="producto-editor">
      <Link to="/panel/promociones" className="panel-volver">← Promociones</Link>
      <h1 className="promo-titulo">{esNueva ? 'Nueva promoción' : datos.nombre}</h1>
      {aviso && <p className="acceso-error" role="alert">{aviso}</p>}

      <form onSubmit={guardar} className="producto-formulario" noValidate>
        <label className="acceso-campo">
          <span>Nombre</span>
          <input value={datos.nombre} onChange={(e) => poner('nombre', e.target.value)} placeholder="Por ejemplo, Semana de jeans" aria-invalid={Boolean(errores.nombre)} />
          <MensajeError texto={errores.nombre} />
        </label>

        <fieldset className="selector">
          <legend>Tipo (RN-05)</legend>
          <div className="compra-medios local-medios">
            {[['PORCENTAJE', 'Descuento', 'Con cualquier medio de pago'], ['MEDIO_PAGO', 'Por medio de pago', 'Solo pagando con un medio']].map(([valor, titulo, detalle]) => (
              <label key={valor} className={`compra-medio${datos.tipo === valor ? ' elegido' : ''}`}>
                <input type="radio" name="tipo" checked={datos.tipo === valor} onChange={() => poner('tipo', valor)} />
                <span><strong>{titulo}</strong><small>{detalle}</small></span>
              </label>
            ))}
          </div>
          <MensajeError texto={errores.tipo} />
        </fieldset>

        <div className="producto-fila">
          {datos.tipo === 'MEDIO_PAGO' && (
            <label className="acceso-campo">
              <span>Medio de pago</span>
              <select value={datos.medioPago} onChange={(e) => poner('medioPago', e.target.value)}>
                {MEDIOS_LOCAL.map(([valor, nombre]) => <option key={valor} value={valor}>{nombre}</option>)}
              </select>
              <MensajeError texto={errores.medioPago} />
            </label>
          )}
          <label className="acceso-campo">
            <span>Porcentaje de descuento</span>
            <input type="number" min="1" max="100" step="1" value={datos.porcentaje} onChange={(e) => poner('porcentaje', e.target.value)} aria-invalid={Boolean(errores.porcentaje)} />
            <MensajeError texto={errores.porcentaje} />
          </label>
        </div>

        <fieldset className="selector">
          <legend>Vigencia (HU-05.2)</legend>
          <div className="producto-fila">
            <label className="acceso-campo">
              <span>Desde</span>
              <input type="datetime-local" value={datos.desde} onChange={(e) => poner('desde', e.target.value)} aria-invalid={Boolean(errores['vigencia.desde'])} />
              <MensajeError texto={errores['vigencia.desde']} />
            </label>
            <label className="acceso-campo">
              <span>Hasta</span>
              <input type="datetime-local" value={datos.hasta} onChange={(e) => poner('hasta', e.target.value)} aria-invalid={Boolean(errores['vigencia.hasta'])} />
              <MensajeError texto={errores['vigencia.hasta']} />
            </label>
          </div>
          <small className="acceso-ayuda">En la hora de esta computadora.</small>
        </fieldset>

        <fieldset className="selector">
          <legend>A qué se aplica</legend>
          <div className="panel-filtros">
            {[['TIENDA', 'Toda la tienda'], ['CATEGORIA', 'Categorías'], ['PRODUCTO', 'Productos']].map(([valor, nombre]) => (
              <button key={valor} type="button" aria-pressed={datos.alcanceTipo === valor} onClick={() => poner('alcanceTipo', valor)}>{nombre}</button>
            ))}
          </div>
          {datos.alcanceTipo === 'CATEGORIA' && (
            <div className="producto-talles" role="group" aria-label="Categorías de la promoción">
              {CATEGORIAS.map(([c, nombre]) => {
                const elegida = datos.categorias.includes(c);
                return (
                  <label key={c} className={elegida ? 'elegido' : ''}>
                    <input type="checkbox" checked={elegida} onChange={() => poner('categorias', elegida ? datos.categorias.filter((x) => x !== c) : [...datos.categorias, c])} />
                    {nombre}
                  </label>
                );
              })}
            </div>
          )}
          {datos.alcanceTipo === 'PRODUCTO' && <ElegirProductos token={token} elegidos={datos.productos} setElegidos={(f) => poner('productos', f(datos.productos))} />}
          <MensajeError texto={errores['alcance.ids'] || errores['alcance.tipo']} />
        </fieldset>

        <div className="producto-guardar">
          <button type="submit" className="boton" disabled={guardando}>{guardando ? 'Guardando…' : esNueva ? 'Crear promoción' : 'Guardar cambios'}</button>
        </div>
      </form>
    </article>
  );
}
