import { useCallback, useEffect, useState } from 'react';
import { Link, useParams } from 'react-router';
import { avanzarVenta, confirmarTransferencia, ErrorApi, obtenerVentaAdmin } from '../../api.js';
import { formatearPrecio } from '../../dominio.js';
import { useSesion } from '../../sesion/contexto.jsx';
import { nombreEstado } from './PanelVentas.jsx';
import { accionesDe, CANALES, MEDIOS_CORTOS } from './ventas.js';

const fechaHora = new Intl.DateTimeFormat('es-AR', { dateStyle: 'long', timeStyle: 'short', hourCycle: 'h23' });

const ACCIONES = {
  CONFIRMAR: { boton: 'Confirmar que llegó la transferencia', pregunta: '¿Confirmás que la transferencia ya está en la cuenta? La venta pasa a Pagada y se le avisa al comprador.' },
  CONFIRMAR_TARDE: { boton: 'Confirmar una transferencia que llegó tarde', pregunta: 'La reserva ya no está vigente: la venta va a quedar A verificar y no se descuenta stock (RN-14). ¿Confirmás?' },
  AL_CORREO: { boton: 'Marcar como entregada al correo', pregunta: '¿Ya la entregaste al correo? Se le avisa al comprador por mail (HU-20.3).' },
  ENTREGADA: { boton: 'Marcar como entregada', pregunta: '¿El pedido ya llegó al comprador?' },
};

// Confirmar responde 202 mientras Inventario confirma la reserva: se consulta hasta que cambie, sin esperar para siempre.
const ESPERAS = [500, 1000, 1500, 2000, 3000, 4000];

export default function PanelVenta() {
  const { id } = useParams();
  const { sesion, cerrar } = useSesion();
  const token = sesion.token;
  const [venta, setVenta] = useState(null);
  const [error, setError] = useState(null);
  const [preguntando, setPreguntando] = useState(null);
  const [trabajando, setTrabajando] = useState(false);
  const [mensaje, setMensaje] = useState('');

  const cargar = useCallback(async () => {
    try {
      setVenta(await obtenerVentaAdmin(id, token));
      setError(null);
    } catch (e) {
      if (e instanceof ErrorApi && [401, 403].includes(e.estado)) cerrar();
      else setError(e instanceof ErrorApi && e.estado === 404 ? 'No existe esa venta.' : 'No pudimos cargar la venta en este momento.');
    }
  }, [id, token, cerrar]);

  useEffect(() => { cargar(); }, [cargar]);

  const ejecutar = async (accion) => {
    setTrabajando(true);
    setMensaje('');
    try {
      let actual = accion.startsWith('CONFIRMAR') ? await confirmarTransferencia(id, token) : await avanzarVenta(id, token);
      setVenta(actual);
      for (const espera of ESPERAS) {
        if (actual.estado !== 'PENDIENTE_DE_PAGO') break;
        setMensaje('Confirmando con Inventario…');
        await new Promise((f) => setTimeout(f, espera));
        actual = await obtenerVentaAdmin(id, token);
        setVenta(actual);
      }
      setMensaje(actual.estado === 'PENDIENTE_DE_PAGO'
        ? 'El pago quedó registrado. Inventario todavía no confirmó: la venta pasa a Pagada sola cuando responda.'
        : '');
      setPreguntando(null);
    } catch (e) {
      if (e instanceof ErrorApi && [401, 403].includes(e.estado)) { cerrar(); return; }
      setMensaje(e.codigo === 'TRANSICION_INVALIDA'
        ? 'La venta cambió mientras la mirabas. Ya está actualizada: revisá su estado.'
        : 'No pudimos hacer el cambio en este momento. Probá de nuevo.');
      setPreguntando(null);
      await cargar();
    } finally {
      setTrabajando(false);
    }
  };

  if (error) {
    return (
      <div className="panel-vacio" role="alert">
        <p>{error}</p>
        <button type="button" className="boton-texto" onClick={cargar}>Reintentar</button> · <Link to="/panel/ventas">Volver a ventas</Link>
      </div>
    );
  }
  if (!venta) return <div className="mis-compras-esqueleto" aria-busy="true" aria-label="Cargando la venta" />;

  const estado = nombreEstado(venta.estado);
  const acciones = accionesDe(venta);

  return (
    <article className="panel-venta">
      <Link to="/panel/ventas" className="panel-volver">← Ventas</Link>
      <div className="panel-venta-cabecera">
        <h1>{formatearPrecio(venta.total)}</h1>
        <span className={`resultado-estado ${estado.tono}`}>{estado.nombre}</span>
      </div>

      <dl className="resultado-datos panel-venta-datos">
        <div><dt>Comprador</dt><dd>{venta.email || '—'}</dd></div>
        {venta.referenciaExterna && <div><dt>Referencia del grupo</dt><dd>{venta.referenciaExterna}</dd></div>}
        <div><dt>Canal</dt><dd>{CANALES[venta.canal] ?? venta.canal}</dd></div>
        <div><dt>Medio de pago</dt><dd>{MEDIOS_CORTOS[venta.pago.medio] ?? venta.pago.medio}</dd></div>
        <div><dt>Creada</dt><dd>{fechaHora.format(new Date(venta.creadaEn))}</dd></div>
        {venta.pago.venceEn && venta.estado === 'PENDIENTE_DE_PAGO' && <div><dt>La reserva vence</dt><dd>{fechaHora.format(new Date(venta.pago.venceEn))}</dd></div>}
        <div><dt>Último cambio</dt><dd>{fechaHora.format(new Date(venta.actualizadaEn))}</dd></div>
      </dl>

      <h2>Prendas</h2>
      <ul className="compra-lineas panel-venta-prendas">
        {venta.items.map((i) => (
          <li key={i.varianteId}>
            <span>{i.cantidad} × {i.nombre || i.varianteId}<small>{[i.color, i.talle && `Talle ${i.talle === 'U' ? 'único' : i.talle}`].filter(Boolean).join(' · ')}</small></span>
            <span>{formatearPrecio(i.subtotal)}</span>
          </li>
        ))}
      </ul>

      {venta.estado === 'A_VERIFICAR' && (
        <p className="aviso">El pago llegó con la reserva vencida. Hay que resolverlo a mano con el comprador: cómo se resuelve está pendiente de definir con el cliente (SPEC §7).</p>
      )}

      <div className="panel-acciones" aria-live="polite">
        {mensaje && <p className="panel-mensaje">{mensaje}</p>}
        {preguntando ? (
          <div className="resultado-confirmar">
            <p>{ACCIONES[preguntando].pregunta}</p>
            <button type="button" className="boton" disabled={trabajando} onClick={() => ejecutar(preguntando)}>{trabajando ? 'Guardando…' : 'Sí, confirmar'}</button>
            <button type="button" className="boton-texto" disabled={trabajando} onClick={() => setPreguntando(null)}>No</button>
          </div>
        ) : (
          acciones.map((a) => (
            <button key={a} type="button" className={a === 'CONFIRMAR_TARDE' ? 'boton-texto' : 'boton'} disabled={trabajando} onClick={() => setPreguntando(a)}>
              {ACCIONES[a].boton}
            </button>
          ))
        )}
      </div>
      <p className="resultado-referencia">Venta {venta.ventaId}</p>
    </article>
  );
}
