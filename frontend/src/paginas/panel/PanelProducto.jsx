import { useCallback, useEffect, useState } from 'react';
import { Link, useLocation, useNavigate, useParams } from 'react-router';
import {
  actualizarProducto, agregarVarianteAdmin, crearProducto, despublicarProducto, ErrorApi, obtenerProductoAdmin, publicarProducto,
} from '../../api.js';
import { CATEGORIAS, colorDeMuestra, TALLES } from '../../dominio.js';
import { useSesion } from '../../sesion/contexto.jsx';
import { leerMonto } from './ventaLocal.js';
import { combinarVariantes, erroresPorCampo, nombreCampo, SISTEMAS, tallesDe } from './productos.js';

const vacio = { nombre: '', descripcion: '', categoria: '', sistemaTalles: 'LETRAS', precio: '', fotos: [] };

// Errores de Catálogo que no son de un campo del formulario.
function mensajeGeneral(e) {
  if (e.codigo === 'CONFLICTO_DE_VERSION') return 'Otra persona cambió el producto mientras lo editabas. Lo volvimos a cargar: revisá y guardá de nuevo.';
  if (e.codigo === 'VARIANTE_DUPLICADA') return 'Ya existe una variante con ese talle y color (HU-03.4).';
  if (e.codigo === 'PEDIDO_INVALIDO') return 'Revisá los campos marcados.';
  return 'No pudimos guardar en este momento. Probá de nuevo.';
}

function Campo({ etiqueta, error, children, ayuda }) {
  return (
    <label className="acceso-campo">
      <span>{etiqueta}</span>
      {children}
      {error ? <small role="alert">{error[0].toUpperCase() + error.slice(1)}</small> : ayuda && <small className="acceso-ayuda">{ayuda}</small>}
    </label>
  );
}

// Colores y talles del producto nuevo: todas las combinaciones se crean juntas.
function ElegirVariantes({ sistema, colores, setColores, talles, setTalles, error }) {
  const [color, setColor] = useState('');
  const sumarColor = () => { if (color.trim()) { setColores((c) => [...c, color.trim()]); setColor(''); } };
  const opciones = tallesDe(sistema);
  return (
    <fieldset className="selector producto-variantes">
      <legend>Variantes</legend>
      {sistema !== 'UNICO' && (
        <div className="producto-talles" role="group" aria-label="Talles">
          {opciones.map((t) => (
            <label key={t} className={talles.includes(t) ? 'elegido' : ''}>
              <input type="checkbox" checked={talles.includes(t)} onChange={(e) => setTalles((a) => (e.target.checked ? [...a, t] : a.filter((x) => x !== t)))} />
              {t}
            </label>
          ))}
        </div>
      )}
      <div className="producto-colores">
        <input value={color} onChange={(e) => setColor(e.target.value)} placeholder="Color, por ejemplo Blanco" aria-label="Color nuevo"
          onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); sumarColor(); } }} />
        <button type="button" className="boton-texto" onClick={sumarColor}>Agregar color</button>
      </div>
      {colores.length > 0 && (
        <ul className="producto-chips">
          {colores.map((c) => (
            <li key={c}><span className="muestra" style={{ background: colorDeMuestra(c) }} />{c}
              <button type="button" aria-label={`Quitar ${c}`} onClick={() => setColores((a) => a.filter((x) => x !== c))}>×</button></li>
          ))}
        </ul>
      )}
      {error && <small className="campo-error" role="alert">{error[0].toUpperCase() + error.slice(1)}</small>}
    </fieldset>
  );
}

export default function PanelProducto() {
  const { id } = useParams();
  const esNuevo = !id;
  const { sesion, cerrar } = useSesion();
  const token = sesion.token;
  const navegar = useNavigate();
  const { state } = useLocation();
  const [producto, setProducto] = useState(null);
  const [datos, setDatos] = useState(vacio);
  const [colores, setColores] = useState([]);
  const [talles, setTalles] = useState([]);
  const [errores, setErrores] = useState({});
  const [mensaje, setMensaje] = useState(state?.mensaje ?? '');
  const [aviso, setAviso] = useState('');
  const [guardando, setGuardando] = useState(false);
  const [nuevaVariante, setNuevaVariante] = useState({ talle: '', color: '' });
  const [noEncontrado, setNoEncontrado] = useState(false);

  const cargarEnFormulario = (p) => {
    setProducto(p);
    setDatos({ nombre: p.nombre, descripcion: p.descripcion ?? '', categoria: p.categoria, sistemaTalles: p.sistemaTalles, precio: String(p.precioBase.monto), fotos: p.fotos.map((f) => ({ url: f.url, color: f.color ?? '' })) });
  };

  const fallo = useCallback((e) => {
    if (e instanceof ErrorApi && [401, 403].includes(e.estado)) { cerrar(); return true; }
    return false;
  }, [cerrar]);

  const cargar = useCallback(async () => {
    try {
      cargarEnFormulario(await obtenerProductoAdmin(id, token));
    } catch (e) {
      if (fallo(e)) return;
      if (e instanceof ErrorApi && e.estado === 404) setNoEncontrado(true); else setAviso('No pudimos cargar el producto. Recargá la página.');
    }
  }, [id, token, fallo]);

  useEffect(() => { if (!esNuevo) cargar(); }, [esNuevo, cargar]);

  // Al tocar un campo se borra su error: ya no corresponde a lo que está escrito.
  const cambiar = (campo) => (e) => {
    setDatos((d) => ({ ...d, [campo]: e.target.value }));
    setErrores((actuales) => { const resto = { ...actuales }; delete resto[campo === 'precio' ? 'precioBase.monto' : campo]; return resto; });
  };
  const coloresDelProducto = esNuevo ? colores : [...new Set(producto?.variantes.map((v) => v.color) ?? [])];

  const guardar = async (e) => {
    e.preventDefault();
    setMensaje('');
    setAviso('');
    setErrores({});
    const monto = leerMonto(datos.precio);
    const variantes = esNuevo ? combinarVariantes(colores, datos.sistemaTalles === 'UNICO' ? ['U'] : talles) : null;
    const locales = {};
    if (monto === null || monto <= 0) locales['precioBase.monto'] = 'escribí el precio en pesos, por ejemplo 18900 o 15999,90';
    if (esNuevo && variantes.length === 0) locales.variantes = 'elegí al menos un color y un talle (RN-01)';
    if (Object.keys(locales).length) { setErrores(locales); return; }

    const cuerpo = {
      nombre: datos.nombre, descripcion: datos.descripcion, categoria: datos.categoria,
      precioBase: { monto, moneda: 'ARS' },
      fotos: datos.fotos.filter((f) => f.url.trim()).map((f) => ({ url: f.url.trim(), color: f.color || null })),
    };
    setGuardando(true);
    try {
      if (esNuevo) {
        const creado = await crearProducto(token, { ...cuerpo, sistemaTalles: datos.sistemaTalles, variantes });
        navegar(`/panel/productos/${creado.productoId}`, { replace: true, state: { mensaje: 'Producto creado. Queda sin publicar hasta que lo publiques (HU-03.1).' } });
        return;
      }
      // atributos no se edita acá, pero se manda tal cual: el PUT reemplaza los datos y si faltara se borraría.
      cargarEnFormulario(await actualizarProducto(token, id, { ...cuerpo, atributos: producto.atributos ?? null, version: producto.version }));
      setMensaje('Cambios guardados.');
    } catch (falla) {
      if (fallo(falla)) return;
      setErrores(erroresPorCampo(falla.errores));
      setAviso(mensajeGeneral(falla));
      if (falla.codigo === 'CONFLICTO_DE_VERSION') await cargar();
    } finally {
      setGuardando(false);
    }
  };

  const cambiarPublicacion = async () => {
    setGuardando(true);
    setMensaje('');
    setAviso('');
    try {
      if (producto.publicado) {
        await despublicarProducto(token, id); // baja lógica: el producto no se borra (D2)
        await cargar();
        setMensaje('Despublicado: ya no se ve en la tienda.');
      } else {
        cargarEnFormulario(await publicarProducto(token, id));
        setMensaje('Publicado: ya se ve en la tienda.');
      }
    } catch (falla) {
      if (!fallo(falla)) setAviso(mensajeGeneral(falla));
    } finally {
      setGuardando(false);
    }
  };

  const agregarVariante = async (e) => {
    e.preventDefault();
    setMensaje('');
    setAviso('');
    setErrores({});
    const talle = producto.sistemaTalles === 'UNICO' ? 'U' : nuevaVariante.talle;
    setGuardando(true);
    try {
      await agregarVarianteAdmin(token, id, { talle, color: nuevaVariante.color });
      await cargar();
      setNuevaVariante({ talle: '', color: '' });
      setMensaje('Variante agregada. Tiene stock 0 hasta que cargues un ingreso.');
    } catch (falla) {
      if (fallo(falla)) return;
      setErrores(Object.fromEntries(Object.entries(erroresPorCampo(falla.errores)).map(([k, v]) => [`variante.${k}`, v])));
      setAviso(mensajeGeneral(falla));
    } finally {
      setGuardando(false);
    }
  };

  if (noEncontrado) return <p className="panel-vacio">No existe ese producto. <Link to="/panel/productos">Volver a productos</Link></p>;
  if (!esNuevo && !producto) return aviso ? <p className="aviso">{aviso}</p> : <div className="mis-compras-esqueleto" aria-busy="true" aria-label="Cargando el producto" />;

  const otrosErrores = Object.entries(errores).filter(([c]) => !['nombre', 'categoria', 'precioBase.monto', 'variantes', 'sistemaTalles'].includes(c) && !c.startsWith('variante.') && !c.startsWith('fotos'));

  return (
    <article className="producto-editor">
      <Link to="/panel/productos" className="panel-volver">← Productos</Link>
      <div className="panel-venta-cabecera">
        <h1>{esNuevo ? 'Nuevo producto' : producto.nombre}</h1>
        {!esNuevo && <span className={`resultado-estado ${producto.publicado ? 'bien' : 'neutro'}`}>{producto.publicado ? 'Publicado' : 'Sin publicar'}</span>}
      </div>
      {!esNuevo && (
        <div className="producto-publicar">
          <button type="button" className={producto.publicado ? 'boton-texto' : 'boton'} disabled={guardando} onClick={cambiarPublicacion}>
            {producto.publicado ? 'Despublicar' : 'Publicar en la tienda'}
          </button>
          {producto.publicado && <Link to={`/producto/${producto.productoId}`} className="boton-texto">Ver en la tienda</Link>}
        </div>
      )}

      <div aria-live="polite">
        {mensaje && <p className="panel-mensaje local-registrada">{mensaje}</p>}
        {aviso && <p className="acceso-error" role="alert">{aviso}</p>}
        {otrosErrores.length > 0 && <ul className="acceso-error">{otrosErrores.map(([c, m]) => <li key={c}>{nombreCampo(c)}: {m}</li>)}</ul>}
      </div>

      <form onSubmit={guardar} className="producto-formulario" noValidate>
        <h2>Datos</h2>
        <Campo etiqueta="Nombre" error={errores.nombre}><input value={datos.nombre} onChange={cambiar('nombre')} aria-invalid={Boolean(errores.nombre)} /></Campo>
        <Campo etiqueta="Descripción (opcional)"><textarea rows={3} value={datos.descripcion} onChange={cambiar('descripcion')} /></Campo>
        <div className="producto-fila">
          <Campo etiqueta="Categoría" error={errores.categoria}>
            <select value={datos.categoria} onChange={cambiar('categoria')} aria-invalid={Boolean(errores.categoria)}>
              <option value="">Elegí una</option>
              {CATEGORIAS.map(([c, nombre]) => <option key={c} value={c}>{nombre}</option>)}
            </select>
          </Campo>
          <Campo etiqueta="Precio base" error={errores['precioBase.monto']} ayuda="Sin promociones: esas se cargan aparte (RN-05).">
            <input inputMode="decimal" value={datos.precio} onChange={cambiar('precio')} placeholder="$" aria-invalid={Boolean(errores['precioBase.monto'])} />
          </Campo>
        </div>

        {esNuevo && (
          <>
            <Campo etiqueta="Sistema de talles" error={errores.sistemaTalles} ayuda="No se puede cambiar después: los talles de las variantes dependen de él (RN-02).">
              <select value={datos.sistemaTalles} onChange={(e) => { cambiar('sistemaTalles')(e); setTalles([]); }}>
                {SISTEMAS.map((s) => <option key={s.id} value={s.id}>{s.nombre}</option>)}
              </select>
            </Campo>
            <ElegirVariantes sistema={datos.sistemaTalles} colores={colores} setColores={setColores} talles={talles} setTalles={setTalles} error={errores.variantes} />
            {colores.length > 0 && (datos.sistemaTalles === 'UNICO' || talles.length > 0) && (
              <p className="local-fisico">Se van a crear {combinarVariantes(colores, datos.sistemaTalles === 'UNICO' ? ['U'] : talles).length} variantes, con stock 0.</p>
            )}
          </>
        )}

        <h2>Fotos</h2>
        <p className="local-fisico">La primera es la principal. Una foto con color se muestra al elegir ese color (RN-03). Por ahora se guarda la dirección de la imagen.</p>
        {datos.fotos.map((f, i) => (
          <div key={i} className="producto-foto">
            <Campo etiqueta={`Foto ${i + 1}`} error={errores[`fotos[${i}].url`]}>
              <input type="url" value={f.url} placeholder="https://…" onChange={(e) => setDatos((d) => ({ ...d, fotos: d.fotos.map((x, j) => (j === i ? { ...x, url: e.target.value } : x)) }))} />
            </Campo>
            <select aria-label={`Color de la foto ${i + 1}`} value={f.color} onChange={(e) => setDatos((d) => ({ ...d, fotos: d.fotos.map((x, j) => (j === i ? { ...x, color: e.target.value } : x)) }))}>
              <option value="">Todos los colores</option>
              {coloresDelProducto.map((c) => <option key={c} value={c}>{c}</option>)}
            </select>
            <button type="button" className="boton-texto" onClick={() => setDatos((d) => ({ ...d, fotos: d.fotos.filter((_, j) => j !== i) }))}>Quitar</button>
          </div>
        ))}
        <button type="button" className="boton-texto" onClick={() => setDatos((d) => ({ ...d, fotos: [...d.fotos, { url: '', color: '' }] }))}>Agregar foto</button>

        <div className="producto-guardar">
          <button type="submit" className="boton" disabled={guardando}>{guardando ? 'Guardando…' : esNuevo ? 'Crear producto' : 'Guardar cambios'}</button>
        </div>
      </form>

      {!esNuevo && (
        <section className="producto-existentes">
          <h2>Variantes</h2>
          <p className="local-fisico">Una variante no se borra ni se renombra: su id es el que usan Inventario y las ventas. Sistema de talles: {SISTEMAS.find((s) => s.id === producto.sistemaTalles)?.nombre}.</p>
          <ul className="producto-chips">
            {[...producto.variantes].sort((a, b) => a.color.localeCompare(b.color) || TALLES.indexOf(a.talle) - TALLES.indexOf(b.talle)).map((v) => (
              <li key={v.varianteId}><span className="muestra" style={{ background: colorDeMuestra(v.color) }} />{v.color} · {v.talle === 'U' ? 'Único' : v.talle}</li>
            ))}
          </ul>
          <form className="producto-nueva-variante" onSubmit={agregarVariante}>
            {producto.sistemaTalles !== 'UNICO' && (
              <select aria-label="Talle de la variante nueva" value={nuevaVariante.talle} onChange={(e) => setNuevaVariante((v) => ({ ...v, talle: e.target.value }))}>
                <option value="">Talle</option>
                {tallesDe(producto.sistemaTalles).map((t) => <option key={t} value={t}>{t}</option>)}
              </select>
            )}
            <input aria-label="Color de la variante nueva" placeholder="Color" value={nuevaVariante.color} onChange={(e) => setNuevaVariante((v) => ({ ...v, color: e.target.value }))} />
            <button type="submit" className="boton" disabled={guardando || !nuevaVariante.color.trim() || (producto.sistemaTalles !== 'UNICO' && !nuevaVariante.talle)}>Agregar variante</button>
          </form>
          {(errores['variante.color'] || errores['variante.talle']) && <small className="campo-error" role="alert">{errores['variante.color'] || errores['variante.talle']}</small>}
        </section>
      )}
    </article>
  );
}
