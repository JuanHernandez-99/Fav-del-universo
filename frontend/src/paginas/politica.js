// Contenido de la página de política de cambios (HU-24, RN-21). Es el único lugar para completarlo.
// [?] Pendiente con el cliente: el texto de la política y los datos de contacto (SPEC §12). No se inventan plazos ni condiciones.
export const POLITICA = {
  parrafos: [], // cada párrafo del texto que proporcione el cliente
  mail: '',
  telefono: '',
};

// Para el enlace tel:, sin espacios ni guiones: "+54 11 1234-5678" da "+541112345678".
export const enlaceTelefono = (telefono) => `tel:${telefono.replace(/[^\d+]/g, '')}`;

export const faltaTexto = ({ parrafos }) => parrafos.length === 0;
export const faltaContacto = ({ mail, telefono }) => !mail && !telefono;
