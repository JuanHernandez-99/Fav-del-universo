import { Navigate, NavLink, Outlet, useLocation } from 'react-router';
import { useSesion } from '../../sesion/contexto.jsx';

export const ROL_ADMINISTRADOR = 'administrador';

// Esconder el panel es solo comodidad: quien autoriza cada pedido es el gateway, con el rol del token (ADR-016).
export default function Panel() {
  const { sesion } = useSesion();
  const { pathname } = useLocation();

  if (!sesion) return <Navigate to={`/ingresar?volver=${encodeURIComponent(pathname)}`} replace />;
  if (sesion.rol !== ROL_ADMINISTRADOR) {
    return (
      <div className="estado-vacio pagina-angosta">
        <h1>No tenés acceso al panel</h1>
        <p>El panel es solo para la cuenta de administración (HU-19).</p>
      </div>
    );
  }

  return (
    <div className="panel">
      <nav className="panel-menu" aria-label="Panel">
        <p className="panel-titulo">Panel</p>
        <NavLink to="/panel/ventas" end>Ventas</NavLink>
        <NavLink to="/panel/venta-local">Venta del local</NavLink>
        <NavLink to="/panel/productos">Productos</NavLink>
        <NavLink to="/panel/promociones">Promociones</NavLink>
        <NavLink to="/panel/stock">Stock</NavLink>
        <NavLink to="/panel/gimnasio">Gimnasio</NavLink>
      </nav>
      <div className="panel-contenido"><Outlet /></div>
    </div>
  );
}
