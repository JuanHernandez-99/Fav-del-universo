import { useEffect, useRef, useState } from 'react';
import { Link, useLocation } from 'react-router';
import { CATEGORIAS } from '../dominio.js';

// Las categorías de la tienda (RN-17) en un desplegable junto a la marca. Se cierra al elegir, al tocar
// afuera o con Escape, y marca la categoría que se está mirando.
export default function MenuCategorias() {
  const [abierto, setAbierto] = useState(false);
  const contenedor = useRef(null);
  const boton = useRef(null);
  const { pathname, search } = useLocation();
  const actual = pathname === '/' ? new URLSearchParams(search).get('categoria') ?? '' : null;

  // Elegir una categoría lo cierra al tocarla; esto cubre además volver atrás o adelante con el navegador.
  useEffect(() => setAbierto(false), [pathname, search]);

  useEffect(() => {
    if (!abierto) return undefined;
    const afuera = (e) => { if (!contenedor.current?.contains(e.target)) setAbierto(false); };
    const tecla = (e) => { if (e.key === 'Escape') { setAbierto(false); boton.current?.focus(); } };
    document.addEventListener('pointerdown', afuera);
    document.addEventListener('keydown', tecla);
    return () => { document.removeEventListener('pointerdown', afuera); document.removeEventListener('keydown', tecla); };
  }, [abierto]);

  return (
    <div className="menu-categorias" ref={contenedor}>
      <button ref={boton} type="button" className="menu-categorias-boton" aria-expanded={abierto} aria-controls="menu-categorias-lista"
        onClick={() => setAbierto((a) => !a)}>
        {/* En el celular se ve solo el ícono, pero el texto sigue ahí para los lectores de pantalla. */}
        <svg className="menu-categorias-icono" viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h16M4 12h16M4 17h16" /></svg>
        <span className="menu-categorias-texto">Categorías</span>
        <svg className="menu-categorias-flecha" viewBox="0 0 12 12" aria-hidden="true"><polyline points="3,4.5 6,7.5 9,4.5" /></svg>
      </button>
      {abierto && (
        <nav id="menu-categorias-lista" className="menu-categorias-lista" aria-label="Categorías"
          onClick={(e) => { if (e.target.closest('a')) setAbierto(false); }}>
          <Link to="/" className="menu-categorias-todo" aria-current={actual === '' ? 'page' : undefined}>Toda la tienda</Link>
          <ul>
            {CATEGORIAS.map(([id, nombre]) => (
              <li key={id}><Link to={`/?categoria=${id}`} aria-current={actual === id ? 'page' : undefined}>{nombre}</Link></li>
            ))}
          </ul>
        </nav>
      )}
    </div>
  );
}
