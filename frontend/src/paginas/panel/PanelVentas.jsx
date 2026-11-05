import { useCallback, useEffect, useState } from 'react';
import { Link, useSearchParams } from 'react-router';
import { ErrorApi, listarVentasAdmin } from '../../api.js';
import { ESTADOS } from '../../compra/compra.js';
import { formatearPrecio } from '../../dominio.js';
import { useSesion } from '../../sesion/contexto.jsx';
import { CANALES, FILTROS, MEDIOS_CORTOS } from './ventas.js';

const TAMANO = 25;
const fecha = new Intl.DateTimeFormat('es-AR', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' });

export const nombreEstado = (estado) => (estado === 'REGISTRADA' ? { nombre: 'Venta del local', tono: 'neutro' } : ESTADOS[estado] ?? { nombre: estado, tono: 'pendiente' });

// Cambiar de filtro arma la lista de cero: no se ve ni un instante la lista del filtro anterior.
export default function PanelVentas() {
  const [parametros, setParametros] = useSearchParams();
  const estado = parametros.get('estado') ?? '';
  return <ListaVentas key={estado} estado={estado} filtrar={(id) => setParametros(id ? { estado: id } : {})} />;
}

function ListaVentas({ estado, filtrar }) {
  const { sesion, cerrar } = useSesion();
  const token = sesion.token;
  const [ventas, setVentas] = useState(null);
  const [hayMas, setHayMas] = useState(false);
  const [cargando, setCargando] = useState(true);
  const [error, setError] = useState(false);
  const [reintento, setReintento] = useState(0);

  // Se pide una de más para saber si hay otra página: Ventas no informa el total.
  const pedirPagina = useCallback(async (desde, signal) => {
    const { items } = await listarVentasAdmin(token, { estado, limite: TAMANO + 1, desde }, { signal });
    return { pagina: items.slice(0, TAMANO), mas: items.length > TAMANO };
  }, [token, estado]);

  const fallo = useCallback((e) => {
    if (e instanceof ErrorApi && [401, 403].includes(e.estado)) cerrar(); // el gateway rechazó el token
    else setError(true);
  }, [cerrar]);

  useEffect(() => {
    const control = new AbortController();
    setCargando(true);
    setError(false);
    pedirPagina(0, control.signal)
      .then(({ pagina, mas }) => { if (!control.signal.aborted) { setVentas(pagina); setHayMas(mas); } })
      .catch((e) => { if (!control.signal.aborted) fallo(e); })
      .finally(() => { if (!control.signal.aborted) setCargando(false); });
    return () => control.abort();
  }, [pedirPagina, fallo, reintento]);

  const verMas = async () => {
    setCargando(true);
    setError(false);
    try {
      const { pagina, mas } = await pedirPagina(ventas.length);
      setVentas((actuales) => {
        const vistas = new Set(actuales.map((v) => v.ventaId));
        return [...actuales, ...pagina.filter((v) => !vistas.has(v.ventaId))];
      });
      setHayMas(mas);
    } catch (e) {
      fallo(e);
    } finally {
      setCargando(false);
    }
  };

  return (
    <>
      <h1>Ventas</h1>
      <nav className="panel-filtros" aria-label="Filtrar por estado">
        {FILTROS.map(([id, nombre]) => (
          <button key={id} type="button" aria-pressed={estado === id}
            onClick={() => filtrar(id)}>{nombre}</button>
        ))}
      </nav>

      {error && (
        <div className="aviso" role="alert">
          No pudimos cargar las ventas en este momento.{' '}
          <button type="button" className="boton-texto" onClick={() => (ventas ? verMas() : setReintento((n) => n + 1))}>Reintentar</button>
        </div>
      )}
      {!ventas && cargando && <div className="mis-compras-esqueleto" aria-busy="true" aria-label="Cargando ventas" />}
      {ventas?.length === 0 && <p className="panel-vacio">No hay ventas {estado ? 'en este estado' : 'todavía'}.</p>}

      {ventas?.length > 0 && (
        <div className="panel-tabla" role="table" aria-label="Ventas">
          <div className="panel-fila panel-fila-titulos" role="row">
            {['Fecha', 'Comprador', 'Canal y pago', 'Total', 'Estado'].map((t) => <span key={t} role="columnheader">{t}</span>)}
          </div>
          {ventas.map((v) => {
            const e = nombreEstado(v.estado);
            return (
              <Link key={v.ventaId} to={`/panel/ventas/${v.ventaId}`} className="panel-fila" role="row">
                <span role="cell"><time dateTime={v.creadaEn}>{fecha.format(new Date(v.creadaEn))}</time></span>
                <span role="cell" className="panel-comprador">{v.email || v.referenciaExterna || '—'}</span>
                <span role="cell">{CANALES[v.canal] ?? v.canal} · {MEDIOS_CORTOS[v.pago.medio] ?? v.pago.medio}</span>
                <span role="cell" className="panel-total">{formatearPrecio(v.total)}</span>
                <span role="cell"><span className={`resultado-estado ${e.tono}`}>{e.nombre}</span></span>
              </Link>
            );
          })}
        </div>
      )}
      {hayMas && <button type="button" className="boton mis-compras-mas" disabled={cargando} onClick={verMas}>{cargando ? 'Cargando…' : 'Ver más'}</button>}
    </>
  );
}
