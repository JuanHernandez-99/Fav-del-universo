import { describe, expect, it } from 'vitest';
import { conDetalle, estadoReposicion, explicarRechazoReposicion, ESTADOS_REPOSICION } from './gimnasio.js';

describe('estados de una reposición (ADR-018)', () => {
  it('explica los cinco estados del contrato', () => {
    expect(Object.keys(ESTADOS_REPOSICION)).toEqual(['PENDIENTE_CONFIRMACION', 'CONFIRMADA', 'RECHAZADA', 'REQUIERE_REVISION', 'BLOQUEADA_CONFIGURACION']);
    Object.values(ESTADOS_REPOSICION).forEach((e) => expect(e.ayuda).not.toBe(''));
  });

  it('un estado desconocido se muestra tal cual, sin romper la pantalla', () => {
    expect(estadoReposicion('OTRO')).toMatchObject({ nombre: 'OTRO', tono: 'neutro' });
  });
});

describe('filas del gimnasio', () => {
  it('agrega el detalle de Catálogo y deja sin detalle lo que no conoce', () => {
    const filas = conDetalle(
      [{ varianteId: 'var-remera-basica-blanca-m', cantidad: 3 }, { varianteId: 'var-vieja', cantidad: 1 }],
      [{ varianteId: 'var-remera-basica-blanca-m', nombre: 'Remera básica', color: 'Blanco', talle: 'M' }],
    );
    expect(filas[0].detalle.nombre).toBe('Remera básica');
    expect(filas[1].detalle).toBeUndefined();
  });
});

describe('rechazos al crear una reposición', () => {
  it('dice cuánto hay en el local si no alcanza', () => {
    expect(explicarRechazoReposicion({ codigo: 'STOCK_INSUFICIENTE', faltantes: [{ disponible: 2 }] })).toContain('hay 2 unidades');
  });

  it('un error desconocido invita a reintentar sin duplicar', () => {
    expect(explicarRechazoReposicion({ codigo: 'NO_DISPONIBLE' })).toContain('no se va a aplicar dos veces');
  });
});
