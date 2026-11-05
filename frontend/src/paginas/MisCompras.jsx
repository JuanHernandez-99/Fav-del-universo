import { useCallback, useEffect, useState } from 'react';
import { Link, Navigate } from 'react-router';
import { ErrorApi, listarMisCompras } from '../api.js';
import { ESTADOS } from '../compra/compra.js';
import { formatearPrecio } from '../dominio.js';
import { useSesion } from '../sesion/contexto.jsx';

const TAMANO = 10;
const fecha = new Intl.DateTimeFormat('es-AR', { day: 'numeric', month: 'long', year: 'numeric' });

function Tarjeta({ venta }) {
  const estado = ESTADOS[venta.estado] ?? { nombre: venta.estado, tono: 'pendiente' };
  return (
    <li>
      <Link to={`/compra/${venta.ventaId}`} className="mis-compras-tarjeta">
        <div className="mis-compras-cabecera">
          <time dateTime={venta.creadaEn}>{fecha.format(new Date(venta.creadaEn))}</time>
          <span className={`resultado-estado ${estado.tono}`}>{estado.nombre}</span>
        </div>
        <ul className="mis-compras-prendas">
          {venta.items.map((i) => (
            <li key={i.varianteId}>{i.cantidad} × {i.nombre} <small>{i.color} · Talle {i.talle === 'U' ? 'único' : i.talle}</small></li>
          ))}
        </ul>
        <div className="mis-compras-pie">
          <strong>{formatearPrecio(venta.total)}</strong>
          <span>{venta.estado === 'PENDIENTE_DE_PAGO' ? 'Ver cómo pagar' : 'Ver detalle'} →</span>
        </div>
      </Link>
    </li>
  );
}

export default function MisCompras() {
  const { sesion, cerrar } = useSesion();
  const token = sesion?.token;
  const [ventas, setVentas] = useState(null);
  const [hayMas, setHayMas] = useState(false);
  const [cargando, setCargando] = useState(true);
  const [error, setError] = useState(false);
  const [reintento, setReintento] = useState(0);

  // Se pide una de más para saber si hay otra página: Ventas no informa el total.
  const pedirPagina = useCallback(async (desde, signal) => {
    const { items } = await listarMisCompras(token, { limite: TAMANO + 1, desde }, { signal });
    return { pagina: items.slice(0, TAMANO), mas: items.length > TAMANO };
  }, [token]);

  useEffect(() => {
    if (!token) return undefined;
    const control = new AbortController();
    setCargando(true);
    setError(false);
    pedirPagina(0, control.signal)
      .then(({ pagina, mas }) => {
        if (control.signal.aborted) return;
        setVentas(pagina);
        setHayMas(mas);
        setCargando(false);
      })
      .catch((e) => {
        if (control.signal.aborted) return;
        if (e instanceof ErrorApi && e.estado === 401) { cerrar(); return; } // token rechazado por el gateway
        setError(true);
        setCargando(false);
      });
    return () => control.abort();
  }, [token, pedirPagina, cerrar, reintento]);

  if (!sesion) return <Navigate to={`/ingresar?volver=${encodeURIComponent('/mis-compras')}`} replace />; // HU-14.2

  const verMas = async () => {
    setCargando(true);
    setError(false);
    try {
      const { pagina, mas } = await pedirPagina(ventas.length);
      setVentas((actuales) => {
        const vistas = new Set(actuales.map((v) => v.ventaId)); // una compra nueva corre la lista: no repetir
        return [...actuales, ...pagina.filter((v) => !vistas.has(v.ventaId))];
      });
      setHayMas(mas);
    } catch {
      setError(true);
    } finally {
      setCargando(false);
    }
  };

  return (
    <div className="mis-compras">
      <h1>Mis compras</h1>
      {error && (
        <div className="aviso" role="alert">
          No pudimos cargar tus compras en este momento.{' '}
          <button type="button" className="boton-texto" onClick={() => (ventas ? verMas() : setReintento((n) => n + 1))}>Reintentar</button>
        </div>
      )}
      {!ventas && cargando && <div className="mis-compras-esqueleto" aria-busy="true" aria-label="Cargando tus compras" />}
      {ventas?.length === 0 && (
        <div className="estado-vacio">
          <h2>Todavía no compraste con esta cuenta</h2>
          <p>Las compras que hagas sin iniciar sesión te llegan por mail y no aparecen acá.</p>
          <Link to="/" className="boton">Ir a la tienda</Link>
        </div>
      )}
      {ventas?.length > 0 && <ul className="mis-compras-lista">{ventas.map((v) => <Tarjeta key={v.ventaId} venta={v} />)}</ul>}
      {hayMas && <button type="button" className="boton mis-compras-mas" disabled={cargando} onClick={verMas}>{cargando ? 'Cargando…' : 'Ver más'}</button>}
    </div>
  );
}
