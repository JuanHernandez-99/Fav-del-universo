import { useEffect, useState } from 'react';
import { Link, NavLink, Outlet, useLocation, useNavigate, useSearchParams } from 'react-router';
import { useCarrito } from '../carrito/contexto.jsx';
import { useSesion } from '../sesion/contexto.jsx';
import { CATEGORIAS } from '../dominio.js';

function Buscador() {
  const [parametros] = useSearchParams();
  const navegar = useNavigate();
  const actual = parametros.get('q') ?? '';
  const [texto, setTexto] = useState(actual);

  useEffect(() => setTexto(actual), [actual]);

  const buscar = (e) => {
    e.preventDefault();
    const q = texto.trim();
    navegar(q ? `/?q=${encodeURIComponent(q)}` : '/');
  };

  return (
    <form className="buscador" role="search" onSubmit={buscar}>
      <svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="7" /><path d="m20 20-3.800-3.800" /></svg>
      <input type="search" value={texto} onChange={(e) => setTexto(e.target.value)} placeholder="Buscar remeras, jeans, vestidos…" aria-label="Buscar productos" />
    </form>
  );
}

// Fuera de esas páginas, ingresar vuelve a donde estaba el comprador.
function enlaceIngresar(pathname, search) {
  if (pathname === '/' && !search) return '/ingresar';
  if (pathname === '/ingresar' || pathname === '/registrarme') return `/ingresar${search}`;
  return `/ingresar?volver=${encodeURIComponent(pathname + search)}`;
}

function Cuenta({ pathname, search }) {
  const { sesion, cerrar } = useSesion();
  if (!sesion) return <Link to={enlaceIngresar(pathname, search)} className="enlace-cuenta">Ingresar</Link>;
  return (
    <div className="cuenta">
      <span className="cuenta-email" title={sesion.email}>{sesion.email}</span>
      <button type="button" className="boton-texto" onClick={cerrar}>Salir</button>
    </div>
  );
}

export default function Marco() {
  const { unidades } = useCarrito();
  const { pathname, search } = useLocation();

  // Una página nueva arranca desde arriba; cambiar un filtro de la tienda, no.
  useEffect(() => { window.scrollTo(0, 0); }, [pathname]);
  useEffect(() => {
    if (pathname === '/' && new URLSearchParams(search).has('pagina')) window.scrollTo(0, 0);
  }, [pathname, search]);

  return (
    <>
      <a className="saltar" href="#contenido">Saltar al contenido</a>
      <header className="encabezado">
        <div className="encabezado-interior">
          <Link to="/" className="marca" aria-label="FAV del universo, ir al inicio">
            <span className="marca-fav">FAV</span>
            <span className="marca-universo">del universo</span>
          </Link>
          <Buscador />
          <nav className="navegacion" aria-label="Principal">
            <Cuenta pathname={pathname} search={search} />
            <NavLink to="/carrito" className="enlace-carrito" aria-label={`Carrito, ${unidades} ${unidades === 1 ? 'prenda' : 'prendas'}`}>
              <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M6 8h12l1.200 12H4.800L6 8Z" /><path d="M9 10V6.500a3 3 0 0 1 6 0V10" /></svg>
              <span>Carrito</span>
              {unidades > 0 && <span className="contador">{unidades}</span>}
            </NavLink>
          </nav>
        </div>
      </header>
      <main id="contenido"><Outlet /></main>
      <footer className="pie">
        <div className="pie-interior">
          <div>
            <p className="pie-marca">FAV <em>del universo</em></p>
            <p>Indumentaria femenina. La entrega se acuerda con el vendedor después de la compra.</p>
          </div>
          <nav aria-label="Categorías">
            <h2>Categorías</h2>
            <ul>{CATEGORIAS.slice(0, 6).map(([id, nombre]) => <li key={id}><Link to={`/?categoria=${id}`}>{nombre}</Link></li>)}</ul>
          </nav>
          <nav aria-label="Más categorías">
            <h2>Más</h2>
            <ul>{CATEGORIAS.slice(6).map(([id, nombre]) => <li key={id}><Link to={`/?categoria=${id}`}>{nombre}</Link></li>)}</ul>
          </nav>
        </div>
      </footer>
    </>
  );
}
