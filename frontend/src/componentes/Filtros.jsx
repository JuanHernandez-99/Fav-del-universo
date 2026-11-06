import { useEffect, useState } from 'react';
import { ORDENES, TALLES } from '../dominio.js';

// Los montos se aplican al salir del campo o con Enter, para no pedir el catálogo en cada tecla.
function CampoPrecio({ etiqueta, valor, alCambiar }) {
  const [texto, setTexto] = useState(valor);
  useEffect(() => setTexto(valor), [valor]);
  const aplicar = () => { if (texto !== valor) alCambiar(texto); };
  return (
    <label className="campo campo-precio">
      <span>{etiqueta}</span>
      <input type="number" inputMode="numeric" min="0" step="1" placeholder="$" value={texto}
        onChange={(e) => setTexto(e.target.value)} onBlur={aplicar}
        onKeyDown={(e) => { if (e.key === 'Enter') aplicar(); }} />
    </label>
  );
}

// Columna de filtros a la izquierda. En pantallas angostas se pliega detrás de un botón.
export default function Filtros({ filtros, colores, alCambiar, alLimpiar, hayFiltros }) {
  const [abiertos, setAbiertos] = useState(false);
  return (
    <div className="filtros">
      <button type="button" className="filtros-plegar" aria-expanded={abiertos} onClick={() => setAbiertos((a) => !a)}>
        {abiertos ? 'Ocultar filtros' : hayFiltros ? 'Filtros (aplicados)' : 'Filtros'}
      </button>
      <div className={`filtros-campos${abiertos ? ' abiertos' : ''}`} role="group" aria-label="Filtros">
        <p className="filtros-titulo">Filtrar</p>
        <label className="campo">
          <span>Talle</span>
          <select value={filtros.talle} onChange={(e) => alCambiar('talle', e.target.value)}>
            <option value="">Todos</option>
            {TALLES.map((t) => <option key={t} value={t}>{t === 'U' ? 'Único' : t}</option>)}
          </select>
        </label>
        <label className="campo">
          <span>Color</span>
          <select value={filtros.color} onChange={(e) => alCambiar('color', e.target.value)}>
            <option value="">Todos</option>
            {[...new Set([...colores, filtros.color].filter(Boolean))].sort().map((c) => <option key={c} value={c}>{c}</option>)}
          </select>
        </label>
        <fieldset className="filtros-precio">
          <legend>Precio</legend>
          <CampoPrecio etiqueta="Desde" valor={filtros.precioMin} alCambiar={(v) => alCambiar('precioMin', v)} />
          <CampoPrecio etiqueta="Hasta" valor={filtros.precioMax} alCambiar={(v) => alCambiar('precioMax', v)} />
        </fieldset>
        {hayFiltros && <button type="button" className="boton-texto" onClick={alLimpiar}>Limpiar filtros</button>}
      </div>
    </div>
  );
}

export function Orden({ valor, alCambiar }) {
  return (
    <label className="orden">
      <span>Ordenar por</span>
      <select value={valor || 'nuevos'} onChange={(e) => alCambiar(e.target.value)}>
        {ORDENES.map(([id, nombre]) => <option key={id} value={id}>{nombre}</option>)}
      </select>
    </label>
  );
}
