// Package gimnasio consume la capacidad del gimnasio que vende ropa de FAV en consignación (ADR-009).
// Es una capa anticorrupción: traduce el modelo del gimnasio al de FAV, y el resto de Inventario
// no conoce sus rutas, sus nombres de campo ni sus errores.
//
// El grupo del gimnasio todavía no publicó su contrato: este adaptador usa el contrato supuesto de
// mocks/gimnasio-consignacion. Cuando llegue el real, cambia solo este paquete.
package gimnasio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/sony/gobreaker/v2"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/dominio"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/observabilidad"
)

const dependencia = "gimnasio"

type Config struct {
	URLBase    string
	APIKey     string
	ProveedorID string // cómo nos identifica el gimnasio
	Timeout    time.Duration
}

type Cliente struct {
	cfg      Config
	http     *http.Client
	circuito *gobreaker.CircuitBreaker[[]byte]
}

// errorRechazo marca una respuesta 4xx: el gimnasio entendió el pedido y lo rechazó.
// No es una falla de disponibilidad, así que no abre el circuito.
type errorRechazo struct {
	codigo int
	cuerpo string
}

func (e *errorRechazo) Error() string { return fmt.Sprintf("el gimnasio respondió %d: %s", e.codigo, e.cuerpo) }

func NuevoCliente(cfg Config) *Cliente {
	c := &Cliente{cfg: cfg, http: observabilidad.ClienteHTTP(cfg.Timeout)}
	c.circuito = gobreaker.NewCircuitBreaker[[]byte](gobreaker.Settings{
		Name:        dependencia,
		MaxRequests: 1,                // en semiabierto deja pasar una llamada de prueba
		Timeout:     30 * time.Second, // tiempo abierto antes de probar de nuevo
		ReadyToTrip: func(conteo gobreaker.Counts) bool { return conteo.ConsecutiveFailures >= 5 },
		IsSuccessful: func(err error) bool {
			var rechazo *errorRechazo
			return err == nil || errors.As(err, &rechazo)
		},
		OnStateChange: func(_ string, de, a gobreaker.State) {
			slog.Warn("cambió el estado del circuit breaker", "dependencia", dependencia, "de", de.String(), "a", a.String())
			observabilidad.EstadoCircuito.WithLabelValues(dependencia).Set(float64(a))
		},
	})
	return c
}

// ---------- Modelo del gimnasio (contrato supuesto) ----------

type reposicionGimnasio struct {
	ProveedorID    string `json:"proveedorId"`
	ProductoCodigo string `json:"productoCodigo"`
	Unidades       int    `json:"unidades"`
	Referencia     string `json:"referencia"`
}

type stockGimnasio struct {
	Items []struct {
		ProductoCodigo      string `json:"productoCodigo"`
		UnidadesDisponibles int    `json:"unidadesDisponibles"`
		UnidadesVendidas    int    `json:"unidadesVendidas"`
	} `json:"items"`
}

// ---------- Puerto ----------

func (c *Cliente) EnviarReposicion(ctx context.Context, r dominio.Reposicion) error {
	cuerpo, err := json.Marshal(reposicionGimnasio{
		ProveedorID:    c.cfg.ProveedorID,
		ProductoCodigo: r.VarianteID, // FAV identifica la prenda por la variante
		Unidades:       r.Cantidad,
		Referencia:     r.ID,
	})
	if err != nil {
		return err
	}
	// La clave de idempotencia es la de la reposición: si un envío llegó pero se perdió la respuesta,
	// el reintento no duplica la mercadería en el gimnasio.
	_, err = c.llamar(ctx, http.MethodPost, "/api/v1/consignaciones/reposiciones", cuerpo, r.ClaveIdempotencia)
	var rechazo *errorRechazo
	if errors.As(err, &rechazo) {
		return fmt.Errorf("%w: %s", dominio.ErrReposicionRechazada, rechazo.Error())
	}
	return err
}

func (c *Cliente) ConsultarStock(ctx context.Context) ([]dominio.StockConsignacion, error) {
	respuesta, err := c.llamar(ctx, http.MethodGet, "/api/v1/consignaciones/stock?proveedorId="+c.cfg.ProveedorID, nil, "")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", dominio.ErrProveedorNoDisponible, err)
	}
	var s stockGimnasio
	if err := json.Unmarshal(respuesta, &s); err != nil {
		// Un cambio de formato no avisado se trata como indisponibilidad, no como error de FAV.
		return nil, fmt.Errorf("%w: respuesta con formato inesperado: %v", dominio.ErrProveedorNoDisponible, err)
	}
	stock := make([]dominio.StockConsignacion, 0, len(s.Items))
	for _, it := range s.Items {
		stock = append(stock, dominio.StockConsignacion{VarianteID: it.ProductoCodigo, Cantidad: it.UnidadesDisponibles, Vendidas: it.UnidadesVendidas})
	}
	return stock, nil
}

// llamar hace una llamada protegida por el circuit breaker. Con el circuito abierto falla enseguida,
// sin esperar el timeout: así un gimnasio caído no frena a los procesos ni al panel.
func (c *Cliente) llamar(ctx context.Context, metodo, ruta string, cuerpo []byte, claveIdempotencia string) ([]byte, error) {
	respuesta, err := c.circuito.Execute(func() ([]byte, error) {
		pedido, err := http.NewRequestWithContext(ctx, metodo, c.cfg.URLBase+ruta, bytes.NewReader(cuerpo))
		if err != nil {
			return nil, err
		}
		pedido.Header.Set("Content-Type", "application/json")
		pedido.Header.Set("X-API-Key", c.cfg.APIKey)
		if claveIdempotencia != "" {
			pedido.Header.Set("Idempotency-Key", claveIdempotencia)
		}
		res, err := c.http.Do(pedido)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		contenido, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		switch {
		case res.StatusCode >= 500 || res.StatusCode == http.StatusTooManyRequests:
			return nil, fmt.Errorf("el gimnasio respondió %d", res.StatusCode)
		case res.StatusCode >= 400:
			return nil, &errorRechazo{codigo: res.StatusCode, cuerpo: string(contenido)}
		}
		return contenido, nil
	})

	var rechazo *errorRechazo
	switch {
	case err == nil:
		observabilidad.LlamadasDependencia.WithLabelValues(dependencia, "ok").Inc()
	case errors.As(err, &rechazo):
		observabilidad.LlamadasDependencia.WithLabelValues(dependencia, "rechazada").Inc()
	case errors.Is(err, gobreaker.ErrOpenState), errors.Is(err, gobreaker.ErrTooManyRequests):
		observabilidad.LlamadasDependencia.WithLabelValues(dependencia, "circuito_abierto").Inc()
	default:
		observabilidad.LlamadasDependencia.WithLabelValues(dependencia, "error").Inc()
	}
	return respuesta, err
}
