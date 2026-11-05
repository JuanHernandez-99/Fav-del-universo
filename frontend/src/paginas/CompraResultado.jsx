import { useEffect, useState } from 'react';
import { Link, useParams } from 'react-router';
import { cancelarCompra, ErrorApi, obtenerMiCompra } from '../api.js';
import { ESTADOS, guardarVenta, leerVenta, MEDIOS } from '../compra/compra.js';
import { formatearPrecio } from '../dominio.js';
import { useSesion } from '../sesion/contexto.jsx';

const CADA_MS = 3000; // "ver el pago reflejado" sin recargar, mientras está pendiente

const fechaHora = new Intl.DateTimeFormat('es-AR', { day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' });

const TITULOS = {
  PAGADA: ['Tu compra está pagada', 'Recibimos tu pago. Te avisamos por mail cuando despachemos el pedido.'],
  ENTREGADA_AL_CORREO: ['Tu pedido está en camino', 'Lo entregamos al correo.'],
  ENTREGADA: ['Tu pedido fue entregado', '¡Gracias por tu compra!'],
  CANCELADA: ['La compra está cancelada', 'Las prendas reservadas se liberaron. Si el pago fue rechazado, podés volver a intentar desde la tienda.'],
  VENCIDA: ['La reserva venció', 'Pasó el plazo sin que se confirmara el pago y las prendas se liberaron.'],
  A_VERIFICAR: ['Estamos revisando tu pago', 'El pago se confirmó cuando la reserva ya había vencido. El local lo va a revisar.'],
};

function Copiar({ texto }) {
  const [copiado, setCopiado] = useState(false);
  if (!navigator.clipboard) return null; // solo existe con HTTPS o en localhost
  const copiar = async () => {
    await navigator.clipboard.writeText(texto);
    setCopiado(true);
    setTimeout(() => setCopiado(false), 2000);
  };
  return <button type="button" className="boton-texto" onClick={copiar}>{copiado ? 'Copiado' : 'Copiar'}</button>;
}

function Pendiente({ venta, conSesion }) {
  const { pago } = venta;
  const vence = pago.venceEn ? fechaHora.format(new Date(pago.venceEn)) : null;
  if (pago.medio === 'TRANSFERENCIA') {
    const datos = pago.datosTransferencia;
    return (
      <>
        <h1>Falta tu transferencia</h1>
        <p className="resultado-bajada">Tenés las prendas reservadas{vence && <> hasta el <strong>{vence}</strong></>}. Cuando confirmemos el pago, te avisamos por mail.</p>
        {datos && (
          <dl className="resultado-datos">
            <div><dt>Monto</dt><dd><strong>{formatearPrecio(venta.total)}</strong></dd></div>
            <div><dt>Alias</dt><dd>{datos.alias} <Copiar texto={datos.alias} /></dd></div>
            <div><dt>Titular</dt><dd>{datos.titular}</dd></div>
          </dl>
        )}
      </>
    );
  }
  return (
    <>
      <h1>Falta el pago</h1>
      <p className="resultado-bajada">Tenés las prendas reservadas{vence && <> hasta el <strong>{vence}</strong></>}. Se abre en otra pestaña.</p>
      {pago.urlPago && <a className="boton resultado-pagar" href={pago.urlPago} target="_blank" rel="noopener noreferrer">Pagar con Mercado Pago</a>}
      <p className="resultado-nota" role="status">
        {conSesion
          ? 'Cuando pagues, esta pantalla se actualiza sola.'
          : 'Como compraste sin cuenta, el resultado del pago te llega por mail. Guardá el mail: desde esta pantalla no se ve.'}
      </p>
    </>
  );
}

export default function CompraResultado() {
  const { id } = useParams();
  const { sesion } = useSesion();
  const [venta, setVenta] = useState(() => leerVenta(id));
  const [noEncontrada, setNoEncontrada] = useState(false);
  const [cancelando, setCancelando] = useState(false);
  const [confirmarCancelar, setConfirmarCancelar] = useState(false);
  const [errorCancelar, setErrorCancelar] = useState('');

  const actualizar = (nueva) => {
    guardarVenta(nueva);
    setVenta(nueva);
  };

  // Con cuenta, Ventas informa el estado. Se consulta mientras esté pendiente. El invitado no puede consultarlo (RN-24).
  const pendiente = !venta || venta.estado === 'PENDIENTE_DE_PAGO';
  useEffect(() => {
    if (!sesion || !pendiente) return undefined;
    let vigente = true;
    const consultar = async () => {
      try {
        const nueva = await obtenerMiCompra(id, sesion.token);
        if (vigente) actualizar(nueva);
      } catch (e) {
        // Una falla pasajera no cambia lo que se muestra: se vuelve a consultar en la próxima vuelta.
        if (vigente && e instanceof ErrorApi && e.estado === 404) setNoEncontrada(true);
      }
    };
    consultar();
    const temporizador = setInterval(consultar, CADA_MS);
    return () => { vigente = false; clearInterval(temporizador); };
  }, [id, sesion, pendiente]);

  const cancelar = async () => {
    setCancelando(true);
    setErrorCancelar('');
    try {
      actualizar(await cancelarCompra(id, sesion?.token));
      setConfirmarCancelar(false);
    } catch (e) {
      setErrorCancelar(e.codigo === 'TRANSICION_INVALIDA'
        ? 'Esta compra ya no se puede cancelar: puede que el pago se haya registrado. Revisá tu mail.' // HU-13.2
        : 'No pudimos cancelar en este momento. Probá de nuevo en un rato.');
    } finally {
      setCancelando(false);
    }
  };

  if (!venta) {
    if (sesion && !noEncontrada) return <div className="estado-vacio pagina-angosta" aria-busy="true"><p>Buscando tu compra…</p></div>;
    return (
      <div className="estado-vacio pagina-angosta">
        <h1>No podemos mostrar esta compra</h1>
        <p>{sesion ? 'No la encontramos en tu cuenta.' : 'El detalle se ve en la pestaña donde compraste. Los avisos de tu compra te llegan por mail.'}</p>
        <Link to="/" className="boton">Volver a la tienda</Link>
      </div>
    );
  }

  const estado = ESTADOS[venta.estado] ?? { nombre: venta.estado, tono: 'pendiente' };
  const titulo = TITULOS[venta.estado];

  return (
    <div className="resultado">
      <div className="resultado-principal">
        <p className={`resultado-estado ${estado.tono}`}>{estado.nombre}</p>
        {titulo
          ? <><h1>{titulo[0]}</h1><p className="resultado-bajada">{titulo[1]}</p></>
          : <Pendiente venta={venta} conSesion={Boolean(sesion)} />}

        {venta.email && <p className="resultado-nota">Los avisos van a <strong>{venta.email}</strong>.</p>}

        {errorCancelar && <p className="acceso-error" role="alert">{errorCancelar}</p>}
        {venta.estado === 'PENDIENTE_DE_PAGO' && (
          confirmarCancelar ? (
            <div className="resultado-confirmar">
              <p>¿Cancelar la compra? Las prendas se liberan.</p>
              <button type="button" className="boton" disabled={cancelando} onClick={cancelar}>{cancelando ? 'Cancelando…' : 'Sí, cancelar'}</button>
              <button type="button" className="boton-texto" onClick={() => setConfirmarCancelar(false)}>No, seguir</button>
            </div>
          ) : (
            <button type="button" className="boton-texto resultado-cancelar" onClick={() => setConfirmarCancelar(true)}>Cancelar la compra</button>
          )
        )}
        {venta.estado !== 'PENDIENTE_DE_PAGO' && <Link to="/" className="boton resultado-seguir">Volver a la tienda</Link>}
      </div>

      <aside className="carrito-resumen" aria-label="Detalle de la compra">
        <h2>Tu pedido</h2>
        <ul className="compra-lineas">
          {venta.items.map((i) => (
            <li key={i.varianteId}>
              <span>{i.cantidad} × {i.nombre}<small>{i.color} · Talle {i.talle === 'U' ? 'único' : i.talle}</small></span>
              <span>{formatearPrecio(i.subtotal)}</span>
            </li>
          ))}
        </ul>
        <dl><div><dt>Total</dt><dd>{formatearPrecio(venta.total)}</dd></div></dl>
        <p className="carrito-resumen-nota">Pago: {MEDIOS[venta.pago.medio]?.nombre ?? venta.pago.medio}</p>
        <p className="resultado-referencia">Compra {venta.ventaId}</p>
      </aside>
    </div>
  );
}
