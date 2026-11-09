// Compara Elasticsearch y Solr en lo que usa la búsqueda de FAV (ADR-006): mismo catálogo, mismo análisis de texto y mismos
// casos. Levanta un Solr temporal y un índice temporal en Elasticsearch, y los borra al terminar. No toca el índice de la
// tienda. Requiere el sistema levantado (docker compose up), Node y Docker.
//
//   node services/catalogo/pruebas/comparar_solr.mjs
import { execSync } from 'node:child_process';

const ES = 'http://localhost:9200';
const SOLR = 'http://localhost:8983/solr';
const CONTENEDOR = 'fav-comparacion-solr';
const docker = (comando) => execSync(`docker ${comando}`, { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
const esperar = (ms) => new Promise((listo) => setTimeout(listo, ms));

async function pedir(url, { metodo = 'GET', cuerpo } = {}) {
  const respuesta = await fetch(url, {
    method: metodo,
    headers: cuerpo ? { 'Content-Type': 'application/json' } : {},
    body: cuerpo ? JSON.stringify(cuerpo) : undefined,
  });
  const texto = await respuesta.text();
  let datos;
  try { datos = JSON.parse(texto); } catch { datos = texto; }
  return { codigo: respuesta.status, datos };
}
async function exigir(url, opciones) {
  const r = await pedir(url, opciones);
  if (r.codigo >= 300) throw new Error(`${url}: ${r.codigo} ${JSON.stringify(r.datos).slice(0, 500)}`);
  return r.datos;
}

// ---------- Elasticsearch: usa la configuración real del índice de la tienda ----------
async function crearIndiceEs(nombre, documentos) {
  const vivo = Object.values(await exigir(`${ES}/productos`))[0];
  await exigir(`${ES}/${nombre}`, {
    metodo: 'PUT',
    cuerpo: {
      settings: { number_of_shards: 1, number_of_replicas: 0, analysis: vivo.settings.index.analysis },
      mappings: vivo.mappings,
    },
  });
  for (const d of documentos) await indexarEs(nombre, d, 1);
  await exigir(`${ES}/${nombre}/_refresh`, { metodo: 'POST' });
}
const indexarEs = (indice, d, version) =>
  pedir(`${ES}/${indice}/_doc/${encodeURIComponent(d.productoId)}?version_type=external&version=${version}&refresh=true`, { metodo: 'PUT', cuerpo: d });

// La misma consulta que internal/busqueda/cliente.go.
async function buscarEs(f, indice = 'comparacion') {
  const filtros = [];
  const variante = [];
  if (f.talle) variante.push({ term: { 'variantes.talle': f.talle } });
  if (f.color) variante.push({ term: { 'variantes.color': f.color } });
  if (variante.length) filtros.push({ nested: { path: 'variantes', query: { bool: { filter: variante } } } });
  const consulta = {
    from: f.desde ?? 0, size: f.tamano ?? 100, _source: false,
    sort: f.orden === 'precio' ? [{ precio: 'asc' }, { productoId: 'asc' }] : ['_score', { productoId: 'asc' }],
    query: { bool: {
      must: [{ match: { texto: { query: f.q, operator: 'and', fuzziness: 'AUTO' } } }],
      should: [{ match_phrase: { nombre: { query: f.q, boost: 3 } } }],
      filter: filtros,
    } },
  };
  const r = await exigir(`${ES}/${indice}/_search`, { metodo: 'POST', cuerpo: consulta });
  return r.hits.hits.map((h) => h._id);
}

// ---------- Solr: lo más parecido que se puede armar ----------
async function crearNucleoSolr(nucleo, documentos) {
  await exigir(`${SOLR}/${nucleo}/schema`, {
    metodo: 'POST',
    cuerpo: {
      'add-field-type': [
        { name: 'texto_es', class: 'solr.TextField', positionIncrementGap: '100', analyzer: {
          tokenizer: { class: 'solr.StandardTokenizerFactory' },
          filters: [
            { class: 'solr.LowerCaseFilterFactory' },
            { class: 'solr.ASCIIFoldingFilterFactory' },
            { class: 'solr.StopFilterFactory', words: 'lang/stopwords_es.txt', format: 'snowball', ignoreCase: 'true' },
            { class: 'solr.SpanishLightStemFilterFactory' },
          ],
        } },
        { name: 'texto_sin_raiz', class: 'solr.TextField', positionIncrementGap: '100', analyzer: {
          tokenizer: { class: 'solr.StandardTokenizerFactory' },
          filters: [{ class: 'solr.LowerCaseFilterFactory' }, { class: 'solr.ASCIIFoldingFilterFactory' }],
        } },
        { name: 'sin_mayusculas', class: 'solr.TextField', analyzer: {
          tokenizer: { class: 'solr.KeywordTokenizerFactory' },
          filters: [{ class: 'solr.LowerCaseFilterFactory' }, { class: 'solr.ASCIIFoldingFilterFactory' }],
        } },
      ],
      'add-field': [
        { name: 'doc_tipo', type: 'string' },
        { name: 'nombre', type: 'texto_es' },
        { name: 'descripcion', type: 'texto_es' },
        { name: 'colores', type: 'texto_es', multiValued: true },
        { name: 'texto', type: 'texto_es', multiValued: true, stored: false },
        { name: 'texto_difuso', type: 'texto_sin_raiz', multiValued: true, stored: false },
        { name: 'categoria', type: 'string' },
        { name: 'precio', type: 'plong' },
        { name: 'creadoEn', type: 'pdate' },
        { name: 'version_externa', type: 'plong' },
        { name: 'talle', type: 'string' },
        { name: 'color', type: 'sin_mayusculas' },
      ],
      'add-copy-field': ['nombre', 'descripcion', 'colores'].flatMap((source) => [{ source, dest: 'texto' }, { source, dest: 'texto_difuso' }]),
    },
  });
  await exigir(`${SOLR}/${nucleo}/config`, {
    metodo: 'POST',
    cuerpo: { 'add-updateprocessor': {
      name: 'versionado', class: 'solr.DocBasedVersionConstraintsProcessorFactory', versionField: 'version_externa', ignoreOldUpdates: false,
    } },
  });
  await exigir(`${SOLR}/${nucleo}/update?commit=true`, { metodo: 'POST', cuerpo: documentos.map((d) => aSolr(d, 1)) });
}
const aSolr = (d, version) => ({
  id: d.productoId, doc_tipo: 'producto', nombre: d.nombre, descripcion: d.descripcion, colores: d.colores,
  categoria: d.categoria, precio: d.precio, creadoEn: d.creadoEn, version_externa: version,
  _childDocuments_: d.variantes.map((v, i) => ({ id: `${d.productoId}#${i}`, doc_tipo: 'variante', talle: v.talle, color: v.color })),
});

// Solr no tiene fuzziness AUTO: se calcula por palabra, con la misma regla. Un término difuso tampoco pasa por el análisis, así
// que no coincidiría con la raíz guardada ("remra" contra "remer"): se busca en un campo aparte, guardado sin raíz.
function difusa(palabra) {
  const n = palabra.length;
  const distancia = n <= 2 ? 0 : n <= 5 ? 1 : 2;
  return distancia === 0 ? `texto:${palabra}` : `(texto:${palabra} texto_difuso:${palabra}~${distancia})`;
}
async function buscarSolr(f, nucleo = 'fav') {
  const palabras = f.q.toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '').split(/\s+/);
  const q = `${palabras.map((p) => `+${difusa(p)}`).join(' ')} nombre:"${f.q}"^3`;
  const parametros = new URLSearchParams({
    q, fl: 'id', wt: 'json', start: f.desde ?? 0, rows: f.tamano ?? 100, 'q.op': 'OR', df: 'texto',
    sort: f.orden === 'precio' ? 'precio asc,id asc' : 'score desc,id asc',
  });
  parametros.append('fq', 'doc_tipo:producto');
  const hijo = [];
  if (f.talle) hijo.push(`+talle:${f.talle}`);
  if (f.color) hijo.push(`+color:${f.color.toLowerCase()}`);
  if (hijo.length) parametros.append('fq', `{!parent which="doc_tipo:producto"}${hijo.join(' ')}`);
  const r = await exigir(`${SOLR}/${nucleo}/select?${parametros}`);
  return r.response.docs.map((d) => d.id);
}

// ---------- Casos ----------
const resultados = [];
function anotar(caso, es, solr, detalle = '') {
  resultados.push({ caso, es, solr, detalle });
  console.log(`${es ? 'OK   ' : 'FALLA'} ${solr ? 'OK   ' : 'FALLA'} ${caso}${detalle ? `  (${detalle})` : ''}`);
}

const CRUZADA = {
  productoId: 'prueba-cruzada', nombre: 'Camisa cruzada', descripcion: 'Prenda de prueba.', colores: ['Blanco', 'Negro'],
  categoria: 'camisas', precio: 500000, creadoEn: new Date().toISOString(),
  variantes: [{ talle: 'M', color: 'Blanco' }, { talle: 'L', color: 'Negro' }],
};

async function casosDeTexto() {
  const casos = [
    ['Plural encuentra singular: "remeras"', { q: 'remeras' }, (ids) => ids.includes('prod-remera-basica')],
    ['Sin acento encuentra con acento: "algodon"', { q: 'algodon' }, (ids) => ids.includes('prod-remera-basica')],
    ['Con acento de más: "rémera"', { q: 'rémera' }, (ids) => ids.includes('prod-remera-basica')],
    ['Error de tipeo: "remra"', { q: 'remra' }, (ids) => ids.includes('prod-remera-basica')],
    ['Dos errores en palabra larga: "constalasion"', { q: 'constalasion' }, (ids) => ids.length > 0],
    ['Todas las palabras: "remera algodon"', { q: 'remera algodon' }, (ids) => ids.includes('prod-remera-basica')],
    ['Sin coincidencia de todas: "remera zzzzzzz"', { q: 'remera zzzzzzz' }, (ids) => ids.length === 0],
    ['Busca en colores: "bordo"', { q: 'bordo' }, (ids) => ids.length > 0],
    ['Talle y color en la misma variante: M Blanco incluye', { q: 'camisa', talle: 'M', color: 'Blanco' }, (ids) => ids.includes('prueba-cruzada')],
    ['Talle y color en distinta variante: M Negro excluye', { q: 'camisa', talle: 'M', color: 'Negro' }, (ids) => !ids.includes('prueba-cruzada')],
  ];
  for (const [caso, filtro, correcto] of casos) {
    const [es, solr] = [await buscarEs(filtro), await buscarSolr(filtro)];
    anotar(caso, correcto(es), correcto(solr), `${es.length} y ${solr.length} resultados`);
  }
}

async function casoDeOrden() {
  const f = { q: 'remera', orden: 'precio' };
  const [es, solr] = [await buscarEs(f), await buscarSolr(f)];
  anotar('Orden por precio y desempate: mismo orden en los dos', JSON.stringify(es) === JSON.stringify(solr), JSON.stringify(es) === JSON.stringify(solr), `${es.length} resultados`);
  const paginas = async (buscar) => {
    const vistos = [];
    for (let desde = 0; desde < 40; desde += 5) vistos.push(...await buscar({ ...f, desde, tamano: 5 }));
    return vistos.length === new Set(vistos).size;
  };
  anotar('Paginación sin repetidos', await paginas(buscarEs), await paginas(buscarSolr));
}

async function casoDeRelevancia() {
  const f = { q: 'remera basica' };
  const [es, solr] = [await buscarEs(f), await buscarSolr(f)];
  anotar('Coincidencia exacta en el nombre primero: "remera basica"', es[0] === 'prod-remera-basica', solr[0] === 'prod-remera-basica', `primeros: ${es[0]} y ${solr[0]}`);
}

async function casoDeVersion() {
  const nueva = { ...CRUZADA, nombre: 'Camisa versión nueva' };
  const vieja = { ...CRUZADA, nombre: 'Camisa versión vieja' };
  const es5 = await indexarEs('comparacion', nueva, 5);
  const es3 = await indexarEs('comparacion', vieja, 3);
  const esNombre = (await exigir(`${ES}/comparacion/_doc/prueba-cruzada`))._source.nombre;
  const url = `${SOLR}/fav/update?commit=true&processor=versionado`;
  const solr5 = await pedir(url, { metodo: 'POST', cuerpo: [aSolr(nueva, 5)] });
  const solr3 = await pedir(url, { metodo: 'POST', cuerpo: [aSolr(vieja, 3)] });
  const solrDoc = await exigir(`${SOLR}/fav/select?q=id:prueba-cruzada&fl=nombre,version_externa`);
  const solrNombre = solrDoc.response.docs[0]?.nombre;
  const detalle = `Solr: nueva ${solr5.codigo}, vieja ${solr3.codigo}, quedó "${solrNombre}"`;
  if (solr5.codigo >= 300) console.log('   ', JSON.stringify(solr5.datos).slice(0, 400));
  anotar('Un evento viejo no pisa un dato nuevo', es5.codigo < 300 && es3.codigo === 409 && esNombre === nueva.nombre,
    solr5.codigo < 300 && solr3.codigo === 409 && solrNombre === nueva.nombre, detalle);

  // Sacar una variante no puede dejar una huérfana que siga coincidiendo con el filtro.
  const sinNegro = { ...nueva, colores: ['Blanco'], variantes: [{ talle: 'M', color: 'Blanco' }] };
  await indexarEs('comparacion', sinNegro, 6);
  const solr6 = await pedir(url, { metodo: 'POST', cuerpo: [aSolr(sinNegro, 6)] });
  const f = { q: 'camisa', talle: 'L', color: 'Negro' };
  const [es, solr] = [await buscarEs(f), await buscarSolr(f)];
  anotar('Editar una prenda quita la variante eliminada', !es.includes('prueba-cruzada'), solr6.codigo < 300 && !solr.includes('prueba-cruzada'), `Solr: ${solr6.codigo}`);
}

// Consultas continuas mientras se cambia al índice nuevo.
async function consultarDurante(buscar, cambiar) {
  let fallidas = 0;
  let total = 0;
  let seguir = true;
  const consultas = (async () => {
    while (seguir) {
      total++;
      try { if (!(await buscar({ q: 'remera' })).length) fallidas++; } catch { fallidas++; }
    }
  })();
  await esperar(500);
  await cambiar();
  await esperar(1500);
  seguir = false;
  await consultas;
  return { fallidas, total };
}

async function casoDeReconstruccion(documentos) {
  await exigir(`${ES}/_aliases`, { metodo: 'POST', cuerpo: { actions: [{ add: { index: 'comparacion-a', alias: 'comparacion' } }] } });
  await crearIndiceEs('comparacion-b', documentos);
  const es = await consultarDurante((f) => buscarEs(f), () => exigir(`${ES}/_aliases`, {
    metodo: 'POST',
    cuerpo: { actions: [{ remove: { index: 'comparacion-a', alias: 'comparacion' } }, { add: { index: 'comparacion-b', alias: 'comparacion' } }] },
  }));
  docker(`exec ${CONTENEDOR} solr create -c fav-b`);
  await crearNucleoSolr('fav-b', documentos);
  const solr = await consultarDurante((f) => buscarSolr(f), () => exigir(`${SOLR}/admin/cores?action=SWAP&core=fav&other=fav-b`));
  anotar('Reconstrucción sin cortar la búsqueda', es.fallidas === 0, solr.fallidas === 0,
    `fallidas: ${es.fallidas} de ${es.total} y ${solr.fallidas} de ${solr.total}`);
}

async function medirLatencia() {
  const consultas = ['remera', 'remeras', 'algodon', 'remra', 'remera basica', 'jean', 'abanico'];
  const medir = async (buscar) => {
    const tiempos = [];
    for (let i = 0; i < 700; i++) {
      const inicio = performance.now();
      await buscar({ q: consultas[i % consultas.length] });
      tiempos.push(performance.now() - inicio);
    }
    tiempos.sort((a, b) => a - b);
    return `p50 ${tiempos[350].toFixed(1)} ms, p95 ${tiempos[665].toFixed(1)} ms`;
  };
  await medir((f) => buscarEs(f));
  await medir((f) => buscarSolr(f));
  console.log(`\nLatencia, 700 consultas una tras otra, ya en caliente:\n  Elasticsearch: ${await medir((f) => buscarEs(f))}\n  Solr:          ${await medir((f) => buscarSolr(f))}`);
}

function memoria() {
  const leer = (nombre) => docker(`stats --no-stream --format "{{.MemUsage}}" ${nombre}`);
  const es = execSync('docker compose ps -q elasticsearch', { encoding: 'utf8' }).trim();
  console.log(`\nMemoria en reposo, heap de 512 MB en los dos:\n  Elasticsearch: ${leer(es)}\n  Solr:          ${leer(CONTENEDOR)}`);
}

// ---------- Programa ----------
async function limpiar() {
  for (const indice of ['comparacion-a', 'comparacion-b']) await pedir(`${ES}/${indice}`, { metodo: 'DELETE' }).catch(() => {});
  try { docker(`rm -f ${CONTENEDOR}`); } catch { /* no estaba */ }
}

await limpiar();
try {
  const respuesta = await exigir(`${ES}/productos/_search?size=1000`);
  const documentos = [...respuesta.hits.hits.map((h) => h._source), CRUZADA];
  console.log(`Catálogo: ${documentos.length} productos, copiados del índice de la tienda más uno de prueba.\n`);

  const inicio = Date.now();
  docker(`run -d --name ${CONTENEDOR} -p 127.0.0.1:8983:8983 -e SOLR_HEAP=512m solr:9.7.0 solr-precreate fav`);
  for (;;) {
    const r = await pedir(`${SOLR}/fav/admin/ping`).catch(() => null);
    if (r?.codigo === 200) break;
    if (Date.now() - inicio > 120_000) throw new Error('Solr no arrancó en 2 minutos');
    await esperar(500);
  }
  console.log(`Solr listo a los ${((Date.now() - inicio) / 1000).toFixed(1)} s.\n`);

  await crearIndiceEs('comparacion-a', documentos);
  await exigir(`${ES}/_aliases`, { metodo: 'POST', cuerpo: { actions: [{ add: { index: 'comparacion-a', alias: 'comparacion' } }] } });
  await crearNucleoSolr('fav', documentos);

  console.log('ES    Solr');
  await casosDeTexto();
  await casoDeOrden();
  await casoDeRelevancia();
  await casoDeReconstruccion(documentos);
  await casoDeVersion();
  await medirLatencia();
  await esperar(5000);
  memoria();

  const fallasEs = resultados.filter((r) => !r.es).length;
  const fallasSolr = resultados.filter((r) => !r.solr).length;
  console.log(`\nResultado: ${resultados.length} casos. Fallas: Elasticsearch ${fallasEs}, Solr ${fallasSolr}.`);
} finally {
  await limpiar();
}
