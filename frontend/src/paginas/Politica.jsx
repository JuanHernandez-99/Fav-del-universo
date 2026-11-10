import { Link } from 'react-router';
import { enlaceTelefono, faltaContacto, faltaTexto, POLITICA } from './politica.js';

// Página informativa y pública (HU-24): el sistema no gestiona cambios ni devoluciones (RN-21).
export default function Politica({ contenido = POLITICA }) {
  return (
    <article className="politica">
      <h1>Cambios, devoluciones y contacto</h1>

      <section>
        <h2>Cómo se gestionan</h2>
        <p>Los cambios, las devoluciones, el saldo a favor y los reclamos no se hacen desde la tienda: se gestionan por mail o por teléfono.</p>
        <p>Una compra que todavía no pagaste la podés cancelar vos. Después del pago, cualquier cambio se gestiona por esos mismos medios.</p>
      </section>

      <section>
        <h2>Política de cambios y devoluciones</h2>
        {faltaTexto(contenido)
          ? <p className="aviso">Estamos terminando de publicar la política de cambios y devoluciones. Mientras tanto, consultanos antes de comprar.</p>
          : contenido.parrafos.map((parrafo) => <p key={parrafo}>{parrafo}</p>)}
      </section>

      <section>
        <h2>Contacto</h2>
        {faltaContacto(contenido) ? (
          <p className="aviso">Estamos terminando de publicar los datos de contacto.</p>
        ) : (
          <ul className="politica-contacto">
            {contenido.mail && <li>Mail: <a href={`mailto:${contenido.mail}`}>{contenido.mail}</a></li>}
            {contenido.telefono && <li>Teléfono: <a href={enlaceTelefono(contenido.telefono)}>{contenido.telefono}</a></li>}
          </ul>
        )}
      </section>

      <Link to="/" className="boton">Volver a la tienda</Link>
    </article>
  );
}
