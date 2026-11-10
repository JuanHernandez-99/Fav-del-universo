// Package inventario habla con Inventario por el balanceo del gateway. Aplica los tiempos y
// reintentos del ADR-005: 2 s por intento y hasta 2 reintentos, solo ante fallas transitorias.
package inventario

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/dominio"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/observabilidad"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/puertos"
)

const dependencia = "inventario"

type Cliente struct {
	urlBase string
	http    *http.Client
	esperas []time.Duration
}

func NuevoCliente(urlBase string) *Cliente {
	return &Cliente{urlBase, observabilidad.ClienteHTTP(2 * time.Second), []time.Duration{0, 200 * time.Millisecond, 400 * time.Millisecond}}
}

type item struct {
	VarianteID string `json:"varianteId"`
	Cantidad   int    `json:"cantidad"`
}

func itemsDe(lineas []dominio.LineaPedido) []item {
	items := make([]item, len(lineas))
	for i, l := range lineas {
		items[i] = item{l.VarianteID, l.Cantidad}
	}
	return items
}

func (c *Cliente) Reservar(ctx context.Context, clave, referencia string, lineas []dominio.LineaPedido, vigencia time.Duration) (puertos.Reserva, error) {
	cuerpo, err := json.Marshal(map[string]any{"referencia": referencia, "vigenciaMinutos": int(vigencia.Minutes()), "items": itemsDe(lineas)})
	if err != nil {
		return puertos.Reserva{}, err
	}
	contenido, err := c.llamar(ctx, "/v1/reservas", clave, cuerpo)
	if err != nil {
		return puertos.Reserva{}, err
	}
	var r struct {
		ReservaID string    `json:"reservaId"`
		Estado    string    `json:"estado"`
		VenceEn   time.Time `json:"venceEn"`
		CreadaEn  time.Time `json:"creadaEn"`
	}
	if err := json.Unmarshal(contenido, &r); err != nil {
		return puertos.Reserva{}, fmt.Errorf("%w: respuesta con formato inesperado: %v", dominio.ErrInventarioNoDisponible, err)
	}
	return puertos.Reserva{ID: r.ReservaID, Estado: r.Estado, VenceEn: r.VenceEn, CreadaEn: r.CreadaEn}, nil
}

func (c *Cliente) Confirmar(ctx context.Context, reservaID string) error {
	_, err := c.llamar(ctx, "/v1/reservas/"+reservaID+"/confirmar", "", nil)
	return err
}

func (c *Cliente) Liberar(ctx context.Context, reservaID string) error {
	_, err := c.llamar(ctx, "/v1/reservas/"+reservaID+"/liberar", "", nil)
	return err
}

func (c *Cliente) DescontarFisico(ctx context.Context, clave, referencia string, lineas []dominio.LineaPedido) error {
	cuerpo, err := json.Marshal(map[string]any{"referencia": referencia, "items": itemsDe(lineas)})
	if err != nil {
		return err
	}
	_, err = c.llamar(ctx, "/v1/descuentos-fisicos", clave, cuerpo)
	return err
}

// llamar reintenta solo ante timeout, conexión rechazada, 502, 503 o 504. Nunca ante un error de
// negocio, que es definitivo. Es seguro: reservar lleva clave de idempotencia y confirmar y liberar
// son idempotentes en Inventario (ADR-005, puntos 2 y 3).
func (c *Cliente) llamar(ctx context.Context, ruta, clave string, cuerpo []byte) ([]byte, error) {
	var ultimoError error
	for intento, espera := range c.esperas {
		if espera > 0 {
			espera += time.Duration(rand.IntN(100)) * time.Millisecond
			slog.WarnContext(ctx, "reintentando la llamada a Inventario", "ruta", ruta, "intento", intento+1, "espera_ms", espera.Milliseconds(), "error", ultimoError)
			select {
			case <-time.After(espera):
			case <-ctx.Done():
				return nil, fmt.Errorf("%w: %v", dominio.ErrInventarioNoDisponible, ctx.Err())
			}
		}
		contenido, codigo, err := c.intentar(ctx, ruta, clave, cuerpo)
		switch {
		case err != nil:
			ultimoError = err
		case codigo == http.StatusBadGateway || codigo == http.StatusServiceUnavailable || codigo == http.StatusGatewayTimeout:
			ultimoError = fmt.Errorf("inventario respondió %d", codigo)
		case codigo >= 200 && codigo < 300:
			observabilidad.LlamadasDependencia.WithLabelValues(dependencia, "ok").Inc()
			return contenido, nil
		default:
			observabilidad.LlamadasDependencia.WithLabelValues(dependencia, "rechazada").Inc()
			return nil, traducirError(codigo, contenido)
		}
	}
	observabilidad.LlamadasDependencia.WithLabelValues(dependencia, "error").Inc()
	return nil, fmt.Errorf("%w: %v", dominio.ErrInventarioNoDisponible, ultimoError)
}

func (c *Cliente) intentar(ctx context.Context, ruta, clave string, cuerpo []byte) ([]byte, int, error) {
	pedido, err := http.NewRequestWithContext(ctx, http.MethodPost, c.urlBase+ruta, bytes.NewReader(cuerpo))
	if err != nil {
		return nil, 0, err
	}
	pedido.Header.Set("Content-Type", "application/json")
	if clave != "" {
		pedido.Header.Set("Idempotency-Key", clave)
	}
	res, err := c.http.Do(pedido)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	contenido, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return contenido, res.StatusCode, err
}

// traducirError pasa los códigos estables de Inventario a errores del dominio de Ventas.
func traducirError(codigo int, contenido []byte) error {
	var p struct {
		Codigo    string             `json:"codigo"`
		Variantes []string           `json:"variantes"`
		Faltantes []dominio.Faltante `json:"faltantes"`
	}
	if err := json.Unmarshal(contenido, &p); err != nil {
		return fmt.Errorf("respuesta inesperada de inventario (%d): %w", codigo, err)
	}
	switch p.Codigo {
	case "STOCK_INSUFICIENTE":
		return &dominio.ErrorStockInsuficiente{Faltantes: p.Faltantes}
	case "VARIANTE_INEXISTENTE":
		return &dominio.ErrorVarianteInexistente{Variantes: p.Variantes}
	case "CLAVE_IDEMPOTENCIA_REUTILIZADA":
		return dominio.ErrClaveReutilizada
	case "RESERVA_VENCIDA":
		return dominio.ErrReservaVencida
	case "RESERVA_LIBERADA":
		return dominio.ErrReservaLiberada
	case "RESERVA_CONFIRMADA":
		return dominio.ErrReservaConfirmada
	case "RESERVA_NO_ENCONTRADA":
		return dominio.ErrReservaNoEncontrada
	}
	return errors.New("inventario respondió " + http.StatusText(codigo) + ": " + p.Codigo)
}
