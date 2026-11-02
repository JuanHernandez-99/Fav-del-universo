"""Genera los datos de prueba del catálogo (PROYECTO.md sección 8).

Determinista: con la misma semilla produce siempre los mismos archivos. Escribe Extended JSON
($numberLong, $date), que es el formato que entiende mongoimport.

Archivos:
  categorias.json         las 13 categorías (RN-17)
  productos_minimo.json   los 2 productos del contrato, con los ids que ya tienen stock en Inventario
  productos.json          el volumen del TP (250 productos)
  promociones.json        vigentes, vencidas y futuras respecto de la defensa (03/11/2026)

Uso, desde tp-mongo/:  python datos/generar.py
"""

import json
import random
import re
import unicodedata
from datetime import datetime, timedelta, timezone
from pathlib import Path

SEMILLA = 20261103
SALIDA = Path(__file__).parent
ARGENTINA = timezone(timedelta(hours=-3))

TALLES = {
    "LETRAS": ["XS", "S", "M", "L", "XL", "XXL"],
    "NUMERICO_PAR": ["32", "34", "36", "38", "40", "42", "44", "46"],
    "NUMERICO_1_4": ["1", "2", "3", "4"],
    "UNICO": ["U"],
}

COLORES = ["Negro", "Blanco", "Gris", "Beige", "Azul", "Celeste", "Azul marino", "Rojo", "Bordó",
           "Rosa", "Lila", "Verde", "Verde oliva", "Marrón", "Amarillo", "Natural"]

# Por categoría: nombre, cantidad de productos, sistemas de talles con su peso, rango de precio en
# pesos, colores, materiales, tipos de prenda (con su género gramatical) y descriptores.
# Un descriptor es un texto o un par (femenino, masculino). `fijos` son nombres que siempre se
# incluyen para cubrir casos límite (tildes y ñ).
CATEGORIAS = {
    "tops": dict(
        nombre="Tops", cantidad=20, sistemas={"LETRAS": 8, "NUMERICO_1_4": 2}, precio=(9000, 22000),
        materiales=["algodón", "lino", "modal", "raso", "encaje"],
        tipos=[("Top", "m"), ("Top corto", "m"), ("Top de breteles", "m"), ("Top halter", "m"),
               ("Top strapless", "m")],
        descriptores=["liso", "con lazo", "de lino", "acanalado", "de encaje", "estampado", "de raso",
                      "con volados"]),
    "bodys": dict(
        nombre="Bodys", cantidad=14, sistemas={"LETRAS": 8, "NUMERICO_1_4": 2}, precio=(10000, 24000),
        materiales=["modal", "algodón", "encaje", "lycra"],
        tipos=[("Body", "m"), ("Body manga larga", "m"), ("Body escote cuadrado", "m"),
               ("Body de tiras", "m")],
        descriptores=["liso", "de encaje", "acanalado", "de modal", "con transparencias", "drapeado"]),
    "remeras": dict(
        nombre="Remeras", cantidad=30, sistemas={"LETRAS": 3, "NUMERICO_1_4": 1}, precio=(8000, 25000),
        materiales=["algodón", "lino", "modal", "algodón peinado"],
        tipos=[("Remera", "f"), ("Remera oversize", "f"), ("Remera manga larga", "f"),
               ("Remera cuello V", "f"), ("Musculosa", "f")],
        descriptores=["básica", "lisa", "rayada", "estampada", "con bolsillo", "de lino", "acanalada",
                      "con nudo", "con frase", "de algodón peinado"]),
    "buzos": dict(
        nombre="Buzos", cantidad=20, sistemas={"LETRAS": 85, "NUMERICO_1_4": 15}, precio=(25000, 60000),
        materiales=["frisa", "algodón", "rústico"],
        tipos=[("Buzo", "m"), ("Buzo con capucha", "m"), ("Buzo oversize", "m"),
               ("Buzo cuello redondo", "m"), ("Canguro", "m")],
        descriptores=["liso", "de frisa", "rústico", "bordado", "estampado", "con cierre", "de algodón",
                      "universitario"]),
    "camperas": dict(
        nombre="Camperas", cantidad=18, sistemas={"LETRAS": 1}, precio=(40000, 120000),
        materiales=["paño", "cuero ecológico", "nylon", "gabardina", "jean"],
        tipos=[("Campera", "f"), ("Tapado", "m"), ("Saco", "m"), ("Chaleco", "m"), ("Parka", "f")],
        descriptores=[("corta", "corto"), ("larga", "largo"), "de paño", "con capucha",
                      "de cuero ecológico", ("acolchada", "acolchado"), "oversize",
                      ("entallada", "entallado")],
        fijos=["Tapado de paño"]),
    "jeans": dict(
        nombre="Jeans", cantidad=26, sistemas={"NUMERICO_PAR": 1}, precio=(30000, 65000),
        colores=["Celeste", "Azul", "Negro", "Gris", "Blanco", "Azul oscuro"],
        materiales=["denim"],
        tipos=[("Jean mom", "m"), ("Jean recto", "m"), ("Jean wide leg", "m"), ("Jean skinny", "m"),
               ("Jean cargo", "m"), ("Jean oxford", "m")],
        descriptores=["clásico", "tiro alto", "con roturas", "nevado", "con tachas", "elastizado",
                      "rígido", "con dobladillo"]),
    "pantalones": dict(
        nombre="Pantalones", cantidad=22, sistemas={"NUMERICO_PAR": 7, "LETRAS": 3}, precio=(25000, 55000),
        materiales=["lino", "gabardina", "sastrería", "algodón"],
        tipos=[("Pantalón", "m"), ("Pantalón palazzo", "m"), ("Pantalón cargo", "m"), ("Jogger", "m"),
               ("Pantalón recto", "m")],
        descriptores=["de lino", "de sastrería", "de gabardina", "con pinzas", "de vestir",
                      ("ancha", "ancho"), "tiro alto", "con cordón"]),
    "polleras": dict(
        nombre="Polleras", cantidad=18,
        sistemas={"LETRAS": 5, "NUMERICO_PAR": 3, "NUMERICO_1_4": 2}, precio=(18000, 40000),
        materiales=["jean", "lino", "raso", "gabardina"],
        tipos=[("Pollera", "f"), ("Pollera midi", "f"), ("Pollera larga", "f"), ("Minifalda", "f"),
               ("Falda pantalón", "f")],
        descriptores=["plisada", "de jean", "de lino", "cruzada", "tableada", "con tajo", "de raso",
                      "acampanada"]),
    "shorts": dict(
        nombre="Shorts", cantidad=16, sistemas={"NUMERICO_PAR": 6, "LETRAS": 4}, precio=(15000, 32000),
        materiales=["jean", "lino", "algodón"],
        tipos=[("Short", "m"), ("Short de jean", "m"), ("Bermuda", "f"), ("Short deportivo", "m")],
        descriptores=["de lino", "tiro alto", "con roturas", "con cinto", "de algodón",
                      ("holgada", "holgado"), "con cordón"]),
    "vestidos": dict(
        nombre="Vestidos", cantidad=22, sistemas={"LETRAS": 8, "UNICO": 2}, precio=(25000, 70000),
        materiales=["lino", "raso", "viscosa", "punto"],
        tipos=[("Vestido", "m"), ("Vestido largo", "m"), ("Vestido corto", "m"),
               ("Vestido camisero", "m"), ("Solero", "m")],
        descriptores=["de lino", "floreado", "de fiesta", "con volados", "de raso", "cruzado",
                      "de punto", "con espalda descubierta"]),
    "conjuntos": dict(
        nombre="Conjuntos", cantidad=16, sistemas={"LETRAS": 9, "UNICO": 1}, precio=(35000, 80000),
        materiales=["lino", "plush", "algodón", "frisa"],
        tipos=[("Conjunto", "m"), ("Conjunto deportivo", "m"), ("Conjunto de lino", "m"),
               ("Conjunto de plush", "m")],
        descriptores=["de top y pollera", "de buzo y jogger", "de saco y pantalón",
                      "de remera y short", "de camisa y bermuda", "estampado"]),
    "abanicos": dict(
        nombre="Abanicos", cantidad=10, sistemas={"UNICO": 1}, precio=(5000, 15000),
        colores=["Natural", "Negro", "Rojo", "Blanco", "Dorado"],
        materiales=["madera", "tela", "bambú", "seda", "papel"],
        tipos=[("Abanico", "m")],
        descriptores=["de madera", "de tela", "calado", "pintado a mano", "de bambú", "con encaje",
                      "plegable", "de nácar", "estampado", "de seda", "de papel", "con borla"]),
    "accesorios": dict(
        nombre="Accesorios", cantidad=18, sistemas={"UNICO": 1}, precio=(3000, 20000),
        materiales=["cuero", "lona", "seda", "metal", "lana"],
        tipos=[("Cinto", "m"), ("Gorra", "f"), ("Bolso", "m"), ("Collar", "m"), ("Bufanda", "f"),
               ("Riñonera", "f"), ("Pañuelo", "m")],
        descriptores=["de cuero", ("tejida", "tejido"), "de lona", ("trenzada", "trenzado"),
                      "con tachas", "de seda"],
        fijos=["Riñonera de cuero"]),
}

FRASES = ["Ideal para el día a día.", "Corte relajado.", "Edición limitada.", "Calce cómodo.",
          "Combina con todo.", "Lavar a mano.", "Nueva temporada."]


# ---------- Ids (PROYECTO.md 5.5) ----------

def normalizar(texto):
    """Minúsculas, sin acentos (la ñ queda n) y todo lo que no sea letra o número pasa a '-'.
    La función de Go que genere ids tiene que dar exactamente el mismo resultado."""
    descompuesto = unicodedata.normalize("NFD", texto.lower())
    sin_acentos = "".join(c for c in descompuesto if unicodedata.category(c) != "Mn")
    return re.sub(r"[^a-z0-9]+", "-", sin_acentos).strip("-")


def id_producto(nombre, usados):
    base = "prod-" + normalizar(nombre)
    candidato, n = base, 2
    while candidato in usados:  # sufijo numérico si ya existe [propuesta]
        candidato, n = f"{base}-{n}", n + 1
    usados.add(candidato)
    return candidato


def id_variante(producto_id, color, talle):
    vid = f"var-{producto_id.removeprefix('prod-')}-{normalizar(color)}-{normalizar(talle)}"
    assert len(vid) <= 100, f"id de variante de más de 100 caracteres: {vid}"
    return vid


# ---------- Extended JSON ----------

def fecha(dt):
    return {"$date": dt.astimezone(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")}


def centavos(pesos):
    return {"$numberLong": str(int(pesos * 100))}


def fecha_ar(anio, mes, dia, hora=0, minuto=0, segundo=0):
    """Las vigencias se definen en hora de Argentina y se guardan en UTC [propuesta]."""
    return datetime(anio, mes, dia, hora, minuto, segundo, tzinfo=ARGENTINA)


# ---------- Conjunto mínimo (contrato catalogo-pedidos-v1 y 002_datos_de_ejemplo.sql) ----------

def productos_minimo():
    utc = timezone.utc
    return [
        {
            "_id": "prod-remera-basica",
            "nombre": "Remera básica",
            "descripcion": "Remera de algodón, corte recto.",
            "categoria": "remeras",
            "sistemaTalles": "LETRAS",
            "precioBase": {"monto": centavos(15000), "moneda": "ARS"},
            "publicado": True,
            "atributos": {"material": "algodón", "genero": "unisex"},
            "fotos": [
                {"url": "https://fav.example/fotos/remera-basica.jpg", "color": None},
                {"url": "https://fav.example/fotos/remera-basica-blanca-1.jpg", "color": "Blanco"},
            ],
            # Ids heredados: ya tienen stock en Inventario y no siguen la regla de 5.5 ("blanca").
            "variantes": [
                {"id": "var-remera-basica-blanca-m", "talle": "M", "color": "Blanco"},
                {"id": "var-remera-basica-negra-l", "talle": "L", "color": "Negro"},
            ],
            "creadoEn": fecha(datetime(2026, 10, 1, 12, 0, tzinfo=utc)),
            "actualizadoEn": fecha(datetime(2026, 10, 20, 15, 30, tzinfo=utc)),
            "version": 3,
        },
        {
            # Sin descripción ni fotos, igual que en el ejemplo del contrato.
            "_id": "prod-jean-mom",
            "nombre": "Jean mom",
            "categoria": "jeans",
            "sistemaTalles": "NUMERICO_PAR",
            "precioBase": {"monto": centavos(42000), "moneda": "ARS"},
            "publicado": True,
            "variantes": [
                {"id": "var-jean-mom-celeste-38", "talle": "38", "color": "Celeste"},
            ],
            "creadoEn": fecha(datetime(2026, 10, 1, 12, 0, tzinfo=utc)),
            "actualizadoEn": fecha(datetime(2026, 10, 1, 12, 0, tzinfo=utc)),
            "version": 1,
        },
    ]


# ---------- Volumen ----------

def nombres_de(rng, cfg, cantidad, excluir):
    fijos = [n for n in cfg.get("fijos", []) if n not in excluir]
    combinaciones = []
    for tipo, genero in cfg["tipos"]:
        for d in cfg["descriptores"]:
            palabra = d if isinstance(d, str) else (d[0] if genero == "f" else d[1])
            nombre = f"{tipo} {palabra}"
            if nombre not in excluir and nombre not in fijos:
                combinaciones.append(nombre)
    rng.shuffle(combinaciones)
    return fijos + combinaciones[: cantidad - len(fijos)]


def elegir_talles(rng, sistema):
    talles = TALLES[sistema]
    if sistema == "UNICO":
        return talles
    largo = rng.choice([1, 2, 2, 3])
    inicio = rng.randrange(0, len(talles) - largo + 1)
    return talles[inicio: inicio + largo]


def atributos_de(rng, categoria, cfg, nombre):
    texto = nombre.lower()
    material = next((m for m in cfg["materiales"] if m in texto), rng.choice(cfg["materiales"]))
    if categoria in ("abanicos", "accesorios"):
        genero = "unisex"
    else:
        genero = rng.choices(["mujer", "unisex", "hombre"], weights=[7, 2, 1])[0]
    atributos = {"material": material, "genero": genero}
    # Atributos propios de cada categoría: la parte flexible del modelo (5.3).
    if categoria == "jeans":
        atributos["calce"] = nombre.split()[1] if nombre.split()[1] != "wide" else "wide leg"
    elif categoria in ("pantalones", "shorts"):
        atributos["tiro"] = rng.choice(["alto", "medio", "bajo"])
    elif categoria in ("vestidos", "polleras"):
        atributos["largo"] = rng.choice(["mini", "midi", "largo"])
    elif categoria in ("buzos", "camperas"):
        atributos["abrigo"] = rng.choice(["liviano", "medio", "alto"])
    elif categoria == "accesorios":
        atributos["tipo"] = nombre.split()[0].lower()
    elif categoria == "abanicos":
        atributos["estampado"] = rng.random() < 0.5
    return atributos


def precio_de(rng, minimo, maximo):
    precio = round(rng.uniform(minimo, maximo) / 500) * 500
    return precio - 10 if rng.random() < 0.5 else precio  # precios tipo $24.990


def producto_de(rng, categoria, cfg, nombre, usados):
    pid = id_producto(nombre, usados)
    sistema = rng.choices(list(cfg["sistemas"]), weights=list(cfg["sistemas"].values()))[0]
    colores = rng.sample(cfg.get("colores", COLORES), rng.choices([1, 2, 3], weights=[35, 40, 25])[0])
    talles = elegir_talles(rng, sistema)
    variantes = [{"id": id_variante(pid, c, t), "talle": t, "color": c} for c in colores for t in talles]

    slug = pid.removeprefix("prod-")
    cantidad_fotos = rng.choices([0, 1, 2, 3], weights=[1, 4, 3, 2])[0]
    fotos = [{"url": f"https://fav.example/fotos/{slug}-{i + 1}.jpg",
              "color": None if i == 0 else rng.choice(colores)} for i in range(cantidad_fotos)]

    creado = datetime(2026, 3, 1, tzinfo=timezone.utc) + timedelta(minutes=rng.randrange(0, 237 * 24 * 60))
    version = rng.choices([1, 2, 3, 4, 5], weights=[5, 3, 2, 1, 1])[0]
    actualizado = creado if version == 1 else min(
        creado + timedelta(minutes=rng.randrange(60, 20 * 24 * 60)),
        datetime(2026, 10, 31, 23, 0, tzinfo=timezone.utc))

    producto = {
        "_id": pid,
        "nombre": nombre,
        "categoria": categoria,
        "sistemaTalles": sistema,
        "precioBase": {"monto": centavos(precio_de(rng, *cfg["precio"])), "moneda": "ARS"},
        "publicado": rng.random() < 0.85,
        "variantes": variantes,
        "creadoEn": fecha(creado),
        "actualizadoEn": fecha(actualizado),
        "version": version,
    }
    atributos = atributos_de(rng, categoria, cfg, nombre)
    if rng.random() < 0.85:
        producto["descripcion"] = f"Prenda de {atributos['material']}. {rng.choice(FRASES)}"
    if rng.random() < 0.9:
        producto["atributos"] = atributos
    if fotos:
        producto["fotos"] = fotos
    return producto


def productos_volumen(rng, minimo):
    usados = {p["_id"] for p in minimo}
    excluir = {p["nombre"] for p in minimo}
    productos = []
    for categoria, cfg in CATEGORIAS.items():
        for nombre in nombres_de(rng, cfg, cfg["cantidad"], excluir):
            productos.append(producto_de(rng, categoria, cfg, nombre, usados))

    # Casos límite (sección 8), elegidos de forma determinista.
    sin_nada = next(p for p in productos if p["categoria"] == "tops")
    for campo in ("descripcion", "atributos", "fotos"):
        sin_nada.pop(campo, None)

    muchas = next(p for p in productos if p["categoria"] == "remeras" and p["sistemaTalles"] == "LETRAS")
    colores = ["Negro", "Blanco", "Gris", "Rosa"]
    muchas["variantes"] = [{"id": id_variante(muchas["_id"], c, t), "talle": t, "color": c}
                           for c in colores for t in TALLES["LETRAS"]]
    for i, foto in enumerate(muchas.get("fotos", [])[1:]):  # que las fotos usen colores que existen
        foto["color"] = colores[i % len(colores)]

    mas_barato = next(p for p in productos if p["categoria"] == "accesorios")
    mas_barato["precioBase"]["monto"] = centavos(1000)
    mas_caro = next(p for p in productos if p["categoria"] == "camperas")
    mas_caro["precioBase"]["monto"] = centavos(180000)
    return productos


# ---------- Promociones ----------

def promociones(rng, productos):
    def elegir(categoria):
        return rng.choice([p["_id"] for p in productos if p["categoria"] == categoria and p["publicado"]])

    # Un jean entra en "Semana del jean" (20%) y en la liquidación (30%): gana la mayor (D3).
    liquidacion = [elegir("jeans"), elegir("remeras"), elegir("vestidos")]

    def promo(pid, nombre, tipo, porcentaje, desde, hasta, alcance, ids=(), medio=None):
        return {"_id": pid, "nombre": nombre, "tipo": tipo, "porcentaje": porcentaje, "medioPago": medio,
                "vigencia": {"desde": fecha(desde), "hasta": fecha(hasta)},
                "alcance": {"tipo": alcance, "ids": list(ids)}}

    fin = (23, 59, 59)
    return [
        # Vigentes el día de la defensa (03/11/2026).
        promo("promo-efectivo-tienda", "10% pagando en efectivo", "MEDIO_PAGO", 10,
              fecha_ar(2026, 11, 1), fecha_ar(2026, 11, 30, *fin), "TIENDA", medio="EFECTIVO"),
        promo("promo-mercado-pago", "5% con Mercado Pago", "MEDIO_PAGO", 5,
              fecha_ar(2026, 10, 1), fecha_ar(2026, 12, 31, *fin), "TIENDA", medio="MERCADO_PAGO"),
        promo("promo-semana-del-jean", "Semana del jean", "PORCENTAJE", 20,
              fecha_ar(2026, 11, 1), fecha_ar(2026, 11, 15, *fin), "CATEGORIA", ["jeans"]),
        promo("promo-media-estacion", "Buzos y camperas de media estación", "PORCENTAJE", 15,
              fecha_ar(2026, 10, 15), fecha_ar(2026, 11, 10, *fin), "CATEGORIA", ["buzos", "camperas"]),
        promo("promo-liquidacion", "Liquidación de productos seleccionados", "PORCENTAJE", 30,
              fecha_ar(2026, 10, 20), fecha_ar(2026, 11, 20, *fin), "PRODUCTO", liquidacion),
        promo("promo-accesorios", "Semana de accesorios", "PORCENTAJE", 25,
              fecha_ar(2026, 11, 2), fecha_ar(2026, 11, 8, *fin), "CATEGORIA", ["accesorios", "abanicos"]),
        # Vencidas.
        promo("promo-hot-sale", "Hot Sale", "PORCENTAJE", 25,
              fecha_ar(2026, 5, 11), fecha_ar(2026, 5, 13, *fin), "TIENDA"),
        promo("promo-invierno-camperas", "Invierno en camperas", "PORCENTAJE", 20,
              fecha_ar(2026, 6, 1), fecha_ar(2026, 7, 31, *fin), "CATEGORIA", ["camperas"]),
        promo("promo-transferencia-septiembre", "10% por transferencia en septiembre", "MEDIO_PAGO", 10,
              fecha_ar(2026, 9, 1), fecha_ar(2026, 9, 30, *fin), "TIENDA", medio="TRANSFERENCIA"),
        promo("promo-dia-de-la-madre", "Día de la Madre", "PORCENTAJE", 15,
              fecha_ar(2026, 10, 10), fecha_ar(2026, 10, 18, *fin), "CATEGORIA", ["vestidos", "conjuntos"]),
        # Futuras.
        promo("promo-black-friday", "Black Friday", "PORCENTAJE", 25,
              fecha_ar(2026, 11, 27), fecha_ar(2026, 11, 30, *fin), "TIENDA"),
        promo("promo-verano-remeras", "Verano en remeras", "PORCENTAJE", 20,
              fecha_ar(2026, 12, 1), fecha_ar(2026, 12, 31, *fin), "CATEGORIA", ["remeras"]),
        promo("promo-navidad", "Navidad en vestidos y conjuntos", "PORCENTAJE", 15,
              fecha_ar(2026, 12, 15), fecha_ar(2026, 12, 24, *fin), "CATEGORIA", ["vestidos", "conjuntos"]),
        promo("promo-transferencia-enero", "10% por transferencia en enero", "MEDIO_PAGO", 10,
              fecha_ar(2027, 1, 2), fecha_ar(2027, 1, 31, *fin), "TIENDA", medio="TRANSFERENCIA"),
    ]


# ---------- Salida ----------

def escribir(nombre, documentos):
    with open(SALIDA / nombre, "w", encoding="utf-8", newline="\n") as f:
        json.dump(documentos, f, ensure_ascii=False, indent=2)
        f.write("\n")
    print(f"{nombre}: {len(documentos)} documentos")


def main():
    rng = random.Random(SEMILLA)
    categorias = [{"_id": cid, "nombre": cfg["nombre"]} for cid, cfg in CATEGORIAS.items()]
    minimo = productos_minimo()
    volumen = productos_volumen(rng, minimo)
    promos = promociones(rng, volumen)

    ids = [v["id"] for p in minimo + volumen for v in p["variantes"]]
    assert len(ids) == len(set(ids)), "hay ids de variante repetidos"

    escribir("categorias.json", categorias)
    escribir("productos_minimo.json", minimo)
    escribir("productos.json", volumen)
    escribir("promociones.json", promos)
    print(f"variantes: {len(ids)}")


if __name__ == "__main__":
    main()
