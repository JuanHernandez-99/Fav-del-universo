import { useState } from 'react';
import { Link, Navigate, useNavigate, useSearchParams } from 'react-router';
import { useSesion } from '../sesion/contexto.jsx';
import { rutaDeVuelta } from '../sesion/sesion.js';

const LARGO_MINIMO = 8; // RN-26

const TEXTOS = {
  ingresar: { titulo: 'Hola de nuevo', bajada: 'Ingresá para comprar con tu cuenta y ver tus compras.', boton: 'Ingresar', ocupado: 'Ingresando…' },
  registrar: { titulo: 'Creá tu cuenta', bajada: 'Con una cuenta podés ver tus compras y su estado. También podés comprar sin cuenta.', boton: 'Crear cuenta', ocupado: 'Creando tu cuenta…' },
};

function mensajeDeError(error) {
  switch (error.codigo) {
    case 'CREDENCIALES_INVALIDAS': return 'El email o la contraseña no son correctos.'; // no revela si el email existe (HU-18.3)
    case 'EMAIL_EN_USO': return 'Ya hay una cuenta con ese email. Probá ingresar.';
    default:
  }
  if (error.estado === 429) return 'Hubo demasiados intentos. Esperá un minuto y probá de nuevo.';
  return 'No pudimos procesar el pedido en este momento. Probá de nuevo en un rato.';
}

export default function Acceso({ modo }) {
  const { sesion, ingresar, registrar } = useSesion();
  const navegar = useNavigate();
  const [parametros] = useSearchParams();
  const volver = rutaDeVuelta(parametros.get('volver'));
  const consulta = parametros.get('volver') ? `?volver=${encodeURIComponent(volver)}` : '';

  const [email, setEmail] = useState('');
  const [contrasena, setContrasena] = useState('');
  const [verContrasena, setVerContrasena] = useState(false);
  const [enviando, setEnviando] = useState(false);
  const [error, setError] = useState('');
  const [errorCampo, setErrorCampo] = useState({});

  // Evita mostrar el formulario a quien ya ingresó, salvo mientras se envía (la navegación la hace enviar).
  if (sesion && !enviando) return <Navigate to={volver} replace />;

  const textos = TEXTOS[modo];
  const esRegistro = modo === 'registrar';

  const enviar = async (e) => {
    e.preventDefault();
    setError('');
    setErrorCampo({});
    if (esRegistro && contrasena.length < LARGO_MINIMO) {
      setErrorCampo({ contrasena: `Tiene que tener al menos ${LARGO_MINIMO} caracteres.` });
      return;
    }
    setEnviando(true);
    try {
      await (esRegistro ? registrar : ingresar)(email.trim(), contrasena);
      navegar(volver, { replace: true });
    } catch (falla) {
      const campos = Object.fromEntries((falla.errores ?? []).map((c) => [c.campo, c.mensaje]));
      setErrorCampo(campos);
      if (Object.keys(campos).length === 0) setError(mensajeDeError(falla));
      setEnviando(false);
    }
  };

  return (
    <div className="acceso">
      <aside className="acceso-lateral" aria-hidden="true">
        <div className="portada-estrellas" />
        <svg className="acceso-astro" viewBox="0 0 400 400">
          <circle cx="200" cy="200" r="104" />
          <ellipse cx="200" cy="200" rx="188" ry="46" transform="rotate(-24 200 200)" />
          <circle cx="42" cy="262" r="9" />
        </svg>
        <p>Ropa con <em>órbita</em> propia.</p>
      </aside>

      <form className="acceso-formulario" onSubmit={enviar} noValidate>
        <h1>{textos.titulo}</h1>
        <p className="acceso-bajada">{textos.bajada}</p>

        {error && <p className="acceso-error" role="alert">{error}</p>}

        <label className="acceso-campo">
          <span>Email</span>
          <input type="email" name="email" autoComplete="email" required autoFocus value={email}
            aria-invalid={Boolean(errorCampo.email)} onChange={(e) => setEmail(e.target.value)} />
          {errorCampo.email && <small role="alert">{errorCampo.email}</small>}
        </label>

        <label className="acceso-campo">
          <span>Contraseña</span>
          <div className="acceso-contrasena">
            <input type={verContrasena ? 'text' : 'password'} name="contrasena" required value={contrasena}
              autoComplete={esRegistro ? 'new-password' : 'current-password'} aria-invalid={Boolean(errorCampo.contrasena)}
              onChange={(e) => setContrasena(e.target.value)} />
            <button type="button" className="boton-texto" aria-pressed={verContrasena} onClick={() => setVerContrasena((v) => !v)}>
              {verContrasena ? 'Ocultar' : 'Mostrar'}
            </button>
          </div>
          {errorCampo.contrasena
            ? <small role="alert">{errorCampo.contrasena}</small>
            : esRegistro && <small className="acceso-ayuda">Al menos {LARGO_MINIMO} caracteres.</small>}
        </label>

        <button type="submit" className="boton boton-ancho" disabled={enviando || !email.trim() || !contrasena}>
          {enviando ? textos.ocupado : textos.boton}
        </button>

        <p className="acceso-cambiar">
          {esRegistro
            ? <>¿Ya tenés cuenta? <Link to={`/ingresar${consulta}`}>Ingresá</Link></>
            : <>¿Todavía no tenés cuenta? <Link to={`/registrarme${consulta}`}>Creá una</Link></>}
        </p>
      </form>
    </div>
  );
}
