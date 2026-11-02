"""Prueba de concurrencia del caso de consistencia (PROYECTO.md 5.9).

20 pedidos simultáneos intentan agregar la misma variante (S / Blanco) a un producto nuevo.
Resultado esperado: exactamente un 201, diecinueve 409, el producto con una sola variante S / Blanco
y la versión subida una sola vez. El resultado se guarda en evidencia/concurrencia_variante.txt.

Uso, con la API levantada (solo usa la biblioteca estándar de Python):
  python pruebas/concurrencia_variante.py [url_base]      # por defecto http://localhost:8085
"""

import json
import sys
import threading
import urllib.error
import urllib.request
from collections import Counter
from datetime import datetime
from pathlib import Path

BASE = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:8085"
PEDIDOS = 20
VARIANTE = {"talle": "S", "color": "Blanco"}
EVIDENCIA = Path(__file__).resolve().parent.parent / "evidencia" / "concurrencia_variante.txt"


def pedir(metodo, ruta, cuerpo=None):
    datos = json.dumps(cuerpo).encode("utf-8") if cuerpo is not None else None
    pedido = urllib.request.Request(BASE + ruta, data=datos, method=metodo,
                                    headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(pedido, timeout=10) as r:
            return r.status, json.loads(r.read() or b"null")
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read() or b"null")


def main():
    lineas = []

    def anotar(texto=""):
        print(texto)
        lineas.append(texto)

    # 1. Un producto nuevo, que nace con una sola variante (M / Blanco).
    estado, producto = pedir("POST", "/v1/admin/productos", {
        "nombre": "Remera prueba de concurrencia", "categoria": "remeras", "sistemaTalles": "LETRAS",
        "precioBase": {"monto": 12000}, "variantes": [{"talle": "M", "color": "Blanco"}],
    })
    if estado != 201:
        sys.exit(f"no se pudo crear el producto de prueba: {estado} {producto}")
    pid = producto["productoId"]

    anotar(f"Prueba de concurrencia · {datetime.now():%Y-%m-%d %H:%M:%S} · {BASE}")
    anotar(f"Producto: {pid} (version {producto['version']}, {len(producto['variantes'])} variante)")
    anotar(f"{PEDIDOS} pedidos simultáneos: POST /v1/admin/productos/{pid}/variantes {json.dumps(VARIANTE)}")

    # 2. Los hilos esperan en la barrera hasta que están todos y salen a la vez.
    barrera = threading.Barrier(PEDIDOS)
    respuestas = [None] * PEDIDOS

    def trabajar(i):
        barrera.wait()
        respuestas[i] = pedir("POST", f"/v1/admin/productos/{pid}/variantes", VARIANTE)

    hilos = [threading.Thread(target=trabajar, args=(i,)) for i in range(PEDIDOS)]
    for h in hilos:
        h.start()
    for h in hilos:
        h.join()

    conteo = Counter(estado for estado, _ in respuestas)
    codigos = Counter(c.get("codigo") for e, c in respuestas if e != 201 and isinstance(c, dict))
    anotar(f"Respuestas: {dict(sorted(conteo.items()))} · códigos de error: {dict(codigos)}")

    # 3. Cómo quedó el producto en la base.
    _, final = pedir("GET", f"/v1/admin/productos/{pid}")
    iguales = [v for v in final["variantes"] if v["talle"] == VARIANTE["talle"] and v["color"] == VARIANTE["color"]]
    anotar(f"Producto final: version {final['version']}, variantes {[v['varianteId'] for v in final['variantes']]}")

    chequeos = {
        "exactamente un 201": conteo[201] == 1,
        f"{PEDIDOS - 1} respuestas 409": conteo[409] == PEDIDOS - 1,
        "una sola variante S / Blanco": len(iguales) == 1,
        "la versión subió una sola vez (1 -> 2)": final["version"] == 2,
    }
    anotar()
    for descripcion, ok in chequeos.items():
        anotar(f"{'OK   ' if ok else 'FALLA'} {descripcion}")
    resultado = "RESULTADO: OK" if all(chequeos.values()) else "RESULTADO: FALLA"
    anotar(resultado)

    EVIDENCIA.parent.mkdir(exist_ok=True)
    EVIDENCIA.write_text("\n".join(lineas) + "\n", encoding="utf-8")
    print(f"(guardado en {EVIDENCIA})")
    sys.exit(0 if all(chequeos.values()) else 1)


if __name__ == "__main__":
    main()
