// Lecturas de la tienda con k6, para comparar con y sin caché (ADR-007). 70 % fichas y 30 % listados por categoría.
// Son las lecturas públicas del comprador: no usa token. La búsqueda por texto no pasa por acá, porque va a Elasticsearch.
//
// Correr (sistema levantado con `docker compose up -d`):
//   docker run --rm -i -e TASA=200 grafana/k6 run - < loadtest/catalogo.js
import http from 'k6/http';
import { check } from 'k6';

const BASE_URL = __ENV.BASE_URL || 'http://host.docker.internal:8080';
const TASA = Number(__ENV.TASA || 200); // pedidos por segundo
const DURACION = __ENV.DURACION || '60s';

export const options = {
  scenarios: {
    fichas: {
      executor: 'constant-arrival-rate', exec: 'verFicha',
      rate: Math.ceil(TASA * 0.7), timeUnit: '1s', duration: DURACION, preAllocatedVUs: 50, maxVUs: 500,
    },
    listados: {
      executor: 'constant-arrival-rate', exec: 'verListado',
      rate: Math.ceil(TASA * 0.3), timeUnit: '1s', duration: DURACION, preAllocatedVUs: 50, maxVUs: 500,
    },
  },
  summaryTrendStats: ['avg', 'med', 'p(95)', 'p(99)', 'max'],
  thresholds: { 'http_req_duration{lectura:ficha}': [], 'http_req_duration{lectura:listado}': [] }, // para ver cada lectura en el resumen
};

export function setup() {
  const r = http.get(`${BASE_URL}/catalogo/v1/tienda/productos?tamano=100`);
  check(r, { 'catálogo disponible': (x) => x.status === 200 });
  const items = r.json('items');
  return {
    ids: items.map((p) => p.productoId),
    categorias: [...new Set(items.map((p) => p.categoria))],
  };
}

const alAzar = (lista) => lista[Math.floor(Math.random() * lista.length)];

export function verFicha(datos) {
  const r = http.get(`${BASE_URL}/catalogo/v1/tienda/productos/${alAzar(datos.ids)}`, { tags: { lectura: 'ficha' } });
  check(r, { 'ficha 200': (x) => x.status === 200 });
}

export function verListado(datos) {
  const r = http.get(`${BASE_URL}/catalogo/v1/tienda/productos?categoria=${alAzar(datos.categorias)}&tamano=24`, { tags: { lectura: 'listado' } });
  check(r, { 'listado 200': (x) => x.status === 200 });
}
