import { useEffect, useState } from 'react';
import { colorDeMuestra, nombreCategoria } from '../dominio.js';

const SILUETAS = {
  superior: 'M72 28c5 9 13 13 28 13s23-4 28-13l34 15 20 44-27 13-10-20v104H55V80l-10 20-27-13 20-44z',
  pantalon: 'M64 22h72l13 166h-37l-12-108-12 108H51z',
  corto: 'M60 54h80l16 86h-41l-15-50-15 50H44z',
  vestido: 'M78 22c4 8 11 11 22 11s18-3 22-11l14 8-8 50 34 108H30L64 80l-8-50z',
  pollera: 'M72 48h56l34 124H38z',
  accesorio: 'M100 44a56 56 0 1 1 0 112 56 56 0 0 1 0-112zm0 25a31 31 0 1 0 0 62 31 31 0 0 0 0-62z',
};
const SILUETA_DE = {
  tops: 'superior', bodys: 'superior', remeras: 'superior', buzos: 'superior', camperas: 'superior',
  jeans: 'pantalon', pantalones: 'pantalon', shorts: 'corto', polleras: 'pollera',
  vestidos: 'vestido', conjuntos: 'vestido', abanicos: 'accesorio', accesorios: 'accesorio',
};

// Se muestra mientras una prenda no tenga foto, o si la foto no carga: una silueta del color de la prenda.
function Silueta({ categoria, color }) {
  const tono = colorDeMuestra(color);
  return (
    <div className="silueta" role="img" aria-label={`${nombreCategoria(categoria)}${color ? ` color ${color}` : ''}, sin foto`}>
      <svg viewBox="0 0 200 210" aria-hidden="true">
        <path d={SILUETAS[SILUETA_DE[categoria] ?? 'superior']} fill={tono} />
      </svg>
    </div>
  );
}

export default function FotoPrenda({ foto, categoria, color, nombre, prioridad = false }) {
  const [fallo, setFallo] = useState(false);
  useEffect(() => setFallo(false), [foto]);

  if (!foto || fallo) return <Silueta categoria={categoria} color={color} />;
  return (
    <img src={foto} alt={nombre} loading={prioridad ? 'eager' : 'lazy'} onError={() => setFallo(true)} />
  );
}
