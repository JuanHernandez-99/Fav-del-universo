// Ilustraciones de las prendas sin foto: dos por categoría. Cada capa es [tipo, trazo SVG] en un lienzo de 200×210.
// "base" se pinta del color de la variante; "sombra", "luz", "linea" y "costura" van encima, translúcidas, así sirven
// para cualquier color.

const REMERA = 'M72 28c5 9 13 13 28 13s23-4 28-13l34 15 20 44-27 13-10-20v104H55V80l-10 20-27-13 20-44z';
const BUZO = 'M70 30c6 6 16 9 30 9s24-3 30-9l30 14 28 112-24 6-24-84v104H56V78l-24 84-24-6 28-112z';
const PANTALON = 'M64 22h72l13 166h-37l-12-108-12 108H51z';
const CORTO = 'M60 54h80l16 86h-41l-15-50-15 50H44z';
const circulo = (x, y, r) => `M${x - r} ${y}a${r} ${r} 0 1 0 ${2 * r} 0a${r} ${r} 0 1 0 ${-2 * r} 0z`;
const verticales = (desde, hasta, paso, y, alto) => {
  let d = '';
  for (let x = desde; x <= hasta; x += paso) d += `M${x} ${y}v${alto}`;
  return d;
};

export const ILUSTRACIONES = {
  remeras: [
    [ // cuello redondo
      ['base', REMERA],
      ['sombra', 'M130 62h15v122h-15z'],
      ['sombra', 'M78 28c4 6 11 9 22 9s18-3 22-9c-5 4-12 6-22 6s-17-2-22-6z'],
      ['linea', 'M72 28c5 9 13 13 28 13s23-4 28-13M58 40l-3 40M142 40l3 40'],
      ['costura', 'M58 177h84M24 83l25 12M176 83l-25 12'],
    ],
    [ // escote en V con bolsillo
      ['base', 'M74 28l26 30 26-30 36 15 20 44-27 13-10-20v104H55V80l-10 20-27-13 20-44z'],
      ['sombra', 'M80 28l20 23 20-23z'],
      ['linea', 'M74 28l26 30 26-30M58 40l-3 40M142 40l3 40M112 78h20v22h-20z'],
      ['costura', 'M114 82h16M58 177h84M24 83l25 12M176 83l-25 12'],
    ],
  ],
  tops: [
    [ // musculosa
      ['base', 'M70 24h14c2 18 8 26 16 26s14-8 16-26h14c0 22 8 36 20 46v114H50V70c12-10 20-24 20-46z'],
      ['sombra', 'M134 76c4 0 10-2 16-6v114h-16z'],
      ['linea', 'M84 24c2 18 8 26 16 26s14-8 16-26M70 24c0 22-8 36-20 46M130 24c0 22 8 36 20 46'],
      ['costura', 'M54 177h92'],
    ],
    [ // top de tiritas
      ['base', 'M74 30h5v42h-5zM121 30h5v42h-5z'],
      ['base', 'M62 70c14-6 24 6 38 6s24-12 38-6l6 74c-28 8-60 8-88 0z'],
      ['sombra', 'M58 132c28 8 56 8 84 0l2 12c-28 8-60 8-88 0z'],
      ['luz', 'M70 80c8 2 14 4 20 4l-6 40c-6 0-12-2-18-4z'],
      ['linea', 'M62 70c14-6 24 6 38 6s24-12 38-6'],
      ['costura', 'M64 78c12-4 22 4 36 4s24-8 36-4'],
    ],
  ],
  bodys: [
    [ // manga larga
      ['base', 'M74 26c4 8 12 12 26 12s22-4 26-12l30 12 26 104-22 6-16-80v62c-14 8-26 20-36 36H92c-10-16-22-28-36-36V68l-16 80-22-6 26-104z'],
      ['sombra', 'M130 70h14v60c-4 2-9 5-14 9z'],
      ['linea', 'M74 26c4 8 12 12 26 12s22-4 26-12M60 38l-4 30M140 38l4 30M56 130c14 8 26 20 36 36M144 130c-14 8-26 20-36 36'],
      ['costura', 'M20 134l22 6M180 134l-22 6'],
    ],
    [ // halter sin mangas
      ['base', 'M86 22h28l4 20c10 6 22 12 26 26v62c-14 8-26 20-36 36H92c-10-16-22-28-36-36V68c4-14 16-20 26-26z'],
      ['sombra', 'M86 22h28v9H86z'],
      ['luz', 'M66 74c6-10 14-16 22-20l-4 60c-8-4-14-10-18-14z'],
      ['linea', 'M82 42c6 8 12 12 18 12s12-4 18-12M56 130c14 8 26 20 36 36M144 130c-14 8-26 20-36 36'],
    ],
  ],
  buzos: [
    [ // canguro con capucha
      ['base', 'M66 34c0-20 16-30 34-30s34 10 34 30c-8 8-20 12-34 12s-26-4-34-12z'],
      ['sombra', 'M66 34c0-20 16-30 34-30s34 10 34 30c-8 8-20 12-34 12s-26-4-34-12z'],
      ['base', BUZO],
      ['sombra', 'M56 170h84v12H56zM188 156l-24 6-3-11 24-6zM8 156l24 6 3-11-24-6z'],
      ['sombra', 'M72 132h56l8 30H64z'],
      ['linea', 'M72 132h56l8 30H64zM92 44v26M108 44v26M76 33l-20 45M124 33l16 45'],
    ],
    [ // cuello redondo con franja
      ['base', BUZO],
      ['luz', 'M56 108h84v14H56z'],
      ['sombra', 'M56 170h84v12H56zM188 156l-24 6-3-11 24-6zM8 156l24 6 3-11-24-6z'],
      ['linea', 'M70 30c6 6 16 9 30 9s24-3 30-9M76 33l-20 45M124 33l16 45'],
      ['costura', 'M74 36c8 5 16 7 26 7s18-2 26-7'],
    ],
  ],
  camperas: [
    [ // con cierre
      ['base', BUZO],
      ['sombra', 'M70 30l30 14 30-14-4-8c-6 6-14 8-26 8s-20-2-26-8z'],
      ['sombra', 'M56 170h84v12H56zM188 156l-24 6-3-11 24-6zM8 156l24 6 3-11-24-6z'],
      ['linea', 'M100 40v142M66 132l14-22M134 132l-14-22M76 33l-20 45M124 33l16 45'],
      ['costura', 'M104 44v126'],
    ],
    [ // de jean con solapas y botones
      ['base', 'M72 26l28 18 28-18 34 16 26 116-24 6-22-86v106H58V78l-22 86-24-6 26-116z'],
      ['luz', 'M72 26l28 18-10 30-24-38z'],
      ['sombra', 'M128 26l-28 18 10 30 24-38z'],
      ['linea', 'M100 44v140M66 92h22v18H66zM112 92h22v18h-22z'],
      ['costura', 'M58 172h84M64 140h72'],
      ['sombra', `${circulo(100, 96, 3)}${circulo(100, 124, 3)}${circulo(100, 152, 3)}`],
    ],
  ],
  jeans: [
    [ // recto
      ['base', PANTALON],
      ['sombra', 'M64 22h72v10H64z'],
      ['sombra', 'M72 22h4v13h-4zM98 22h4v13h-4zM124 22h4v13h-4z'],
      ['linea', 'M68 34c10 2 16 10 18 20M132 34c-10 2-16 10-18 20'],
      ['costura', 'M108 32v30c0 6-4 10-8 10M53 178h35M112 178h35'],
      ['sombra', circulo(100, 27, 3)],
    ],
    [ // oxford
      ['base', 'M66 22h68l6 70c4 30 14 64 26 96h-48l-18-100-18 100H34c12-32 22-66 26-96z'],
      ['sombra', 'M66 22h68v10H66z'],
      ['luz', 'M64 112a7 18 0 1 0 14 0a7 18 0 1 0-14 0zM122 112a7 18 0 1 0 14 0a7 18 0 1 0-14 0z'],
      ['linea', 'M70 34c10 2 16 10 18 20M130 34c-10 2-16 10-18 20'],
      ['costura', 'M108 32v30c0 6-4 10-8 10M36 180h46M118 180h46'],
    ],
  ],
  pantalones: [
    [ // sastrero con pinzas
      ['base', PANTALON],
      ['sombra', 'M64 22h72v10H64z'],
      ['linea', 'M84 32v36M116 32v36M66 34l8 22M134 34l-8 22'],
      ['costura', 'M82 72l-12 112M118 72l12 112'],
      ['luz', circulo(100, 27, 2.5)],
    ],
    [ // jogger
      ['base', 'M62 22h76l8 150c0 6-4 10-10 10h-28l-8-96-8 96H64c-6 0-10-4-10-10z'],
      ['sombra', 'M62 22h76v14H62zM55 168h41v14H64c-6 0-9-4-9-8zM104 168h41v6c0 4-3 8-9 8h-32z'],
      ['luz', 'M64 40l-6 126h4l6-126zM136 40l6 126h-4l-6-126z'],
      ['linea', 'M96 36c-2 10-6 16-10 20M104 36c2 10 6 16 10 20'],
      ['costura', verticales(68, 132, 8, 24, 10)],
    ],
  ],
  shorts: [
    [ // de jean
      ['base', CORTO],
      ['sombra', 'M60 54h80v9H60z'],
      ['luz', 'M45 128h42l-2 12H44zM113 128h42l1 12h-41z'],
      ['linea', 'M64 64c10 2 15 8 17 16M136 64c-10 2-15 8-17 16'],
      ['costura', 'M108 63v20c0 4-4 7-8 7'],
    ],
    [ // deportivo
      ['base', 'M58 50h84l14 78c-12 6-26 8-40 6l-16-38-16 38c-14 2-28 0-40-6z'],
      ['sombra', 'M58 50h84v12H58z'],
      ['luz', 'M150 96l6 32c-4 2-8 3-12 4zM50 96l-6 32c4 2 8 3 12 4z'],
      ['linea', 'M96 62c-2 8-4 12-8 16M104 62c2 8 4 12 8 16'],
      ['costura', 'M47 122c12 5 24 7 36 6M153 122c-12 5-24 7-36 6'],
    ],
  ],
  polleras: [
    [ // tableada
      ['base', 'M70 44h60l38 132H32z'],
      ['sombra', 'M70 44h60v10H70z'],
      ['sombra', 'M92 54l-10 122h12zM116 54l8 122h12zM76 54l-26 122h10z'],
      ['linea', 'M84 54l-14 122M100 54v122M116 54l14 122M130 54l26 122'],
    ],
    [ // tubo con tajo
      ['base', 'M70 40h60l6 70-4 70H68l-4-70z'],
      ['sombra', 'M70 40h60v10H70z'],
      ['sombra', 'M106 180l-4-34-4 34z'],
      ['luz', 'M74 60c4 0 6 2 8 4l-2 80c-4 0-8-2-10-4z'],
      ['linea', 'M84 50l2 22M116 50l-2 22M102 146v34'],
      ['costura', 'M68 176h64'],
    ],
  ],
  vestidos: [
    [ // con cinturón
      ['base', 'M78 22c4 8 11 11 22 11s18-3 22-11l14 8-8 50 34 108H30L64 80l-8-50z'],
      ['sombra', 'M65 74h70l1 10H64z'],
      ['luz', 'M96 75h8v8h-8z'],
      ['linea', 'M78 22c4 8 11 11 22 11s18-3 22-11M86 92l-16 92M114 92l16 92M100 92v94'],
    ],
    [ // largo con mangas cortas
      ['base', 'M74 24c5 7 13 10 26 10s21-3 26-10l28 14 14 32-20 9-12-18-4 30 26 100H42l26-100-4-30-12 18-20-9 14-32z'],
      ['sombra', 'M120 91h12l26 100h-18z'],
      ['linea', 'M74 24c5 7 13 10 26 10s21-3 26-10M88 91l-18 100M112 91l18 100'],
      ['costura', 'M68 91h64M34 66l20 9M166 66l-20 9'],
      ['luz', `${circulo(100, 48, 2.5)}${circulo(100, 62, 2.5)}${circulo(100, 76, 2.5)}`],
    ],
  ],
  conjuntos: [
    [ // remera corta y pollera
      ['base', 'M74 22c4 7 12 10 26 10s22-3 26-10l28 12 14 30-20 9-10-16v40H66V57l-10 16-20-9 14-30z'],
      ['base', 'M66 108h68l24 82H42z'],
      ['sombra', 'M66 108h68v8H66z'],
      ['linea', 'M74 22c4 7 12 10 26 10s22-3 26-10M86 116l-12 74M114 116l12 74'],
      ['costura', 'M68 92h64'],
    ],
    [ // buzo corto y jogger
      ['base', 'M72 20c6 5 16 8 28 8s22-3 28-8l26 12 22 70-20 6-18-50v38H62V58l-18 50-20-6 22-70z'],
      ['base', 'M66 104h68l6 86h-30l-10-56-10 56H60z'],
      ['sombra', 'M62 86h76v10H62zM66 104h68v8H66zM60 180h30v10H60zM110 180h30v10h-30z'],
      ['linea', 'M72 20c6 5 16 8 28 8s22-3 28-8M96 112c-2 6-4 10-8 12M104 112c2 6 4 10 8 12'],
    ],
  ],
  abanicos: [
    [ // abierto
      ['base', 'M95 156h10v38H95z'],
      ['base', 'M100 160L13 110A100 100 0 0 1 187 110z'],
      ['sombra', 'M100 160L29 89A100 100 0 0 1 50 73zM100 160L74 63A100 100 0 0 1 100 60zM100 160L126 63A100 100 0 0 1 150 73zM100 160L171 89A100 100 0 0 1 187 110z'],
      ['luz', 'M100 160L71 143A34 34 0 0 1 129 143z'],
      ['linea', 'M100 160L29 89M100 160L50 73M100 160L74 63M100 160L100 60M100 160L126 63M100 160L150 73M100 160L171 89'],
      ['sombra', circulo(100, 158, 4)],
    ],
    [ // de encaje
      ['base', 'M94 170h12v30H94z'],
      ['base', 'M100 175L15 90A120 120 0 0 1 185 90z'],
      ['sombra', 'M100 175L54 64A120 120 0 0 1 100 55zM100 175L146 64A120 120 0 0 1 185 90z'],
      ['luz', `${circulo(48, 84, 5)}${circulo(73, 74, 5)}${circulo(100, 70, 5)}${circulo(127, 74, 5)}${circulo(152, 84, 5)}`],
      ['linea', 'M100 175L54 64M100 175L100 55M100 175L146 64'],
      ['costura', 'M30 98A100 100 0 0 1 170 98'],
    ],
  ],
  accesorios: [
    [ // gorro con pompón
      ['base', circulo(100, 40, 14)],
      ['base', 'M50 130c0-50 22-84 50-84s50 34 50 84z'],
      ['base', 'M44 126h112v28H44z'],
      ['sombra', 'M44 142h112v12H44z'],
      ['luz', 'M70 70c6-10 14-16 22-18-8 14-12 34-12 74H66c0-22 0-40 4-56z'],
      ['linea', verticales(56, 144, 12, 128, 24)],
    ],
    [ // bolso
      ['base', 'M70 80c0-40 60-40 60 0h-8c0-30-44-30-44 0z'],
      ['base', 'M48 80h104l10 104H38z'],
      ['sombra', 'M48 80h104l-6 34H54z'],
      ['luz', circulo(100, 114, 6)],
      ['costura', 'M44 176h112'],
    ],
  ],
};

// La misma prenda siempre usa la misma ilustración; entre prendas de una categoría se alternan.
export function ilustracionDe(categoria, semilla = '') {
  const opciones = ILUSTRACIONES[categoria] ?? ILUSTRACIONES.remeras;
  let n = 0;
  for (const letra of String(semilla)) n = (n * 31 + letra.charCodeAt(0)) % 1000003;
  return opciones[n % opciones.length];
}

// Sobre un color oscuro las costuras se dibujan claras, para que se vean.
export function esTonoOscuro(tono) {
  const hex = /^#([0-9a-f]{6})$/i.exec(tono);
  if (!hex) return false; // los colores no reconocidos se generan claros (dominio.colorDeMuestra)
  const [r, g, b] = [0, 2, 4].map((i) => parseInt(hex[1].slice(i, i + 2), 16) / 255);
  return 0.2126 * r + 0.7152 * g + 0.0722 * b < 0.35;
}
