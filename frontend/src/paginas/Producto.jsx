import { useEffect, useMemo, useState } from 'react';
import { Link, useParams } from 'react-router';
import { consultarDisponibilidad, ErrorApi, obtenerProducto } from '../api.js';
import { useCarrito } from '../carrito/contexto.jsx';
import { coloresDe, colorDeMuestra, esColorClaro, nombreCategoria, ordenarTalles } from '../dominio.js';
import FotoPrenda from '../componentes/FotoPrenda.jsx';
import Precio from '../componentes/Precio.jsx';

export default function Producto() {
  const { id } = useParams();
  const { agregar, items } = useCarrito();
  const [producto, setProducto] = useState(null);
  const [error, setError] = useState(null);
  const [disponibilidad, setDisponibilidad] = useState(undefined); // undefined: consultando. null: no respondió.
  const [color, setColor] = useState('');
  const [talle, setTalle] = useState('');
  const [cantidad, setCantidad] = useState(1);
  const [fotoElegida, setFotoElegida] = useState(0);
  const [agregado, setAgregado] = useState(false);
  const [reintento, setReintento] = useState(0);

  useEffect(() => {
    const control = new AbortController();
    setProducto(null);
    setError(null);
    setDisponibilidad(undefined);
    obtenerProducto(id, { signal: control.signal })
      .then(async (p) => {
        if (control.signal.aborted) return;
        setProducto(p);
        let d = null;
        try {
          d = await consultarDisponibilidad(p.variantes.map((v) => v.varianteId), { signal: control.signal });
        } catch { /* Inventario caído: se avisa, sin afirmar que no hay */ }
        if (control.signal.aborted) return;
        setDisponibilidad(d);
        const conStock = d && p.variantes.find((v) => d[v.varianteId] > 0);
        setColor(conStock?.color ?? p.variantes[0].color);
        setTalle('');
      })
      .catch((e) => { if (!control.signal.aborted) setError(e); });
    return () => control.abort();
  }, [id, reintento]);

  const colorActual = color || producto?.variantes[0].color;

  const fotos = useMemo(() => {
    if (!producto) return [];
    const delColor = producto.fotos.filter((f) => f.color === colorActual);
    const generales = producto.fotos.filter((f) => !f.color);
    return [...delColor, ...generales]; // HU-03.5: al elegir un color se ven sus fotos
  }, [producto, colorActual]);

  useEffect(() => setFotoElegida(0), [colorActual]);
  useEffect(() => { if (!agregado) return undefined; const t = setTimeout(() => setAgregado(false), 3000); return () => clearTimeout(t); }, [agregado]);

  if (error) {
    const noExiste = error instanceof ErrorApi && error.estado === 404;
    return (
      <div className="estado-vacio pagina-angosta" role="alert">
        <h1>{noExiste ? 'Esta prenda ya no está en la tienda' : 'No pudimos cargar la prenda'}</h1>
        <p>{noExiste ? 'Puede que se haya despublicado. Mirá el resto del catálogo.' : 'Es un problema de nuestro lado. Probá de nuevo en un rato.'}</p>
        {noExiste
          ? <Link to="/" className="boton">Volver a la tienda</Link>
          : <button type="button" className="boton" onClick={() => setReintento((n) => n + 1)}>Reintentar</button>}
      </div>
    );
  }
  if (!producto) return <div className="ficha ficha-esqueleto" aria-busy="true" aria-label="Cargando prenda"><div /><div /></div>;

  const variantesDelColor = producto.variantes.filter((v) => v.color === colorActual);
  const talles = ordenarTalles(variantesDelColor.map((v) => v.talle));
  const varianteDe = (t) => variantesDelColor.find((v) => v.talle === t);
  const variante = varianteDe(talle);
  const hay = (v) => disponibilidad?.[v.varianteId] > 0;
  const consultando = disponibilidad === undefined;
  const sinDatos = disponibilidad === null;
  const colorAgotado = (c) => disponibilidad && producto.variantes.filter((v) => v.color === c).every((v) => !hay(v));
  const foto = fotos[Math.min(fotoElegida, Math.max(fotos.length - 1, 0))];
  const disponible = variante ? disponibilidad?.[variante.varianteId] ?? 0 : 0;
  const enCarrito = variante ? items.find((i) => i.varianteId === variante.varianteId)?.cantidad ?? 0 : 0;
  const restantes = Math.max(disponible - enCarrito, 0); // lo que todavía se puede sumar (HU-10.2)
  const puedeAgregar = variante && cantidad <= restantes && restantes > 0;

  const agregarAlCarrito = () => {
    agregar({
      varianteId: variante.varianteId, productoId: producto.productoId, nombre: producto.nombre, categoria: producto.categoria,
      talle: variante.talle, color: variante.color, precio: producto.precio, precioBase: producto.precioBase, foto: fotos[0]?.url ?? null,
    }, cantidad);
    setAgregado(true);
    setCantidad(1);
  };
  const elegirTalle = (t) => { setTalle(t); setCantidad(1); };

  return (
    <article className="ficha">
      <div className="ficha-galeria">
        <div className="ficha-foto">
          <FotoPrenda foto={foto?.url} categoria={producto.categoria} color={colorActual} nombre={`${producto.nombre}, color ${colorActual}`} prioridad />
          {producto.promocion && <span className="etiqueta etiqueta-promo">−{producto.promocion.porcentaje}%</span>}
        </div>
        {fotos.length > 1 && (
          <div className="ficha-miniaturas">
            {fotos.map((f, i) => (
              <button key={f.url} type="button" aria-label={`Ver foto ${i + 1}`} aria-pressed={i === fotoElegida} onClick={() => setFotoElegida(i)}>
                <FotoPrenda foto={f.url} categoria={producto.categoria} color={colorActual} nombre="" />
              </button>
            ))}
          </div>
        )}
      </div>

      <div className="ficha-datos">
        <nav className="migas" aria-label="Ubicación">
          <Link to="/">Tienda</Link><span aria-hidden="true">/</span>
          <Link to={`/?categoria=${producto.categoria}`}>{nombreCategoria(producto.categoria)}</Link>
        </nav>
        <h1>{producto.nombre}</h1>
        <Precio precio={producto.precio} precioBase={producto.precioBase} grande />
        {producto.promocion && <p className="ficha-promo">{producto.promocion.nombre}</p>}
        {producto.descripcion && <p className="ficha-descripcion">{producto.descripcion}</p>}

        <fieldset className="selector">
          <legend>Color <span>{colorActual}</span></legend>
          <div className="selector-colores">
            {coloresDe(producto).map((c) => (
              <button key={c} type="button" className={`muestra-boton${colorAgotado(c) ? ' agotado' : ''}${esColorClaro(c) ? ' claro' : ''}`}
                style={{ '--muestra': colorDeMuestra(c) }} aria-pressed={c === colorActual}
                aria-label={`${c}${colorAgotado(c) ? ', sin stock' : ''}`} title={c}
                onClick={() => { setColor(c); setTalle(''); setCantidad(1); }} />
            ))}
          </div>
        </fieldset>

        <fieldset className="selector">
          <legend>Talle {variante && <span>{talle === 'U' ? 'Único' : talle}</span>}</legend>
          <div className="selector-talles">
            {talles.map((t) => {
              const sinStock = disponibilidad && !hay(varianteDe(t));
              return (
                <button key={t} type="button" className={sinStock ? 'agotado' : ''} disabled={sinStock || consultando || sinDatos}
                  aria-pressed={t === talle} aria-label={`Talle ${t === 'U' ? 'único' : t}${sinStock ? ', sin stock' : ''}`} onClick={() => elegirTalle(t)}>
                  {t === 'U' ? 'Único' : t}
                </button>
              );
            })}
          </div>
          {disponibilidad && variantesDelColor.every((v) => !hay(v)) && <p className="selector-nota">Este color no tiene stock online.</p>}
          {variante && hay(variante) && (
            <p className={`selector-stock${disponible === 1 ? ' ultima' : ''}`} role="status">
              {disponible === 1 ? 'Última unidad disponible' : `${disponible} unidades disponibles`}
              {enCarrito > 0 && ` · ${enCarrito} en tu carrito`}
            </p>
          )}
        </fieldset>

        {sinDatos && (
          <div className="aviso" role="status">
            Ahora no podemos consultar el stock, así que no se puede agregar al carrito.{' '}
            <button type="button" className="boton-texto" onClick={() => setReintento((n) => n + 1)}>Reintentar</button>
          </div>
        )}

        <div className="ficha-comprar">
          <div className="cantidad">
            <button type="button" aria-label="Quitar una unidad" disabled={cantidad <= 1} onClick={() => setCantidad((n) => n - 1)}>−</button>
            <output aria-label="Cantidad">{cantidad}</output>
            <button type="button" aria-label="Sumar una unidad" disabled={!variante || cantidad >= restantes} onClick={() => setCantidad((n) => n + 1)}>+</button>
          </div>
          <button type="button" className="boton boton-ancho" disabled={!puedeAgregar} onClick={agregarAlCarrito}>
            {consultando ? 'Consultando stock…' : sinDatos ? 'Stock no disponible' : !variante ? 'Elegí un talle' : restantes === 0 ? 'Ya están todas en tu carrito' : 'Agregar al carrito'}
          </button>
        </div>
        <p className="ficha-mensaje" role="status">
          {agregado && <>Listo, quedó en tu carrito. <Link to="/carrito">Ver carrito</Link></>}
        </p>

        <ul className="ficha-notas">
          <li>Agregar al carrito no aparta la prenda: se reserva cuando comprás, así que las unidades pueden cambiar.</li>
          <li>El total final se confirma al comprar, según el medio de pago.</li>
          <li>La entrega se acuerda con el vendedor.</li>
        </ul>
      </div>
    </article>
  );
}
