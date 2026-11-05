import { Link } from 'react-router';
import { coloresDe, colorDeMuestra, nombreCategoria, ordenarTalles } from '../dominio.js';
import FotoPrenda from './FotoPrenda.jsx';
import Precio from './Precio.jsx';

// disponibilidad: { varianteId: boolean }, o null si Inventario no respondió (no se afirma que no hay).
export default function TarjetaProducto({ producto, disponibilidad, prioridad }) {
  const colores = coloresDe(producto);
  const talles = ordenarTalles(producto.variantes.map((v) => v.talle)).map((t) => (t === 'U' ? 'Talle único' : t));
  const sinStock = disponibilidad != null && producto.variantes.every((v) => disponibilidad[v.varianteId] === false);

  return (
    <article className={`tarjeta${sinStock ? ' tarjeta-sin-stock' : ''}`}>
      <Link to={`/producto/${producto.productoId}`} className="tarjeta-enlace">
        <div className="tarjeta-foto">
          <FotoPrenda foto={producto.fotos[0]?.url} categoria={producto.categoria} color={colores[0]} nombre={producto.nombre} prioridad={prioridad} />
          {producto.promocion && <span className="etiqueta etiqueta-promo">−{producto.promocion.porcentaje}%</span>}
          {sinStock && <span className="etiqueta etiqueta-agotado">Sin stock</span>}
        </div>
        <div className="tarjeta-datos">
          <p className="tarjeta-categoria">{nombreCategoria(producto.categoria)}</p>
          <h3>{producto.nombre}</h3>
          <Precio precio={producto.precio} precioBase={producto.precioBase} />
          <div className="tarjeta-opciones">
            <span className="muestras" aria-label={`Colores: ${colores.join(', ')}`}>
              {colores.slice(0, 5).map((c) => <span key={c} className="muestra" style={{ background: colorDeMuestra(c) }} title={c} />)}
              {colores.length > 5 && <span className="muestra-mas">+{colores.length - 5}</span>}
            </span>
            <span className="tarjeta-talles">{talles.length > 4 ? `${talles[0]} a ${talles.at(-1)}` : talles.join(' · ')}</span>
          </div>
        </div>
      </Link>
    </article>
  );
}
