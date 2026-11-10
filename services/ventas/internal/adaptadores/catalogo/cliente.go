// Package catalogo pide a Catálogo los datos y el precio vigente de las variantes, por lote.
// Es una lectura: timeout de 1 s y un reintento ante fallas transitorias (ADR-005, punto 2).
package catalogo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/dominio"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/observabilidad"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/puertos"
)

const dependencia = "catalogo"

type Cliente struct {
	urlBase string
	http    *http.Client
}

func NuevoCliente(urlBase string) *Cliente {
	return &Cliente{urlBase, observabilidad.ClienteHTTP(time.Second)}
}

type respuestaLote struct {
	Items []struct {
		VarianteID string  `json:"varianteId"`
		Nombre     string  `json:"nombre"`
		Talle      string  `json:"talle"`
		Color      string  `json:"color"`
		Publicado  bool    `json:"publicado"`
		Precio     float64 `json:"precio"` // pesos
	} `json:"items"`
}

func (c *Cliente) Variantes(ctx context.Context, ids []string, medio dominio.Medio) ([]puertos.DetalleVariante, error) {
	consulta := url.Values{"variantes": {strings.Join(ids, ",")}, "medioPago": {string(medio)}}
	destino := c.urlBase + "/v1/variantes?" + consulta.Encode()

	var ultimoError error
	for intento := range 2 {
		if intento > 0 {
			slog.WarnContext(ctx, "reintentando la llamada a Catálogo", "error", ultimoError)
		}
		pedido, err := http.NewRequestWithContext(ctx, http.MethodGet, destino, nil)
		if err != nil {
			return nil, err
		}
		res, err := c.http.Do(pedido)
		if err != nil {
			ultimoError = err
			continue
		}
		contenido, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		switch {
		case err != nil:
			ultimoError = err
		case res.StatusCode == http.StatusBadGateway || res.StatusCode == http.StatusServiceUnavailable || res.StatusCode == http.StatusGatewayTimeout:
			ultimoError = fmt.Errorf("catálogo respondió %d", res.StatusCode)
		case res.StatusCode == http.StatusOK:
			observabilidad.LlamadasDependencia.WithLabelValues(dependencia, "ok").Inc()
			return traducir(contenido)
		default:
			observabilidad.LlamadasDependencia.WithLabelValues(dependencia, "rechazada").Inc()
			return nil, traducirError(res.StatusCode, contenido, ids)
		}
	}
	observabilidad.LlamadasDependencia.WithLabelValues(dependencia, "error").Inc()
	return nil, fmt.Errorf("%w: %v", dominio.ErrCatalogoNoDisponible, ultimoError)
}

func traducir(contenido []byte) ([]puertos.DetalleVariante, error) {
	var r respuestaLote
	if err := json.Unmarshal(contenido, &r); err != nil {
		return nil, fmt.Errorf("%w: respuesta con formato inesperado: %v", dominio.ErrCatalogoNoDisponible, err)
	}
	detalles := make([]puertos.DetalleVariante, len(r.Items))
	for i, it := range r.Items {
		detalles[i] = puertos.DetalleVariante{
			VarianteID: it.VarianteID, Nombre: it.Nombre, Talle: it.Talle, Color: it.Color,
			Publicado: it.Publicado, Precio: int64(math.Round(it.Precio * 100)),
		}
	}
	return detalles, nil
}

func traducirError(codigo int, contenido []byte, ids []string) error {
	var p struct {
		Codigo    string   `json:"codigo"`
		Variantes []string `json:"variantes"`
	}
	_ = json.Unmarshal(contenido, &p)
	if p.Codigo == "VARIANTE_INEXISTENTE" {
		return &dominio.ErrorVarianteInexistente{Variantes: p.Variantes}
	}
	// Ventas validó los ids antes de llamar: otro 4xx indica un problema entre servicios, no del comprador.
	return fmt.Errorf("catálogo respondió %d (%s) a las variantes %v", codigo, p.Codigo, ids)
}
