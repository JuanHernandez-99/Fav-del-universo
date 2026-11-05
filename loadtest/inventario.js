// Prueba de carga de Inventario con k6 (ADR-013).
// ESTADO: se corrió una vez para validar el guion. Falta el escenario de pico (ver README).
//
// Correr (sistema levantado con `docker compose up -d`):
//   docker run --rm -i -e BASE_URL=http://host.docker.internal:8080 grafana/k6 run - < loadtest/inventario.js
import http from 'k6/http';
import { check } from 'k6';

const BASE_URL = __ENV.BASE_URL || 'http://host.docker.internal:8080';
const ADMIN_EMAIL = __ENV.ADMIN_EMAIL || 'admin@fav.example';
const ADMIN_PASSWORD = __ENV.ADMIN_PASSWORD || 'cambiar-admin-local';
const VARIANTE = 'var-carga-1';

export const options = {
  scenarios: {
    lecturas: {
      executor: 'constant-arrival-rate',
      exec: 'leerStock',
      rate: 70, timeUnit: '1s', duration: '60s',
      preAllocatedVUs: 20, maxVUs: 100,
    },
    reservas: {
      executor: 'constant-arrival-rate',
      exec: 'reservar',
      rate: 30, timeUnit: '1s', duration: '60s',
      preAllocatedVUs: 20, maxVUs: 100,
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<300'],
  },
};

const cabeceras = (token, extra = {}) => Object.assign(
  { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` }, extra);

export function setup() {
  // El token sale del inicio de sesión del administrador y se comparte con todos los usuarios virtuales.
  const sesion = http.post(`${BASE_URL}/usuarios/v1/sesiones`,
    JSON.stringify({ email: ADMIN_EMAIL, contrasena: ADMIN_PASSWORD }),
    { headers: { 'Content-Type': 'application/json' } });
  check(sesion, { 'inicio de sesión aceptado': (x) => x.status === 200 });
  const token = sesion.json('token');

  // Stock grande para que las reservas no se agoten durante la prueba.
  const r = http.post(`${BASE_URL}/inventario/v1/ingresos`,
    JSON.stringify({ varianteId: VARIANTE, canal: 'ONLINE', cantidad: 1000000 }),
    { headers: cabeceras(token, { 'Idempotency-Key': `carga-ingreso-${Date.now()}` }) });
  check(r, { 'ingreso inicial aceptado': (x) => x.status < 300 });
  return { token };
}

export function leerStock(datos) {
  const r = http.get(`${BASE_URL}/inventario/v1/stock?variantes=${VARIANTE}`, { headers: cabeceras(datos.token) });
  check(r, { 'stock 200': (x) => x.status === 200 });
}

export function reservar(datos) {
  const clave = `carga-${__VU}-${__ITER}-${Date.now()}`;
  const r = http.post(`${BASE_URL}/inventario/v1/reservas`,
    JSON.stringify({ referencia: clave, vigenciaMinutos: 5, items: [{ varianteId: VARIANTE, cantidad: 1 }] }),
    { headers: cabeceras(datos.token, { 'Idempotency-Key': clave }) });
  check(r, { 'reserva creada': (x) => x.status === 201 });
}
