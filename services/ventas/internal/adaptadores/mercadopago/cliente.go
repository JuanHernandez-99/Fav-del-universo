// Package mercadopago implementa el puerto ProveedorPagos. Hoy habla con el simulador local
// (mocks/mercadopago-simulador, ADR-014), que imita las rutas y la firma de los avisos del proveedor.
// Esto no se probó contra la API real: pasar a ella puede requerir ajustes en este paquete.
package mercadopago

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/sony/gobreaker/v2"

	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/dominio"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/observabilidad"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/puertos"
)

const dependencia = "mercadopago"

type Config struct {
	URLBase      string
	AccessToken  string
	SecretoAviso string
	URLAviso     string // adónde avisa el resultado del pago
	Timeout      time.Duration
}

type Cliente struct {
	cfg      Config
	http     *http.Client
	circuito *gobreaker.CircuitBreaker[[]byte]
}

func NuevoCliente(cfg Config) *Cliente {
	c := &Cliente{cfg: cfg, http: observabilidad.ClienteHTTP(cfg.Timeout)}
	c.circuito = gobreaker.NewCircuitBreaker[[]byte](gobreaker.Settings{
		Name:        dependencia,
		MaxRequests: 1,                // en semiabierto deja pasar una llamada de prueba
		Timeout:     30 * time.Second, // tiempo abierto antes de probar de nuevo
		ReadyToTrip: func(conteo gobreaker.Counts) bool { return conteo.ConsecutiveFailures >= 5 },
		OnStateChange: func(_ string, de, a gobreaker.State) {
			slog.Warn("cambió el estado del circuit breaker", "dependencia", dependencia, "de", de.String(), "a", a.String())
			observabilidad.EstadoCircuito.WithLabelValues(dependencia).Set(float64(a))
		},
	})
	return c
}

// CrearPago no se reintenta (ADR-005, punto 2): el reintento lo hace el comprador con su misma clave.
func (c *Cliente) CrearPago(ctx context.Context, v dominio.Venta) (string, error) {
	items := make([]map[string]any, len(v.Items))
	for i, it := range v.Items {
		items[i] = map[string]any{"title": strings.TrimSpace(it.Nombre + " " + it.Talle + " " + it.Color), "quantity": it.Cantidad, "unit_price": float64(it.Precio) / 100, "currency_id": "ARS"}
	}
	cuerpo, err := json.Marshal(map[string]any{"external_reference": v.ID, "items": items, "notification_url": c.cfg.URLAviso})
	if err != nil {
		return "", err
	}
	contenido, err := c.llamar(ctx, http.MethodPost, "/checkout/preferences", cuerpo)
	if err != nil {
		return "", err
	}
	var r struct {
		InitPoint string `json:"init_point"`
	}
	if err := json.Unmarshal(contenido, &r); err != nil || r.InitPoint == "" {
		return "", fmt.Errorf("%w: respuesta con formato inesperado", dominio.ErrPagoNoDisponible)
	}
	return r.InitPoint, nil
}

func (c *Cliente) ConsultarPago(ctx context.Context, pagoID string) (puertos.Pago, error) {
	contenido, err := c.llamar(ctx, http.MethodGet, "/v1/payments/"+pagoID, nil)
	if err != nil {
		return puertos.Pago{}, err
	}
	var r struct {
		ID                json.Number `json:"id"`
		Status            string      `json:"status"`
		ExternalReference string      `json:"external_reference"`
	}
	if err := json.Unmarshal(contenido, &r); err != nil {
		return puertos.Pago{}, fmt.Errorf("%w: respuesta con formato inesperado: %v", dominio.ErrPagoNoDisponible, err)
	}
	pago := puertos.Pago{ID: r.ID.String(), VentaID: r.ExternalReference, Estado: puertos.PagoPendiente}
	switch r.Status {
	case "approved":
		pago.Estado = puertos.PagoAprobado
	case "rejected", "cancelled":
		pago.Estado = puertos.PagoRechazado
	}
	return pago, nil
}

// VerificarFirma comprueba el HMAC del aviso. La firma tiene la forma "ts=<segundos>,v1=<hmac>".
func (c *Cliente) VerificarFirma(firma, pedidoID, pagoID string) bool {
	var ts, v1 string
	for _, parte := range strings.Split(firma, ",") {
		clave, valor, _ := strings.Cut(strings.TrimSpace(parte), "=")
		switch clave {
		case "ts":
			ts = valor
		case "v1":
			v1 = valor
		}
	}
	if ts == "" || v1 == "" {
		return false
	}
	return hmac.Equal([]byte(v1), []byte(FirmarAviso(c.cfg.SecretoAviso, pagoID, pedidoID, ts)))
}

// FirmarAviso calcula el HMAC del aviso. Lo usan también los tests.
func FirmarAviso(secreto, pagoID, pedidoID, ts string) string {
	h := hmac.New(sha256.New, []byte(secreto))
	fmt.Fprintf(h, "id:%s;request-id:%s;ts:%s;", pagoID, pedidoID, ts)
	return hex.EncodeToString(h.Sum(nil))
}

func (c *Cliente) llamar(ctx context.Context, metodo, ruta string, cuerpo []byte) ([]byte, error) {
	respuesta, err := c.circuito.Execute(func() ([]byte, error) {
		pedido, err := http.NewRequestWithContext(ctx, metodo, c.cfg.URLBase+ruta, bytes.NewReader(cuerpo))
		if err != nil {
			return nil, err
		}
		pedido.Header.Set("Content-Type", "application/json")
		pedido.Header.Set("Authorization", "Bearer "+c.cfg.AccessToken)
		res, err := c.http.Do(pedido)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		contenido, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		if res.StatusCode >= 300 {
			return nil, fmt.Errorf("mercado pago respondió %d", res.StatusCode)
		}
		return contenido, nil
	})
	switch {
	case err == nil:
		observabilidad.LlamadasDependencia.WithLabelValues(dependencia, "ok").Inc()
		return respuesta, nil
	case errors.Is(err, gobreaker.ErrOpenState), errors.Is(err, gobreaker.ErrTooManyRequests):
		observabilidad.LlamadasDependencia.WithLabelValues(dependencia, "circuito_abierto").Inc()
	default:
		observabilidad.LlamadasDependencia.WithLabelValues(dependencia, "error").Inc()
	}
	return nil, fmt.Errorf("%w: %v", dominio.ErrPagoNoDisponible, err)
}
