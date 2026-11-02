"""Exporta catalogo.ipynb (ya ejecutado) a catalogo.html: una página oscura, con una pestaña por indicador.

El notebook no se modifica ni se vuelve a ejecutar: solo se leen sus celdas y sus salidas.
Uso, desde tp-mongo/analisis/ (las dependencias son las de requirements.txt):
  .venv\\Scripts\\python exportar_html.py        (en Linux/Mac: .venv/bin/python)
"""

import html
import re
from pathlib import Path

import mistune
import nbformat
from pygments import highlight
from pygments.formatters import HtmlFormatter
from pygments.lexers import PythonLexer

AQUI = Path(__file__).resolve().parent
NOTEBOOK = AQUI / "catalogo.ipynb"
SALIDA = AQUI / "catalogo.html"

# (id de la pestaña, texto del botón). El orden es el de la página.
PESTANIAS = [
    ("resumen", "Resumen"),
    ("datos", "Datos"),
    ("ind1", "1 · Precio"),
    ("ind2", "2 · Oferta"),
    ("ind3", "3 · Promociones"),
    ("ind4", "4 · Talles y colores"),
    ("conclusion", "Conclusión"),
]

markdown = mistune.create_markdown(escape=False, plugins=["table", "strikethrough"])
resaltador = HtmlFormatter(noclasses=True, style="monokai", nowrap=True)


def repartir_en_pestanias(celdas):
    """Asigna cada celda a una pestaña según los títulos del notebook. Devuelve {pestaña: [bloques]}."""
    pestanias = {id_: [] for id_, _ in PESTANIAS}
    actual = "resumen"
    for celda in celdas:
        fuente = celda.source
        if celda.cell_type == "markdown":
            if re.match(r"##\s+1\.", fuente):
                actual = "datos"
            elif re.match(r"##\s+4\.", fuente):
                actual = "conclusion"
            if actual == "resumen" and "FAV es una tienda" in fuente:
                fuente = fuente[fuente.index("FAV es una tienda"):]  # el título y los autores ya están en la cabecera
            titulo = re.search(r"^###\s+Indicador\s+(\d)", fuente, re.M)
            if titulo:
                # La celda puede traer antes el título "## 3. Indicadores": se descarta, queda la pestaña.
                fuente = fuente[titulo.start():]
                actual = f"ind{titulo.group(1)}"
            pestanias[actual].append(("md", fuente))
        elif celda.cell_type == "code" and fuente.strip() != "cliente.close()":
            pestanias[actual].append(("codigo", celda))
    return pestanias


def indice_en_la_esquina(tabla):
    """pandas agrega una segunda fila de encabezado solo con el nombre del índice ("promocion"). Se la lleva
    a la esquina de la primera, que es donde corresponde, y se quita la fila suelta."""
    patron = (r"<thead>\s*<tr[^>]*>\s*<th></th>(.*?)</tr>\s*<tr>\s*<th>([^<]+)</th>"
              r"(?:\s*<th></th>)+\s*</tr>\s*</thead>")
    return re.sub(patron, r'<thead><tr><th class="indice">\2</th>\1</tr></thead>', tabla, flags=re.S)


def salidas_html(celda):
    partes = []
    for salida in celda.get("outputs", []):
        tipo = salida.output_type
        if tipo == "stream":
            partes.append(f'<pre class="salida">{html.escape(salida.text)}</pre>')
        elif tipo in ("display_data", "execute_result"):
            datos = salida.data
            if "image/png" in datos:
                partes.append(f'<figure><img alt="Gráfico" src="data:image/png;base64,{datos["image/png"].strip()}"></figure>')
            elif "text/html" in datos:
                tabla = re.sub(r"<style.*?</style>", "", datos["text/html"], flags=re.S)
                tabla = indice_en_la_esquina(tabla)
                partes.append(f'<div class="tabla">{tabla}</div>')
            elif "text/plain" in datos:
                partes.append(f'<pre class="salida">{html.escape(datos["text/plain"])}</pre>')
        elif tipo == "error":
            partes.append(f'<pre class="salida error">{html.escape(salida.ename)}: {html.escape(salida.evalue)}</pre>')
    return "\n".join(partes)


def bloque_html(tipo, contenido):
    if tipo == "md":
        return f'<div class="texto">{markdown(contenido)}</div>'
    codigo = highlight(contenido.source, PythonLexer(), resaltador)
    return (
        f'<details class="codigo"><summary>Ver código</summary><pre>{codigo}</pre></details>\n'
        f"{salidas_html(contenido)}"
    )


PLANTILLA = """<!DOCTYPE html>
<html lang="es">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Análisis del catálogo de FAV</title>
<style>
  :root {
    --fondo: #0d1117; --tarjeta: #161b22; --borde: #30363d; --texto: #e6edf3; --tenue: #8b949e;
    --acento: #58a6ff; --acento-2: #f0883e; --verde: #3fb950;
  }
  * { box-sizing: border-box; }
  body { margin: 0; background: var(--fondo); color: var(--texto); line-height: 1.6;
         font: 16px/1.6 system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; }
  header { padding: 40px 16px 24px; border-bottom: 1px solid var(--borde);
           background: linear-gradient(180deg, #11182a 0%, var(--fondo) 100%); }
  .ancho { max-width: 1040px; margin: 0 auto; padding: 0 16px; }
  header .ancho { padding: 0; }
  header h1 { margin: 0 0 4px; font-size: clamp(1.6rem, 4vw, 2.2rem); letter-spacing: -.02em; }
  header p { margin: 0; color: var(--tenue); }
  .autores { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 16px; }
  .autores span { background: var(--tarjeta); border: 1px solid var(--borde); border-radius: 999px;
                  padding: 4px 14px; font-size: .9rem; }
  .autores b { color: var(--acento); font-weight: 600; }
  nav { position: sticky; top: 0; z-index: 5; background: rgba(13, 17, 23, .92);
        backdrop-filter: blur(8px); border-bottom: 1px solid var(--borde); }
  nav .ancho { display: flex; gap: 4px; overflow-x: auto; padding-top: 8px; padding-bottom: 0; }
  nav button { appearance: none; background: none; border: 0; border-bottom: 3px solid transparent; color: var(--tenue);
               font: inherit; font-size: .95rem; padding: 10px 14px; cursor: pointer; white-space: nowrap; }
  nav button:hover { color: var(--texto); }
  nav button[aria-selected="true"] { color: var(--texto); border-bottom-color: var(--acento); font-weight: 600; }
  nav button:focus-visible { outline: 2px solid var(--acento); outline-offset: -2px; border-radius: 6px; }
  main { padding: 28px 0 64px; }
  section[role="tabpanel"] { display: none; }
  section[role="tabpanel"].activa { display: block; }
  .texto { background: var(--tarjeta); border: 1px solid var(--borde); border-radius: 12px; padding: 4px 24px; margin: 0 0 16px; }
  .texto h1, .texto h2, .texto h3 { line-height: 1.25; letter-spacing: -.01em; }
  .texto h1 { font-size: 1.6rem; } .texto h2 { font-size: 1.35rem; } .texto h3 { font-size: 1.2rem; color: var(--acento); }
  .texto a { color: var(--acento); }
  .texto code { background: #1f2630; border-radius: 4px; padding: 1px 6px; font-size: .88em; }
  .texto strong { color: #fff; }
  .texto table { border-collapse: collapse; margin: 12px 0; }
  .texto th, .texto td { border: 1px solid var(--borde); padding: 6px 12px; }
  figure { margin: 0 0 16px; background: var(--tarjeta); border: 1px solid var(--borde); border-radius: 12px;
           padding: 12px; text-align: center; }
  figure img { max-width: 100%; height: auto; border-radius: 6px; }
  .tabla { margin: 0 0 16px; overflow-x: auto; background: var(--tarjeta); border: 1px solid var(--borde);
           border-radius: 12px; padding: 8px; }
  .tabla table { border-collapse: collapse; font-size: .88rem; width: 100%; }
  .tabla th, .tabla td { padding: 6px 12px; text-align: right; border-bottom: 1px solid var(--borde); }
  .tabla thead th { color: var(--tenue); font-weight: 600; }
  .tabla thead th.indice { text-align: left; }
  .tabla tbody th { text-align: left; color: var(--texto); }
  .tabla tbody tr:hover { background: #1f2630; }
  pre.salida { background: var(--tarjeta); border: 1px solid var(--borde); border-radius: 12px; padding: 12px 16px;
               overflow-x: auto; color: var(--tenue); font-size: .85rem; margin: 0 0 16px; }
  pre.error { color: #ff7b72; }
  details.codigo { margin: 0 0 12px; }
  details.codigo summary { cursor: pointer; color: var(--tenue); font-size: .85rem; padding: 4px 0; width: fit-content; }
  details.codigo summary:hover { color: var(--acento); }
  details.codigo pre { background: #1c2128; border: 1px solid var(--borde); border-radius: 10px; padding: 14px 16px;
                       overflow-x: auto; font-size: .82rem; line-height: 1.5; margin: 8px 0 0; }
  footer { border-top: 1px solid var(--borde); color: var(--tenue); font-size: .85rem; padding: 20px 0 40px; text-align: center; }
  @media (max-width: 600px) { .texto { padding: 2px 16px; } nav button { padding: 10px 10px; } }
  @media print { nav { display: none; } section[role="tabpanel"] { display: block !important; } }
</style>
</head>
<body>
<header>
  <div class="ancho">
    <h1>Análisis del catálogo de FAV</h1>
    <p>Trabajo Práctico Integrador · Base de Datos Aplicada · consigna 5.7</p>
    <div class="autores">
      <span><b>Jonathan Brua</b> · 2212370</span>
      <span><b>Juan Pablo Perciavalle</b> · 2510559</span>
    </div>
  </div>
</header>
<nav aria-label="Secciones del análisis"><div class="ancho" role="tablist">
@@BOTONES@@
</div></nav>
<main><div class="ancho">
@@PANELES@@
</div></main>
<footer>Generado a partir de <code>catalogo.ipynb</code> con <code>exportar_html.py</code>. Datos: MongoDB, base <code>catalogo</code>.</footer>
<script>
  const botones = [...document.querySelectorAll('[role="tab"]')];
  const paneles = [...document.querySelectorAll('[role="tabpanel"]')];
  function mostrar(id, enfocar) {
    botones.forEach((b) => b.setAttribute('aria-selected', String(b.dataset.id === id)));
    paneles.forEach((p) => p.classList.toggle('activa', p.id === id));
    if (history.replaceState) history.replaceState(null, '', '#tab-' + id);  // con prefijo, para que el navegador no salte al panel
    window.scrollTo({ top: 0 });
    if (enfocar) botones.find((b) => b.dataset.id === id).focus();
  }
  botones.forEach((b, i) => {
    b.addEventListener('click', () => mostrar(b.dataset.id));
    b.addEventListener('keydown', (e) => {
      const paso = { ArrowRight: 1, ArrowLeft: -1 }[e.key];
      if (paso) { e.preventDefault(); mostrar(botones[(i + paso + botones.length) % botones.length].dataset.id, true); }
    });
  });
  const inicial = location.hash.replace('#tab-', '');
  mostrar(botones.some((b) => b.dataset.id === inicial) ? inicial : botones[0].dataset.id);
</script>
</body>
</html>
"""


def main():
    nb = nbformat.read(NOTEBOOK, as_version=4)
    pestanias = repartir_en_pestanias(nb.cells)

    botones = "\n".join(
        f'<button role="tab" data-id="{id_}" aria-controls="{id_}" aria-selected="false">{html.escape(texto)}</button>'
        for id_, texto in PESTANIAS
    )
    paneles = "\n".join(
        f'<section role="tabpanel" id="{id_}">\n' + "\n".join(bloque_html(*b) for b in pestanias[id_]) + "\n</section>"
        for id_, _ in PESTANIAS
    )
    SALIDA.write_text(PLANTILLA.replace("@@BOTONES@@", botones).replace("@@PANELES@@", paneles), encoding="utf-8")
    print(f"{SALIDA.name}: {SALIDA.stat().st_size / 1024:,.0f} KB, " + ", ".join(f"{i}={len(b)}" for i, b in pestanias.items()))


if __name__ == "__main__":
    main()
