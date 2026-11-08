// Package busqueda es el único lugar que habla con Elasticsearch (ADR-006). El índice es un modelo de
// lectura derivado: guarda lo necesario para buscar, y el producto se sigue leyendo de MongoDB.
package busqueda

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
)

const (
	aliasPorDefecto = "productos"
	tiempoBusqueda  = 2 * time.Second
	tiempoEscritura = 5 * time.Second
	tiempoCarga     = 2 * time.Minute
	loteCarga       = 500
	// ventanaMaxima es el límite de Elasticsearch para paginar con from y size (index.max_result_window).
	ventanaMaxima = 10000
)

// ErrNoDisponible indica que Elasticsearch no respondió o está fallando: conviene reintentar.
var ErrNoDisponible = errors.New("elasticsearch no está disponible")

type Cliente struct {
	url   string
	alias string
	http  *http.Client
}

// ConAlias usa otro alias: los tests no tocan el índice del servicio.
func ConAlias(urlBase, alias string) *Cliente {
	return &Cliente{url: strings.TrimRight(urlBase, "/"), alias: alias, http: &http.Client{}}
}

func Nuevo(urlBase string) *Cliente {
	return ConAlias(urlBase, aliasPorDefecto)
}

// El analizador en español quita acentos y reduce a la raíz: "básica" y "BASICAS" son lo mismo.
// Sin réplicas porque hay un solo nodo. dynamic strict: un campo no previsto es un error y no un mapeo adivinado.
var configuracion = map[string]any{
	"settings": map[string]any{
		"number_of_shards": 1, "number_of_replicas": 0,
		"analysis": map[string]any{
			"filter": map[string]any{
				"parar_espanol": map[string]any{"type": "stop", "stopwords": "_spanish_"},
				"raiz_espanol":  map[string]any{"type": "stemmer", "language": "light_spanish"},
			},
			"analyzer": map[string]any{
				"espanol": map[string]any{"tokenizer": "standard", "filter": []string{"lowercase", "asciifolding", "parar_espanol", "raiz_espanol"}},
			},
			"normalizer": map[string]any{
				"sin_mayusculas": map[string]any{"type": "custom", "filter": []string{"lowercase", "asciifolding"}},
			},
		},
	},
	"mappings": map[string]any{
		"dynamic": "strict",
		"properties": map[string]any{
			"productoId": map[string]any{"type": "keyword"},
			"nombre": map[string]any{"type": "text", "analyzer": "espanol", "copy_to": "texto", "fields": map[string]any{
				"orden": map[string]any{"type": "keyword", "normalizer": "sin_mayusculas"},
			}},
			"descripcion": map[string]any{"type": "text", "analyzer": "espanol", "copy_to": "texto"},
			"colores":     map[string]any{"type": "text", "analyzer": "espanol", "copy_to": "texto"},
			"texto":       map[string]any{"type": "text", "analyzer": "espanol"},
			"categoria":   map[string]any{"type": "keyword"},
			"precio":      map[string]any{"type": "long"},
			"creadoEn":    map[string]any{"type": "date"},
			// nested: talle y color tienen que coincidir en la misma variante.
			"variantes": map[string]any{"type": "nested", "properties": map[string]any{
				"talle": map[string]any{"type": "keyword"},
				"color": map[string]any{"type": "keyword", "normalizer": "sin_mayusculas"},
			}},
		},
	},
}

type documento struct {
	ProductoID  string              `json:"productoId"`
	Nombre      string              `json:"nombre"`
	Descripcion string              `json:"descripcion,omitempty"`
	Colores     []string            `json:"colores"`
	Categoria   string              `json:"categoria"`
	Precio      int64               `json:"precio"` // centavos, precio base (DISENO.md sección 14, punto 7)
	CreadoEn    time.Time           `json:"creadoEn"`
	Variantes   []varianteDocumento `json:"variantes"`
}

type varianteDocumento struct {
	Talle string `json:"talle"`
	Color string `json:"color"`
}

func aDocumento(p modelo.Producto) documento {
	d := documento{
		ProductoID: p.ID, Nombre: p.Nombre, Descripcion: p.Descripcion, Categoria: p.Categoria,
		Precio: p.PrecioBase.Monto, CreadoEn: p.CreadoEn,
		Colores: []string{}, Variantes: make([]varianteDocumento, len(p.Variantes)),
	}
	vistos := map[string]bool{}
	for i, v := range p.Variantes {
		d.Variantes[i] = varianteDocumento{v.Talle, v.Color}
		if !vistos[v.Color] {
			vistos[v.Color] = true
			d.Colores = append(d.Colores, v.Color)
		}
	}
	return d
}

// AsegurarIndice crea el índice con su alias si no existe. Devuelve true solo a quien lo creó: esa réplica
// lo carga desde MongoDB. Si dos réplicas arrancan juntas, Elasticsearch rechaza la segunda creación.
func (c *Cliente) AsegurarIndice(ctx context.Context) (bool, error) {
	ctx, cancelar := context.WithTimeout(ctx, tiempoEscritura)
	defer cancelar()
	estado, _, err := c.pedir(ctx, http.MethodHead, "/_alias/"+c.alias, nil)
	if err != nil {
		return false, err
	}
	if estado == http.StatusOK {
		return false, nil
	}
	cuerpo := map[string]any{"settings": configuracion["settings"], "mappings": configuracion["mappings"], "aliases": map[string]any{c.alias: map[string]any{}}}
	estado, respuesta, err := c.pedirJSON(ctx, http.MethodPut, "/"+c.alias+"-inicial", cuerpo)
	if err != nil {
		return false, err
	}
	if estado == http.StatusBadRequest && strings.Contains(string(respuesta), "resource_already_exists_exception") {
		return false, nil
	}
	if estado >= 300 {
		return false, fmt.Errorf("creando el índice: %d %s", estado, recortar(respuesta))
	}
	return true, nil
}

// Indexar guarda el producto solo si su versión es mayor a la indexada (versionado externo). Un evento
// repetido o fuera de orden responde 409: no es un error, ya hay un dato igual o más nuevo.
func (c *Cliente) Indexar(ctx context.Context, p modelo.Producto) error {
	ctx, cancelar := context.WithTimeout(ctx, tiempoEscritura)
	defer cancelar()
	estado, respuesta, err := c.pedirJSON(ctx, http.MethodPut, c.rutaDocumento(p.ID, p.Version), aDocumento(p))
	if err != nil {
		return err
	}
	return resultadoEscritura(estado, respuesta)
}

// Quitar borra el documento de un producto despublicado. Si no estaba, no es un error.
func (c *Cliente) Quitar(ctx context.Context, id string, version int) error {
	ctx, cancelar := context.WithTimeout(ctx, tiempoEscritura)
	defer cancelar()
	estado, respuesta, err := c.pedir(ctx, http.MethodDelete, c.rutaDocumento(id, version), nil)
	if err != nil {
		return err
	}
	if estado == http.StatusNotFound {
		return nil
	}
	return resultadoEscritura(estado, respuesta)
}

func (c *Cliente) rutaDocumento(id string, version int) string {
	return "/" + c.alias + "/_doc/" + url.PathEscape(id) + "?version_type=external&version=" + strconv.Itoa(version)
}

func resultadoEscritura(estado int, respuesta []byte) error {
	if estado < 300 || estado == http.StatusConflict {
		return nil
	}
	return fmt.Errorf("elasticsearch rechazó la escritura: %d %s", estado, recortar(respuesta))
}

// Buscar devuelve los ids de la página pedida y el total, en el orden de los resultados. Los datos del
// producto y el precio vigente los pone después Catálogo: el índice no decide qué se vende.
func (c *Cliente) Buscar(ctx context.Context, f modelo.FiltroProductos) ([]string, int64, error) {
	if f.Pagina*f.Tamano > ventanaMaxima {
		v := &modelo.ErrorValidacion{}
		v.Agregar("pagina", "la búsqueda llega hasta el resultado %d: agregá filtros para acotarla", ventanaMaxima)
		return nil, 0, v
	}
	ctx, cancelar := context.WithTimeout(ctx, tiempoBusqueda)
	defer cancelar()
	estado, respuesta, err := c.pedirJSON(ctx, http.MethodPost, "/"+c.alias+"/_search", consulta(f))
	if err != nil {
		return nil, 0, err
	}
	if estado >= 300 {
		return nil, 0, fmt.Errorf("%w: búsqueda rechazada: %d %s", ErrNoDisponible, estado, recortar(respuesta))
	}
	var r struct {
		Hits struct {
			Total struct {
				Value int64 `json:"value"`
			} `json:"total"`
			Hits []struct {
				ID string `json:"_id"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(respuesta, &r); err != nil {
		return nil, 0, fmt.Errorf("%w: respuesta inesperada: %v", ErrNoDisponible, err)
	}
	ids := make([]string, len(r.Hits.Hits))
	for i, h := range r.Hits.Hits {
		ids[i] = h.ID
	}
	return ids, r.Hits.Total.Value, nil
}

// consulta arma la búsqueda: el texto puntúa y tolera errores de tipeo; los filtros recortan sin puntuar.
// Todas las palabras tienen que aparecer, entre el nombre, la descripción y los colores: "remera blanca".
func consulta(f modelo.FiltroProductos) map[string]any {
	filtros := []any{}
	if f.Categoria != "" {
		filtros = append(filtros, termino("categoria", f.Categoria))
	}
	if f.PrecioMin != nil || f.PrecioMax != nil {
		rango := map[string]any{}
		if f.PrecioMin != nil {
			rango["gte"] = *f.PrecioMin
		}
		if f.PrecioMax != nil {
			rango["lte"] = *f.PrecioMax
		}
		filtros = append(filtros, map[string]any{"range": map[string]any{"precio": rango}})
	}
	variante := []any{}
	if f.Talle != "" {
		variante = append(variante, termino("variantes.talle", f.Talle))
	}
	if f.Color != "" {
		variante = append(variante, termino("variantes.color", f.Color))
	}
	if len(variante) > 0 {
		filtros = append(filtros, map[string]any{"nested": map[string]any{
			"path": "variantes", "query": map[string]any{"bool": map[string]any{"filter": variante}},
		}})
	}
	return map[string]any{
		"from": (f.Pagina - 1) * f.Tamano, "size": f.Tamano, "track_total_hits": true, "_source": false,
		"sort": orden(f.Orden),
		"query": map[string]any{"bool": map[string]any{
			"must": []any{map[string]any{"match": map[string]any{"texto": map[string]any{
				"query": f.Texto, "operator": "and", "fuzziness": "AUTO",
			}}}},
			"should": []any{map[string]any{"match_phrase": map[string]any{"nombre": map[string]any{"query": f.Texto, "boost": 3}}}},
			"filter": filtros,
		}},
	}
}

func termino(campo, valor string) map[string]any {
	return map[string]any{"term": map[string]any{campo: valor}}
}

// orden siempre desempata por productoId: sin eso, un resultado podría repetirse entre páginas (HU-02.4).
func orden(o modelo.Orden) []any {
	desempate := map[string]any{"productoId": "asc"}
	switch o {
	case modelo.OrdenPrecioAsc:
		return []any{map[string]any{"precio": "asc"}, desempate}
	case modelo.OrdenPrecioDesc:
		return []any{map[string]any{"precio": "desc"}, desempate}
	case modelo.OrdenNombre:
		return []any{map[string]any{"nombre.orden": "asc"}, desempate}
	case modelo.OrdenNuevos:
		return []any{map[string]any{"creadoEn": "desc"}, desempate}
	}
	return []any{"_score", desempate}
}

// Reconstruir arma un índice nuevo con los productos publicados y recién al terminar cambia el alias, en una
// sola operación: la búsqueda no se corta y sigue contra el índice anterior si algo falla.
func (c *Cliente) Reconstruir(ctx context.Context, productos []modelo.Producto) error {
	ctx, cancelar := context.WithTimeout(ctx, tiempoCarga)
	defer cancelar()
	nuevo := fmt.Sprintf("%s-%d", c.alias, time.Now().UnixNano())
	estado, respuesta, err := c.pedirJSON(ctx, http.MethodPut, "/"+nuevo, configuracion)
	if err != nil {
		return err
	}
	if estado >= 300 {
		return fmt.Errorf("creando el índice %s: %d %s", nuevo, estado, recortar(respuesta))
	}
	if err := c.cargar(ctx, nuevo, productos); err != nil {
		_, _, _ = c.pedir(ctx, http.MethodDelete, "/"+nuevo, nil)
		return err
	}
	// Sin este refresh, el índice nuevo se vería vacío hasta un segundo después de cambiar el alias.
	if estado, respuesta, err = c.pedir(ctx, http.MethodPost, "/"+nuevo+"/_refresh", nil); err != nil {
		return err
	}
	if estado >= 300 {
		return fmt.Errorf("refrescando el índice %s: %d %s", nuevo, estado, recortar(respuesta))
	}
	anteriores, err := c.indicesDelAlias(ctx)
	if err != nil {
		return err
	}
	acciones := []any{map[string]any{"add": map[string]any{"index": nuevo, "alias": c.alias}}}
	for _, indice := range anteriores {
		acciones = append(acciones, map[string]any{"remove": map[string]any{"index": indice, "alias": c.alias}})
	}
	estado, respuesta, err = c.pedirJSON(ctx, http.MethodPost, "/_aliases", map[string]any{"actions": acciones})
	if err != nil {
		return err
	}
	if estado >= 300 {
		return fmt.Errorf("cambiando el alias: %d %s", estado, recortar(respuesta))
	}
	for _, indice := range anteriores {
		_, _, _ = c.pedir(ctx, http.MethodDelete, "/"+indice, nil)
	}
	return nil
}

func (c *Cliente) cargar(ctx context.Context, indice string, productos []modelo.Producto) error {
	for inicio := 0; inicio < len(productos); inicio += loteCarga {
		var cuerpo bytes.Buffer
		codificador := json.NewEncoder(&cuerpo)
		for _, p := range productos[inicio:min(inicio+loteCarga, len(productos))] {
			accion := map[string]any{"index": map[string]any{"_index": indice, "_id": p.ID, "version": p.Version, "version_type": "external"}}
			if err := codificador.Encode(accion); err != nil {
				return err
			}
			if err := codificador.Encode(aDocumento(p)); err != nil {
				return err
			}
		}
		estado, respuesta, err := c.pedirCon(ctx, http.MethodPost, "/_bulk", &cuerpo, "application/x-ndjson")
		if err != nil {
			return err
		}
		if estado >= 300 {
			return fmt.Errorf("cargando el índice: %d %s", estado, recortar(respuesta))
		}
		var r struct {
			Errors bool `json:"errors"`
		}
		if err := json.Unmarshal(respuesta, &r); err != nil || r.Errors {
			return fmt.Errorf("cargando el índice: hubo documentos rechazados: %s", recortar(respuesta))
		}
	}
	return nil
}

func (c *Cliente) indicesDelAlias(ctx context.Context) ([]string, error) {
	estado, respuesta, err := c.pedir(ctx, http.MethodGet, "/_alias/"+c.alias, nil)
	if err != nil {
		return nil, err
	}
	if estado == http.StatusNotFound {
		return nil, nil
	}
	if estado >= 300 {
		return nil, fmt.Errorf("leyendo el alias: %d %s", estado, recortar(respuesta))
	}
	var porIndice map[string]any
	if err := json.Unmarshal(respuesta, &porIndice); err != nil {
		return nil, err
	}
	indices := make([]string, 0, len(porIndice))
	for indice := range porIndice {
		indices = append(indices, indice)
	}
	return indices, nil
}

func (c *Cliente) pedirJSON(ctx context.Context, metodo, ruta string, cuerpo any) (int, []byte, error) {
	datos, err := json.Marshal(cuerpo)
	if err != nil {
		return 0, nil, err
	}
	return c.pedirCon(ctx, metodo, ruta, bytes.NewReader(datos), "application/json")
}

func (c *Cliente) pedir(ctx context.Context, metodo, ruta string, cuerpo io.Reader) (int, []byte, error) {
	return c.pedirCon(ctx, metodo, ruta, cuerpo, "")
}

// pedirCon traduce las fallas de disponibilidad a ErrNoDisponible: red, timeout, 429 y 5xx.
func (c *Cliente) pedirCon(ctx context.Context, metodo, ruta string, cuerpo io.Reader, tipo string) (int, []byte, error) {
	pedido, err := http.NewRequestWithContext(ctx, metodo, c.url+ruta, cuerpo)
	if err != nil {
		return 0, nil, err
	}
	if tipo != "" {
		pedido.Header.Set("Content-Type", tipo)
	}
	respuesta, err := c.http.Do(pedido)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", ErrNoDisponible, err)
	}
	defer respuesta.Body.Close()
	contenido, err := io.ReadAll(io.LimitReader(respuesta.Body, 32<<20))
	if err != nil {
		return 0, nil, fmt.Errorf("%w: %v", ErrNoDisponible, err)
	}
	if respuesta.StatusCode >= 500 || respuesta.StatusCode == http.StatusTooManyRequests {
		return respuesta.StatusCode, contenido, fmt.Errorf("%w: respondió %d", ErrNoDisponible, respuesta.StatusCode)
	}
	return respuesta.StatusCode, contenido, nil
}

func recortar(b []byte) string {
	if len(b) > 500 {
		return string(b[:500])
	}
	return string(b)
}
