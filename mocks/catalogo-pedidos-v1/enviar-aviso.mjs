// Envía un aviso pedidoActualizado firmado a cualquier URL, sin hacer un pedido.
// Sirve para probar el receptor del grupo: firma, deduplicación, frescura y reintentos.
//
//   node enviar-aviso.mjs --url http://localhost:9000/webhooks/fav [opciones]
//
//   --estado PAGADO|VENCIDO|A_VERIFICAR   (por defecto PAGADO)
//   --pedido <uuid>                        (por defecto el del ejemplo del contrato)
//   --referencia <texto>                   referenciaExterna
//   --secreto <texto>                      (por defecto secreto-de-prueba)
//   --duplicado                            envía el mismo aviso dos veces (mismo eventoId)
//   --firma-invalida                       firma con otro secreto: el receptor debe responder 401
//   --viejo                                firma de hace 10 minutos: el receptor debe responder 401
import { parseArgs } from 'node:util';
import { enviarAviso, nuevoEvento } from './avisos.mjs';

const { values: a } = parseArgs({
  options: {
    url: { type: 'string' },
    estado: { type: 'string', default: 'PAGADO' },
    pedido: { type: 'string', default: '7d9f2c1e-4b3a-4e8f-9c2d-1a0b3c4d5e6f' },
    referencia: { type: 'string', default: 'OC-2026-0457' },
    secreto: { type: 'string', default: 'secreto-de-prueba' },
    duplicado: { type: 'boolean', default: false },
    'firma-invalida': { type: 'boolean', default: false },
    viejo: { type: 'boolean', default: false },
  },
});

if (!a.url || !['PAGADO', 'VENCIDO', 'A_VERIFICAR'].includes(a.estado.toUpperCase())) {
  console.error('Uso: node enviar-aviso.mjs --url <url> [--estado PAGADO|VENCIDO|A_VERIFICAR] [--duplicado] [--firma-invalida] [--viejo]');
  process.exit(2);
}

const evento = nuevoEvento({ pedidoId: a.pedido, referenciaExterna: a.referencia, estado: a.estado.toUpperCase() });
console.log('Cuerpo enviado:', JSON.stringify(evento));
const opciones = { secreto: a.secreto, firmaInvalida: a['firma-invalida'], atrasoSegundos: a.viejo ? 600 : 0 };
const r = await enviarAviso(a.url, evento, opciones);
if (a.duplicado && r.entregado) await enviarAviso(a.url, evento, opciones);
process.exit(r.entregado || r.status === 401 ? 0 : 1);
