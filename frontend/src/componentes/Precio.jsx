import { formatearPrecio } from '../dominio.js';

// Precio original y con promoción (HU-01.4).
export default function Precio({ precio, precioBase, grande = false }) {
  const conPromocion = precioBase > precio;
  return (
    <p className={`precio${grande ? ' precio-grande' : ''}`}>
      {conPromocion && <del aria-label={`Antes ${formatearPrecio(precioBase)}`}>{formatearPrecio(precioBase)}</del>}
      <strong>{formatearPrecio(precio)}</strong>
    </p>
  );
}
