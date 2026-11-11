import { useEffect, useState } from 'react';
import { colorDeMuestra, nombreCategoria } from '../dominio.js';
import { esTonoOscuro, ilustracionDe } from './ilustraciones.js';

// Se muestra mientras una prenda no tenga foto, o si la foto no carga: una ilustración del color elegido.
// semilla (el id del producto) elige cuál de las ilustraciones de la categoría.
function Silueta({ categoria, color, semilla }) {
  const tono = colorDeMuestra(color);
  return (
    <div className={`silueta${esTonoOscuro(tono) ? ' oscura' : ''}`} role="img" aria-label={`${nombreCategoria(categoria)}${color ? ` color ${color}` : ''}, sin foto`}>
      <svg viewBox="0 0 200 210" aria-hidden="true">
        {ilustracionDe(categoria, semilla).map(([tipo, d], i) => <path key={i} className={tipo} d={d} fill={tipo === 'base' ? tono : undefined} />)}
      </svg>
    </div>
  );
}

export default function FotoPrenda({ foto, categoria, color, nombre, semilla, prioridad = false }) {
  const [fallo, setFallo] = useState(false);
  useEffect(() => setFallo(false), [foto]);

  if (!foto || fallo) return <Silueta categoria={categoria} color={color} semilla={semilla} />;
  return (
    <img src={foto} alt={nombre} loading={prioridad ? 'eager' : 'lazy'} onError={() => setFallo(true)} />
  );
}
