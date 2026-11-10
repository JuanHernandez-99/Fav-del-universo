import { useEffect, useState } from 'react';
import { Link, useSearchParams } from 'react-router';
import { ErrorApi, listarProductosAdmin } from '../../api.js';
import { CATEGORIAS, formatearPrecio, nombreCategoria } from '../../dominio.js';
import { useSesion } from '../../sesion/contexto.jsx';

const TAMANO = 20;
const FILTROS_PUBLICADO = [['', 'Todos'], ['true', 'Publicados'], ['false', 'Sin publicar']];

export default function PanelProductos() {
  const { sesion, cerrar } = useSesion();
  const [parametros, setParametros] = useSearchParams();
  const filtros = { q: parametros.get('q') ?? '', categoria: parametros.get('categoria') ?? '', publicado: parametros.get('publicado') ?? '', pagina: Number(parametros.get('pagina') ?? 1) };
  const clave = parametros.toString();
  const [texto, setTexto] = useState(filtros.q);
  const [resultado, setResultado] = useState(null);
  const [error, setError] = useState(false);
  const [reintento, setReintento] = useState(0);

  useEffect(() => {
    const control = new AbortController();
    setResultado(null);
    setError(false);
    const f = Object.fromEntries(new URLSearchParams(clave));
    listarProductosAdmin(sesion.token, { ...f, tamano: TAMANO }, { signal: control.signal })
      .then((r) => { if (!control.signal.aborted) setResultado(r); })
      .catch((e) => {
        if (control.signal.aborted) return;
        if (e instanceof ErrorApi && [401, 403].includes(e.estado)) cerrar(); else setError(true);
      });
    return () => control.abort();
  }, [clave, sesion.token, cerrar, reintento]);

  const cambiar = (nombre, valor) => {
    const nuevos = new URLSearchParams(clave);
    if (valor) nuevos.set(nombre, valor); else nuevos.delete(nombre);
    if (nombre !== 'pagina') nuevos.delete('pagina');
    setParametros(nuevos);
  };

  return (
    <>
      <div className="panel-encabezado">
        <h1>Productos</h1>
        <Link to="/panel/productos/nuevo" className="boton">Nuevo producto</Link>
      </div>

      <div className="panel-buscar">
        <form role="search" onSubmit={(e) => { e.preventDefault(); cambiar('q', texto.trim()); }}>
          <input type="search" value={texto} onChange={(e) => setTexto(e.target.value)} placeholder="Buscar por nombre o descripción" aria-label="Buscar en el panel de productos" />
        </form>
        <select value={filtros.categoria} onChange={(e) => cambiar('categoria', e.target.value)} aria-label="Categoría">
          <option value="">Todas las categorías</option>
          {CATEGORIAS.map(([id, nombre]) => <option key={id} value={id}>{nombre}</option>)}
        </select>
      </div>
      <nav className="panel-filtros" aria-label="Publicación">
        {FILTROS_PUBLICADO.map(([id, nombre]) => (
          <button key={id} type="button" aria-pressed={filtros.publicado === id} onClick={() => cambiar('publicado', id)}>{nombre}</button>
        ))}
      </nav>

      {error && <div className="aviso" role="alert">No pudimos cargar los productos. <button type="button" className="boton-texto" onClick={() => setReintento((n) => n + 1)}>Reintentar</button></div>}
      {!resultado && !error && <div className="mis-compras-esqueleto" aria-busy="true" aria-label="Cargando productos" />}
      {resultado?.items.length === 0 && <p className="panel-vacio">No hay productos con esos filtros.</p>}

      {resultado?.items.length > 0 && (
        <>
          <p className="resultado-total">{resultado.total} {resultado.total === 1 ? 'producto' : 'productos'}</p>
          <div className="panel-tabla" role="table" aria-label="Productos">
            <div className="panel-fila panel-fila-productos panel-fila-titulos" role="row">
              {['Producto', 'Categoría', 'Variantes', 'Precio base', 'Estado'].map((t) => <span key={t} role="columnheader">{t}</span>)}
            </div>
            {resultado.items.map((p) => (
              <Link key={p.productoId} to={`/panel/productos/${p.productoId}`} className="panel-fila panel-fila-productos" role="row">
                <span role="cell" className="panel-comprador"><strong>{p.nombre}</strong></span>
                <span role="cell">{nombreCategoria(p.categoria)}</span>
                <span role="cell">{p.variantes.length}</span>
                <span role="cell" className="panel-total">{formatearPrecio(p.precioBase.monto)}</span>
                <span role="cell"><span className={`resultado-estado ${p.publicado ? 'bien' : 'neutro'}`}>{p.publicado ? 'Publicado' : 'Sin publicar'}</span></span>
              </Link>
            ))}
          </div>
          {resultado.paginas > 1 && (
            <nav className="paginas" aria-label="Páginas">
              <button type="button" disabled={filtros.pagina <= 1} onClick={() => cambiar('pagina', String(filtros.pagina - 1))} aria-label="Página anterior">←</button>
              <span className="paginas-grupo">{filtros.pagina} de {resultado.paginas}</span>
              <button type="button" disabled={filtros.pagina >= resultado.paginas} onClick={() => cambiar('pagina', String(filtros.pagina + 1))} aria-label="Página siguiente">→</button>
            </nav>
          )}
        </>
      )}
    </>
  );
}
