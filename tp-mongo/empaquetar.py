"""Arma el zip de la entrega: tp-mongo/dist/catalogo-mongo.zip (PROYECTO.md sección 10).

Contenido: services/catalogo/ + tp-mongo/ + README.md en la raíz del zip. Es lo mínimo para que
`docker compose up` funcione desde cero: el Compose construye la imagen desde ../services/catalogo.
Deja afuera el .env (credenciales), el repositorio git, entornos virtuales y archivos temporales, y
verifica que no se haya colado ningún secreto.

Uso (desde cualquier carpeta; solo biblioteca estándar, sirve en Windows, macOS y Linux):
  python tp-mongo/empaquetar.py
"""

import re
import sys
import zipfile
from pathlib import Path

TP = Path(__file__).resolve().parent
RAIZ = TP.parent
SALIDA = TP / "dist" / "catalogo-mongo.zip"

# Carpetas y archivos que no se entregan. Se comparan contra cada parte de la ruta.
CARPETAS_EXCLUIDAS = {".git", ".venv", "__pycache__", ".ipynb_checkpoints", "dist", "node_modules"}
# CLAUDE.md son instrucciones para el asistente de código; la consigna es del profesor y no hace falta devolverla.
ARCHIVOS_EXCLUIDOS = {".env", ".DS_Store", "Thumbs.db", "CLAUDE.md", "Consigna_Proyecto_NoSQL_MongoDB.pdf"}
SUFIJOS_EXCLUIDOS = {".zip", ".pyc", ".exe", ".test", ".out"}

# Archivos sin los que la entrega no funciona. Se comprueba al final.
OBLIGATORIOS = [
    "README.md",
    "services/catalogo/Dockerfile",
    "services/catalogo/go.mod",
    "services/catalogo/go.sum",
    "services/catalogo/cmd/catalogo/main.go",
    "tp-mongo/docker-compose.yml",
    "tp-mongo/.env.example",
    "tp-mongo/api/openapi.yaml",
    "tp-mongo/scripts/01_crear_colecciones_e_indices.js",
    "tp-mongo/scripts/02_cargar.sh",
    "tp-mongo/scripts/03_verificar.js",
    "tp-mongo/scripts/cargar.sh",
    "tp-mongo/datos/productos.json",
    "tp-mongo/consultas/consultas.js",
    "tp-mongo/analisis/catalogo.ipynb",
    "tp-mongo/analisis/catalogo.html",
    "tp-mongo/informe/00-resumen.md",
]


def incluido(ruta: Path) -> bool:
    partes = ruta.relative_to(RAIZ).parts
    return not (
        any(p in CARPETAS_EXCLUIDAS for p in partes)
        or ruta.name in ARCHIVOS_EXCLUIDOS
        or ruta.suffix in SUFIJOS_EXCLUIDOS
    )


def archivos():
    """(ruta en disco, ruta dentro del zip), en orden."""
    for carpeta in (RAIZ / "services" / "catalogo", TP):
        for ruta in sorted(carpeta.rglob("*")):
            if ruta.is_file() and incluido(ruta):
                yield ruta, ruta.relative_to(RAIZ).as_posix()
    yield TP / "README.md", "README.md"  # el README de la raíz del zip es el de tp-mongo


def valores_secretos():
    """Claves del .env local que no sean las de ejemplo: si aparecen en lo entregado, es un error."""
    def leer(archivo):
        valores = {}
        if archivo.exists():
            for linea in archivo.read_text(encoding="utf-8").splitlines():
                nombre, igual, valor = linea.partition("=")
                if igual and not linea.lstrip().startswith("#"):
                    valores[nombre.strip()] = valor.strip()
        return valores

    real, ejemplo = leer(TP / ".env"), leer(TP / ".env.example")
    return {v for k, v in real.items() if "PASSWORD" in k and v and v != ejemplo.get(k)}


def readme_de_la_raiz(disco: Path) -> str:
    """Los enlaces del README son relativos a tp-mongo/; en la raíz del zip hay que anteponer la carpeta."""
    texto = disco.read_text(encoding="utf-8")
    return re.sub(r"\]\((?!https?:|#|tp-mongo/)([^)\s]+)\)", r"](tp-mongo/\1)", texto)


def main() -> int:
    secretos = valores_secretos()
    problemas = []
    entradas = []

    for disco, destino in archivos():
        entradas.append((disco, destino))
        if disco.suffix in {".md", ".js", ".sh", ".py", ".go", ".yml", ".yaml", ".json", ".txt", ".html", ".ipynb", ".example", ""}:
            try:
                texto = disco.read_text(encoding="utf-8")
            except (UnicodeDecodeError, OSError):
                continue
            for secreto in secretos:
                if secreto in texto:
                    problemas.append(f"{destino}: contiene la clave de tu .env")
            if re.search(r"mongodb(\+srv)?://[^\s:@/]+:[^\s@/]+@", texto) and "USUARIO:CLAVE" not in texto and "$" not in texto:
                problemas.append(f"{destino}: tiene una URI de MongoDB con usuario y clave")

    faltan = [o for o in OBLIGATORIOS if o not in {d for _, d in entradas}]
    problemas += [f"falta {o}" for o in faltan]
    if problemas:
        print("NO SE GENERÓ EL ZIP:")
        for p in problemas:
            print(f"  - {p}")
        return 1

    SALIDA.parent.mkdir(exist_ok=True)
    SALIDA.unlink(missing_ok=True)
    with zipfile.ZipFile(SALIDA, "w", zipfile.ZIP_DEFLATED) as z:
        for disco, destino in entradas:
            if destino == "README.md":
                z.writestr(destino, readme_de_la_raiz(disco))
            else:
                z.write(disco, destino)  # destino con "/", como espera unzip en cualquier sistema

    tamano = SALIDA.stat().st_size / 1024
    print(f"{SALIDA.relative_to(RAIZ)}: {len(entradas)} archivos, {tamano:,.0f} KB")
    for carpeta in ("README.md", "services/", "tp-mongo/"):
        n = sum(1 for _, d in entradas if d == carpeta or d.startswith(carpeta))
        print(f"  {carpeta:<12} {n} archivos")
    print("Sin .env ni claves reales. Para probar desde cero: descomprimir en otra carpeta y seguir el README.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
