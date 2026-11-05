import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import * as carrito from './carrito.js';

const Contexto = createContext(null);

export function ProveedorCarrito({ children }) {
  const [items, setItems] = useState(() => carrito.leerCarrito());

  useEffect(() => carrito.guardarCarrito(items), [items]);

  // Con la tienda abierta en dos pestañas, las dos ven el mismo carrito.
  useEffect(() => {
    const alCambiar = (e) => { if (e.storageArea === localStorage) setItems(carrito.leerCarrito()); };
    window.addEventListener('storage', alCambiar);
    return () => window.removeEventListener('storage', alCambiar);
  }, []);

  const agregar = useCallback((item, cantidad) => setItems((a) => carrito.agregar(a, item, cantidad)), []);
  const cambiarCantidad = useCallback((id, cantidad) => setItems((a) => carrito.cambiarCantidad(a, id, cantidad)), []);
  const quitar = useCallback((id) => setItems((a) => carrito.quitar(a, id)), []);

  const valor = useMemo(() => ({ items, agregar, cambiarCantidad, quitar, unidades: carrito.contarUnidades(items) }),
    [items, agregar, cambiarCantidad, quitar]);

  return <Contexto.Provider value={valor}>{children}</Contexto.Provider>;
}

export const useCarrito = () => useContext(Contexto);
