// Pico de la tienda (RNF-06, ADR-013): compradores que miran fichas y listados, buscan, consultan unidades y compran.
// Todo entra por el gateway, como en el navegador. Las compras pasan por Ventas → Catálogo → Inventario.
//
// La mezcla es una [propuesta] del ADR-013, no un dato medido de FAV:
//   ficha 40 % · listado 25 % · búsqueda 15 % · unidades disponibles 15 % · compra 5 %
// La mitad de las compras va a la misma remera ("la más pedida"), para ver la contención sobre una fila de stock.
//
// Correr una tasa (sistema levantado con `docker compose up -d`):
//   docker run --rm -i -e TASA=100 grafana/k6 run - < loadtest/tienda.js
// Todas las tasas, con el uso de CPU de cada contenedor: bash loadtest/pico_tienda.sh
import http from 'k6/http';
import { check } from 'k6';

const BASE_URL = __ENV.BASE_URL || 'http://host.docker.internal:8080';
const ADMIN_EMAIL = __ENV.ADMIN_EMAIL || 'admin@fav.example';
const ADMIN_PASSWORD = __ENV.ADMIN_PASSWORD || 'cambiar-admin-local';
const TASA = Number(__ENV.TASA || 50); // pedidos por segundo, sumando todas las operaciones
const DURACION = __ENV.DURACION || '60s';
const VARIANTES_A_COMPRAR = 10;

const MEZCLA = [
  ['verFicha', 0.40],
  ['verListado', 0.25],
  ['buscar', 0.15],
  ['verDisponibles', 0.15],
  ['comprar', 0.05],
];

// Un escenario por operación, con tasa fija: si el sistema se pone lento, k6 no baja la carga (ADR-013).
export const options = {
  scenarios: Object.fromEntries(MEZCLA.map(([operacion, peso]) => [operacion, {
    executor: 'constant-arrival-rate', exec: operacion,
    rate: Math.max(1, Math.round(TASA * peso)), timeUnit: '1s', duration: DURACION,
    preAllocatedVUs: 20, maxVUs: 400,
  }])),
  summaryTrendStats: ['avg', 'med', 'p(95)', 'p(99)', 'max'],
  // Umbrales vacíos: solo para que el resumen muestre cada operación por separado.
  thresholds: Object.fromEntries([
    ...MEZCLA.map(([operacion]) => `http_req_duration{operacion:${operacion}}`),
    'http_req_duration{variante:popular}', 'http_req_duration{variante:otra}', // contención sobre una fila de stock
  ].map((metrica) => [metrica, []])),
};

const PALABRAS = ['remera', 'buzo', 'top', 'calza', 'campera', 'basica', 'lila', 'negro'];

export function setup() {
  const sesion = http.post(`${BASE_URL}/usuarios/v1/sesiones`,
    JSON.stringify({ email: ADMIN_EMAIL, contrasena: ADMIN_PASSWORD }),
    { headers: { 'Content-Type': 'application/json' } });
  check(sesion, { 'inicio de sesión aceptado': (r) => r.status === 200 });
  const token = sesion.json('token');

  const r = http.get(`${BASE_URL}/catalogo/v1/tienda/productos?tamano=100`);
  check(r, { 'catálogo disponible': (x) => x.status === 200 });
  const productos = r.json('items');
  const variantes = productos.flatMap((p) => p.variantes.map((v) => v.varianteId)).slice(0, VARIANTES_A_COMPRAR);

  // Stock online grande en las variantes que se compran, para que la prueba mida el sistema y no el stock agotado.
  for (const varianteId of variantes) {
    const ingreso = http.post(`${BASE_URL}/inventario/v1/ingresos`,
      JSON.stringify({ varianteId, canal: 'ONLINE', cantidad: 1000000 }),
      { headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}`, 'Idempotency-Key': `pico-${varianteId}-${Date.now()}` } });
    check(ingreso, { 'ingreso de stock aceptado': (x) => x.status < 300 });
  }
  return {
    ids: productos.map((p) => p.productoId),
    categorias: [...new Set(productos.map((p) => p.categoria))],
    variantes,
  };
}

const alAzar = (lista) => lista[Math.floor(Math.random() * lista.length)];
const etiqueta = (operacion) => ({ tags: { operacion } });

export function verFicha(d) {
  const r = http.get(`${BASE_URL}/catalogo/v1/tienda/productos/${alAzar(d.ids)}`, etiqueta('verFicha'));
  check(r, { 'ficha 200': (x) => x.status === 200 });
}

export function verListado(d) {
  const r = http.get(`${BASE_URL}/catalogo/v1/tienda/productos?categoria=${alAzar(d.categorias)}&tamano=24`, etiqueta('verListado'));
  check(r, { 'listado 200': (x) => x.status === 200 });
}

export function buscar() {
  const r = http.get(`${BASE_URL}/catalogo/v1/tienda/productos?q=${alAzar(PALABRAS)}&orden=precio_asc&tamano=24`, etiqueta('buscar'));
  check(r, { 'búsqueda 200': (x) => x.status === 200 });
}

export function verDisponibles(d) {
  const r = http.get(`${BASE_URL}/inventario/v1/disponibilidad?variantes=${d.variantes.slice(0, 4).join(',')}`, etiqueta('verDisponibles'));
  check(r, { 'disponibilidad 200': (x) => x.status === 200 });
}

export function comprar(d) {
  // La mitad compra la variante más pedida; el resto, cualquiera de las demás.
  const popular = Math.random() < 0.5;
  const varianteId = popular ? d.variantes[0] : alAzar(d.variantes.slice(1));
  const clave = `pico-${__VU}-${__ITER}-${Date.now()}`;
  const r = http.post(`${BASE_URL}/ventas/v1/compras`,
    JSON.stringify({ email: `carga-${__VU}@fav.example`, medioPago: 'TRANSFERENCIA', items: [{ varianteId, cantidad: 1 }] }),
    { headers: { 'Content-Type': 'application/json', 'Idempotency-Key': clave },
      tags: { operacion: 'comprar', variante: popular ? 'popular' : 'otra' } });
  check(r, { 'compra 201': (x) => x.status === 201 });
}
