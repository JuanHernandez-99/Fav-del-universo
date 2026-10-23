// Prueba de carga de Inventario con k6 (ADR-013).
// ESTADO: guion SIN EJECUTAR. Ajustar tasas y variantes después de la primera corrida.
//
// Correr (sistema levantado con `docker compose up -d`):
//   docker run --rm -i -e BASE_URL=http://host.docker.internal:8080 grafana/k6 run - < loadtest/inventario.js
import http from 'k6/http';
import { check } from 'k6';

const BASE_URL = __ENV.BASE_URL || 'http://host.docker.internal:8080';
const ADMIN_KEY = __ENV.ADMIN_KEY || 'clave-admin-local';
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

const cabeceras = (extra = {}) => Object.assign(
  { 'Content-Type': 'application/json', 'X-Admin-Key': ADMIN_KEY }, extra);

export function setup() {
  // Stock grande para que las reservas no se agoten durante la prueba.
  const r = http.post(`${BASE_URL}/inventario/v1/ingresos`,
    JSON.stringify({ varianteId: VARIANTE, canal: 'ONLINE', cantidad: 1000000 }),
    { headers: cabeceras({ 'Idempotency-Key': `carga-ingreso-${Date.now()}` }) });
  check(r, { 'ingreso inicial aceptado': (x) => x.status < 300 });
}

export function leerStock() {
  const r = http.get(`${BASE_URL}/inventario/v1/stock?variantes=${VARIANTE}`, { headers: cabeceras() });
  check(r, { 'stock 200': (x) => x.status === 200 });
}

export function reservar() {
  const clave = `carga-${__VU}-${__ITER}-${Date.now()}`;
  const r = http.post(`${BASE_URL}/inventario/v1/reservas`,
    JSON.stringify({ referencia: clave, vigenciaMinutos: 5, items: [{ varianteId: VARIANTE, cantidad: 1 }] }),
    { headers: cabeceras({ 'Idempotency-Key': clave }) });
  check(r, { 'reserva creada': (x) => x.status === 201 });
}
