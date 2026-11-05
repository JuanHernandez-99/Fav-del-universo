import { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router';
import { actualizarPromocion, ErrorApi, listarPromociones } from '../../api.js';
import { nombreCategoria } from '../../dominio.js';
import { useSesion } from '../../sesion/contexto.jsx';
import { MEDIOS_CORTOS } from './ventas.js';
import { estadoDe, vigenciaParaTerminar } from './promociones.js';

const fecha = new Intl.DateTimeFormat('es-AR', { day: 'numeric', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' });
const GRUPOS = [['VIGENTE', 'Vigentes', 'bien'], ['PROXIMA', 'Próximas', 'pendiente'], ['TERMINADA', 'Terminadas', 'neutro']];

export function describirAlcance({ tipo, ids }) {
  if (tipo === 'TIENDA') return 'Toda la tienda';
  if (tipo === 'CATEGORIA') return `Categorías: ${ids.map(nombreCategoria).join(', ')}`;
  return `${ids.length} ${ids.length === 1 ? 'producto' : 'productos'}`;
}

function Tarjeta({ promocion, alTerminar, ocupado }) {
  const [preguntando, setPreguntando] = useState(false);
  const estado = estadoDe(promocion);
  return (
    <li className="promo-tarjeta">
      <div className="promo-porcentaje">−{promocion.porcentaje}%</div>
      <div className="promo-datos">
        <h3>{promocion.nombre}</h3>
        <p>{promocion.tipo === 'MEDIO_PAGO' ? `Solo pagando con ${MEDIOS_CORTOS[promocion.medioPago]?.toLowerCase() ?? promocion.medioPago}` : 'Con cualquier medio de pago'} · {describirAlcance(promocion.alcance)}</p>
        <p className="promo-vigencia">{fecha.format(new Date(promocion.vigencia.desde))} → {fecha.format(new Date(promocion.vigencia.hasta))}</p>
        {preguntando ? (
          <div className="resultado-confirmar">
            <p>{estado === 'PROXIMA' ? '¿Cancelarla? No va a llegar a aplicarse.' : '¿Terminarla ahora? Las ventas ya registradas conservan su precio (HU-05.1).'}</p>
            <button type="button" className="boton" disabled={ocupado} onClick={() => alTerminar(promocion)}>Sí, terminar</button>
            <button type="button" className="boton-texto" onClick={() => setPreguntando(false)}>No</button>
          </div>
        ) : (
          <div className="promo-acciones">
            <Link to={`/panel/promociones/${promocion.promocionId}`}>Editar</Link>
            {estado !== 'TERMINADA' && <button type="button" className="boton-texto" onClick={() => setPreguntando(true)}>{estado === 'PROXIMA' ? 'Cancelar' : 'Terminar ahora'}</button>}
          </div>
        )}
      </div>
    </li>
  );
}

export default function PanelPromociones() {
  const { sesion, cerrar } = useSesion();
  const [promociones, setPromociones] = useState(null);
  const [error, setError] = useState(false);
  const [ocupado, setOcupado] = useState(false);
  const [mensaje, setMensaje] = useState('');

  const cargar = useCallback(async () => {
    setError(false);
    try {
      setPromociones((await listarPromociones(sesion.token)).items);
    } catch (e) {
      if (e instanceof ErrorApi && [401, 403].includes(e.estado)) cerrar(); else setError(true);
    }
  }, [sesion.token, cerrar]);

  useEffect(() => { cargar(); }, [cargar]);

  const terminar = async (p) => {
    setOcupado(true);
    setMensaje('');
    try {
      const { promocionId, ...datos } = p;
      await actualizarPromocion(sesion.token, promocionId, { ...datos, vigencia: vigenciaParaTerminar(p) });
      setMensaje(`"${p.nombre}" terminó. Ya no se aplica a ventas nuevas.`);
      await cargar();
    } catch (e) {
      if (e instanceof ErrorApi && [401, 403].includes(e.estado)) cerrar(); else setMensaje('No pudimos terminarla en este momento. Probá de nuevo.');
    } finally {
      setOcupado(false);
    }
  };

  return (
    <>
      <div className="panel-encabezado">
        <h1>Promociones</h1>
        <Link to="/panel/promociones/nueva" className="boton">Nueva promoción</Link>
      </div>
      <p className="local-bajada">No se acumulan: si a una prenda le corresponden varias, se aplica la de mayor porcentaje (D3).</p>
      {mensaje && <p className="panel-mensaje local-registrada" role="status">{mensaje}</p>}
      {error && <div className="aviso" role="alert">No pudimos cargar las promociones. <button type="button" className="boton-texto" onClick={cargar}>Reintentar</button></div>}
      {!promociones && !error && <div className="mis-compras-esqueleto" aria-busy="true" aria-label="Cargando promociones" />}
      {promociones?.length === 0 && <p className="panel-vacio">Todavía no hay promociones.</p>}
      {promociones?.length > 0 && GRUPOS.map(([id, titulo, tono]) => {
        const delGrupo = promociones.filter((p) => estadoDe(p) === id)
          .sort((a, b) => (id === 'TERMINADA' ? b.vigencia.hasta.localeCompare(a.vigencia.hasta) : a.vigencia.desde.localeCompare(b.vigencia.desde)));
        if (delGrupo.length === 0) return null;
        // Las terminadas se guardan para el registro, pero se miran poco: van plegadas.
        if (id === 'TERMINADA') {
          return (
            <details key={id} className="promo-grupo">
              <summary><h2>{titulo} <span className={`resultado-estado ${tono}`}>{delGrupo.length}</span></h2></summary>
              <ul>{delGrupo.map((p) => <Tarjeta key={p.promocionId} promocion={p} alTerminar={terminar} ocupado={ocupado} />)}</ul>
            </details>
          );
        }
        return (
          <section key={id} className="promo-grupo">
            <h2>{titulo} <span className={`resultado-estado ${tono}`}>{delGrupo.length}</span></h2>
            <ul>{delGrupo.map((p) => <Tarjeta key={p.promocionId} promocion={p} alTerminar={terminar} ocupado={ocupado} />)}</ul>
          </section>
        );
      })}
    </>
  );
}
