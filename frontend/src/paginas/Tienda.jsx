import { useEffect, useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router';
import { consultarDisponibilidad, ErrorApi, listarProductos } from '../api.js';
import { nombreCategoria } from '../dominio.js';
import Filtros from '../componentes/Filtros.jsx';
import TarjetaProducto from '../componentes/TarjetaProducto.jsx';

const TAMANO_PAGINA = 12;
const NOMBRES_FILTRO = ['q', 'categoria', 'talle', 'color', 'precioMin', 'precioMax', 'orden', 'pagina'];

function Cabecera({ filtros }) {
  if (filtros.q) {
    return <section className="cabecera-busqueda"><p>Resultados para</p><h1>“{filtros.q}”</h1></section>;
  }
  if (filtros.categoria) {
    return <section className="cabecera-busqueda"><p>Categoría</p><h1>{nombreCategoria(filtros.categoria)}</h1></section>;
  }
  return (
    <section className="portada">
      <div className="portada-estrellas" aria-hidden="true" />
      <div className="portada-interior">
        <p className="portada-volanta">Colección online</p>
        <h1>Ropa con <em>órbita</em> propia.</h1>
        <p className="portada-texto">Tu talle, tu color, tu estilo.<br />Comprá online, con cuenta o sin ella.</p>
      </div>
      <svg className="portada-astro" viewBox="0 0 400 400" aria-hidden="true">
        <circle cx="200" cy="200" r="104" />
        <ellipse cx="200" cy="200" rx="188" ry="46" transform="rotate(-24 200 200)" />
        <circle cx="42" cy="262" r="9" />
      </svg>
    </section>
  );
}

function Paginas({ pagina, paginas, alCambiar }) {
  if (paginas <= 1) return null;
  const visibles = [...new Set([1, pagina - 1, pagina, pagina + 1, paginas])].filter((n) => n >= 1 && n <= paginas).sort((a, b) => a - b);
  return (
    <nav className="paginas" aria-label="Páginas">
      <button type="button" disabled={pagina <= 1} onClick={() => alCambiar(pagina - 1)} aria-label="Página anterior">←</button>
      {visibles.map((n, i) => (
        <span key={n} className="paginas-grupo">
          {i > 0 && n - visibles[i - 1] > 1 && <span aria-hidden="true">…</span>}
          <button type="button" aria-current={n === pagina ? 'page' : undefined} onClick={() => alCambiar(n)}>{n}</button>
        </span>
      ))}
      <button type="button" disabled={pagina >= paginas} onClick={() => alCambiar(pagina + 1)} aria-label="Página siguiente">→</button>
    </nav>
  );
}

export default function Tienda() {
  const [parametros, setParametros] = useSearchParams();
  const clave = parametros.toString();
  const filtros = useMemo(
    () => Object.fromEntries(NOMBRES_FILTRO.map((n) => [n, new URLSearchParams(clave).get(n) ?? ''])),
    [clave],
  );

  const [resultado, setResultado] = useState(null);
  const [cargando, setCargando] = useState(true);
  const [error, setError] = useState(null);
  const [disponibilidad, setDisponibilidad] = useState({});
  const [colores, setColores] = useState([]);
  const [reintento, setReintento] = useState(0);

  useEffect(() => {
    const control = new AbortController();
    setCargando(true);
    setError(null);
    listarProductos({ ...filtros, tamano: TAMANO_PAGINA }, { signal: control.signal })
      .then(async (r) => {
        if (control.signal.aborted) return;
        setResultado(r);
        setCargando(false);
        // Catálogo e Inventario se piden aparte: sin Inventario, la tienda se sigue viendo (ADR-006).
        const ids = r.items.flatMap((p) => p.variantes.map((v) => v.varianteId));
        if (ids.length === 0) return;
        try {
          const d = await consultarDisponibilidad(ids, { signal: control.signal });
          if (!control.signal.aborted) setDisponibilidad(d);
        } catch {
          if (!control.signal.aborted) setDisponibilidad(null);
        }
      })
      .catch((e) => {
        if (control.signal.aborted) return;
        setError(e);
        setCargando(false);
      });
    return () => control.abort();
  }, [filtros, reintento]);

  // El color es texto libre: las opciones salen de los primeros productos del catálogo.
  useEffect(() => {
    const control = new AbortController();
    listarProductos({ tamano: 100 }, { signal: control.signal })
      .then((r) => setColores([...new Set(r.items.flatMap((p) => p.variantes.map((v) => v.color)))]))
      .catch(() => {});
    return () => control.abort();
  }, []);

  const cambiar = (nombre, valor) => {
    const nuevos = new URLSearchParams(clave);
    if (valor) nuevos.set(nombre, valor); else nuevos.delete(nombre);
    if (nombre !== 'pagina') nuevos.delete('pagina'); // otro filtro vuelve a la primera página
    setParametros(nuevos);
  };
  const limpiar = () => {
    const nuevos = new URLSearchParams();
    if (filtros.q) nuevos.set('q', filtros.q);
    setParametros(nuevos);
  };

  const hayFiltros = ['categoria', 'talle', 'color', 'precioMin', 'precioMax'].some((n) => filtros[n]);
  const invalido = error instanceof ErrorApi && error.estado === 400;

  return (
    <>
      <Cabecera filtros={filtros} />
      <div className="tienda" id="catalogo">
        <Filtros filtros={filtros} colores={colores} alCambiar={cambiar} alLimpiar={limpiar} hayFiltros={hayFiltros} />

        {disponibilidad === null && (
          <p className="aviso" role="status">Ahora no podemos consultar el stock. Podés seguir mirando las prendas.</p>
        )}

        {error ? (
          <div className="estado-vacio" role="alert">
            <h2>{invalido ? 'Revisá los filtros' : 'La tienda no está disponible en este momento'}</h2>
            <p>{invalido ? 'Algún valor no es válido: por ejemplo, el precio mínimo no puede superar al máximo.' : 'Es un problema de nuestro lado. Probá de nuevo en un rato.'}</p>
            {invalido
              ? <button type="button" className="boton" onClick={limpiar}>Limpiar filtros</button>
              : <button type="button" className="boton" onClick={() => setReintento((n) => n + 1)}>Reintentar</button>}
          </div>
        ) : resultado?.items.length === 0 && !cargando ? (
          <div className="estado-vacio">
            <h2>No encontramos prendas con esos filtros</h2>
            <p>Probá con menos filtros o con otra búsqueda.</p>
            <Link to="/" className="boton">Ver todo el catálogo</Link>
          </div>
        ) : !resultado ? (
          <div className="grilla" aria-busy="true" aria-label="Cargando productos">
            {Array.from({ length: 8 }, (_, i) => <div key={i} className="tarjeta-esqueleto" />)}
          </div>
        ) : (
          <>
            <p className="resultado-total" aria-live="polite">
              {resultado.total} {resultado.total === 1 ? 'prenda' : 'prendas'}
            </p>
            <div className={`grilla${cargando ? ' grilla-cargando' : ''}`} aria-busy={cargando}>
              {resultado.items.map((p, i) => (
                <TarjetaProducto key={p.productoId} producto={p} disponibilidad={disponibilidad} prioridad={i < 4} />
              ))}
            </div>
            <Paginas pagina={resultado.pagina} paginas={resultado.paginas} alCambiar={(n) => cambiar('pagina', String(n))} />
          </>
        )}
      </div>
    </>
  );
}
