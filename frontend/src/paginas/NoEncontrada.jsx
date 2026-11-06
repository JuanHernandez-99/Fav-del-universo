import { Link } from 'react-router';

export default function NoEncontrada() {
  return (
    <div className="estado-vacio pagina-angosta">
      <h1>Esta página se perdió en el universo</h1>
      <p>La dirección no existe o cambió.</p>
      <Link to="/" className="boton">Volver a la tienda</Link>
    </div>
  );
}
