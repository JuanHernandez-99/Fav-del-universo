import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import * as api from '../api.js';
import { borrarToken, guardarToken, leerSesion, sesionDesdeToken } from './sesion.js';

const Contexto = createContext(null);

// setTimeout no admite esperas más largas que esto.
const ESPERA_MAXIMA = 2 ** 31 - 1;

export function ProveedorSesion({ children }) {
  const [sesion, setSesion] = useState(() => leerSesion());

  const cerrar = useCallback(() => {
    borrarToken();
    setSesion(null);
  }, []);

  const ingresar = useCallback(async (email, contrasena) => {
    const { token } = await api.iniciarSesion(email, contrasena);
    guardarToken(token);
    setSesion(sesionDesdeToken(token));
  }, []);

  const registrar = useCallback(async (email, contrasena) => {
    await api.registrarUsuario(email, contrasena);
    await ingresar(email, contrasena);
  }, [ingresar]);

  // Sin refresh token (ADR-001): cuando el token vence, la sesión termina.
  useEffect(() => {
    if (!sesion) return undefined;
    const temporizador = setTimeout(cerrar, Math.min(sesion.venceEn - Date.now(), ESPERA_MAXIMA));
    return () => clearTimeout(temporizador);
  }, [sesion, cerrar]);

  const valor = useMemo(() => ({ sesion, ingresar, registrar, cerrar }), [sesion, ingresar, registrar, cerrar]);

  return <Contexto.Provider value={valor}>{children}</Contexto.Provider>;
}

export const useSesion = () => useContext(Contexto);
