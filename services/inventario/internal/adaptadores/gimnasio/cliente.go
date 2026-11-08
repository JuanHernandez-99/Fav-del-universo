// Package gimnasio consume la capacidad del gimnasio que vende ropa de FAV en consignación (ADR-018).
// Es una capa anticorrupción: traduce el modelo del gimnasio al de FAV, y el resto de Inventario
// no conoce sus rutas, sus nombres de campo ni sus errores.
//
// Contrato: docs/contracts/externos/gimnasio/gimnasio-consignaciones-1.0.0.openapi.yaml.
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
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/sony/gobreaker/v2"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/dominio"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/observabilidad"
)

const (
	dependencia     = "gimnasio"
	rutaStock       = "/api/v1/consignaciones/stock"
	rutaReposicion  = "/api/v1/consignaciones/reposiciones"
	maximoRespuesta = 1 << 20
)

type Config struct {
	URLBase     string
	APIKey      string
	ProveedorID string // cómo nos identifica el gimnasio
	Timeout     time.Duration
}

type Cliente struct {
	cfg      Config
	http     *http.Client
	circuito *gobreaker.CircuitBreaker[respuesta]
}

type respuesta struct {
	estado int
	cuerpo []byte
}

// errorDisponibilidad es una respuesta que indica que el gimnasio no está atendiendo bien: abre el circuito.
type errorDisponibilidad struct{ estado int }

func (e *errorDisponibilidad) Error() string {
	return fmt.Sprintf("el gimnasio respondió %d", e.estado)
}

func NuevoCliente(cfg Config) *Cliente {
	cliente := observabilidad.ClienteHTTP(cfg.Timeout)
	// Una redirección no es parte del contrato: se evalúa la respuesta, sin seguirla.
	cliente.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	c := &Cliente{cfg: cfg, http: cliente}
	c.circuito = gobreaker.NewCircuitBreaker[respuesta](gobreaker.Settings{
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

// ---------- Modelo del gimnasio ----------

type reposicionGimnasio struct {
	ProveedorID    string `json:"proveedorId"`
	ProductoCodigo string `json:"productoCodigo"`
	Unidades       int    `json:"unidades"`
	Referencia     string `json:"referencia"`
}

type reposicionRecibida struct {
	ReposicionID string `json:"reposicionId"`
	Estado       string `json:"estado"`
}

// Punteros: distinguen un campo que no vino de un 0, y `items` ausente de `items: []`.
type stockGimnasio struct {
	ProveedorID string `json:"proveedorId"`
	Items       *[]struct {
		ProductoCodigo      string `json:"productoCodigo"`
		UnidadesDisponibles *int   `json:"unidadesDisponibles"`
		UnidadesVendidas    *int   `json:"unidadesVendidas"`
	} `json:"items"`
}

type errorGimnasio struct {
	Codigo string `json:"codigo"`
}

// ---------- Qué significa cada respuesta del POST (ADR-018) ----------

type resultado int

const (
	resultadoRecibida resultado = iota
	resultadoRechazada
	resultadoConflicto
	resultadoConfiguracion
	resultadoIncierto
	resultadoContratoIncompatible // se trata como incierto, y además queda en el log
)

// rechazosDefinitivos: el contrato garantiza que con estas respuestas no se registró la entrega.
// Se decide por el `codigo`: el mismo estado sin ese código puede venir de un proxy, no del gimnasio.
var rechazosDefinitivos = map[string]int{
	"SOLICITUD_INVALIDA":         http.StatusBadRequest,
	"IDEMPOTENCIA_INVALIDA":      http.StatusBadRequest,
	"SOLICITUD_DEMASIADO_GRANDE": http.StatusRequestEntityTooLarge,
	"TIPO_CONTENIDO_NO_ADMITIDO": http.StatusUnsupportedMediaType,
	"STOCK_NO_REPRESENTABLE":     http.StatusUnprocessableEntity,
}

// clasificar es la única decisión sobre devolver stock. Ante la duda es incierto: devolver unidades que
// quizá ya llegaron al gimnasio es peor que reintentar, porque la misma clave no las duplica.
func clasificar(estado int, cuerpo []byte) (resultado, string, string) {
	if estado == http.StatusCreated {
		var r reposicionRecibida
		if err := json.Unmarshal(cuerpo, &r); err != nil || r.Estado != "RECIBIDA" || uuid.Validate(r.ReposicionID) != nil {
			return resultadoContratoIncompatible, "", ""
		}
		return resultadoRecibida, r.ReposicionID, ""
	}
	var e errorGimnasio
	_ = json.Unmarshal(cuerpo, &e) // sin cuerpo válido, el código queda vacío
	switch {
	case estado == http.StatusUnauthorized, estado == http.StatusForbidden:
		return resultadoConfiguracion, "", e.Codigo
	case estado == http.StatusConflict:
		return resultadoConflicto, "", e.Codigo
	case rechazosDefinitivos[e.Codigo] == estado:
		return resultadoRechazada, "", e.Codigo
	case estado < http.StatusBadRequest:
		return resultadoContratoIncompatible, "", e.Codigo // otro 2xx o una redirección
	}
	return resultadoIncierto, "", e.Codigo
}

// ---------- Puerto ----------

func (c *Cliente) EnviarReposicion(ctx context.Context, r dominio.Reposicion) (string, error) {
	cuerpo, err := json.Marshal(reposicionGimnasio{
		ProveedorID:    c.cfg.ProveedorID,
		ProductoCodigo: r.VarianteID, // el gimnasio identifica la prenda por la variante, con talle y color
		Unidades:       r.Cantidad,
		Referencia:     r.ID,
	})
	if err != nil {
		return "", err
	}
	// La clave y la referencia son siempre las de la reposición: si un envío llegó pero se perdió la respuesta,
	// el reintento devuelve la entrega original y no duplica la mercadería.
	res, err := c.llamar(ctx, http.MethodPost, rutaReposicion, cuerpo, r.ClaveIdempotencia)
	var caida *errorDisponibilidad
	if err != nil && !errors.As(err, &caida) {
		return "", err // red, timeout o circuito abierto: no hay respuesta que interpretar
	}
	que, idGimnasio, codigo := clasificar(res.estado, res.cuerpo)
	contar(ctx, que, r.ID, res.estado, codigo)
	detalle := fmt.Sprintf("gimnasio respondió %d %s", res.estado, codigo)
	switch que {
	case resultadoRecibida:
		return idGimnasio, nil
	case resultadoRechazada:
		return "", fmt.Errorf("%w: %s", dominio.ErrReposicionRechazada, detalle)
	case resultadoConflicto:
		return "", fmt.Errorf("%w: %s", dominio.ErrReposicionConflicto, detalle)
	case resultadoConfiguracion:
		return "", fmt.Errorf("%w: %s", dominio.ErrGimnasioConfiguracion, detalle)
	case resultadoContratoIncompatible:
		return "", fmt.Errorf("CONTRATO_INCOMPATIBLE: %s", detalle)
	}
	return "", errors.New(detalle)
}

func (c *Cliente) ConsultarStock(ctx context.Context) ([]dominio.StockConsignacion, error) {
	consulta := url.Values{"proveedorId": {c.cfg.ProveedorID}}
	res, err := c.llamar(ctx, http.MethodGet, rutaStock+"?"+consulta.Encode(), nil, "")
	if err != nil {
		observabilidad.LlamadasDependencia.WithLabelValues(dependencia, etiquetaDeError(err)).Inc()
		return nil, fmt.Errorf("%w: %v", dominio.ErrProveedorNoDisponible, err)
	}
	if res.estado == http.StatusUnauthorized || res.estado == http.StatusForbidden {
		slog.ErrorContext(ctx, "el gimnasio no aceptó la credencial de FAV", "estado", res.estado)
		observabilidad.LlamadasDependencia.WithLabelValues(dependencia, "configuracion").Inc()
		return nil, fmt.Errorf("%w: credencial rechazada (%d)", dominio.ErrProveedorNoDisponible, res.estado)
	}
	stock, err := c.leerStock(res)
	if err != nil {
		// Un error nunca se muestra como stock en cero: el panel recibe "no disponible".
		slog.WarnContext(ctx, "respuesta del gimnasio fuera de contrato", "error_contrato", "CONTRATO_INCOMPATIBLE", "estado", res.estado, "error", err)
		observabilidad.LlamadasDependencia.WithLabelValues(dependencia, "contrato_incompatible").Inc()
		return nil, fmt.Errorf("%w: %v", dominio.ErrProveedorNoDisponible, err)
	}
	observabilidad.LlamadasDependencia.WithLabelValues(dependencia, "ok").Inc()
	return stock, nil
}

func (c *Cliente) leerStock(res respuesta) ([]dominio.StockConsignacion, error) {
	if res.estado != http.StatusOK {
		return nil, fmt.Errorf("se esperaba 200 y llegó %d", res.estado)
	}
	var s stockGimnasio
	if err := json.Unmarshal(res.cuerpo, &s); err != nil {
		return nil, fmt.Errorf("formato inesperado: %w", err)
	}
	if s.ProveedorID != c.cfg.ProveedorID || s.Items == nil {
		return nil, errors.New("faltan proveedorId o items")
	}
	stock := make([]dominio.StockConsignacion, 0, len(*s.Items))
	for _, it := range *s.Items {
		if it.ProductoCodigo == "" || it.UnidadesDisponibles == nil || it.UnidadesVendidas == nil ||
			*it.UnidadesDisponibles < 0 || *it.UnidadesVendidas < 0 {
			return nil, errors.New("item incompleto o con cantidades negativas")
		}
		stock = append(stock, dominio.StockConsignacion{VarianteID: it.ProductoCodigo, Cantidad: *it.UnidadesDisponibles, Vendidas: *it.UnidadesVendidas})
	}
	return stock, nil
}

// llamar hace una llamada protegida por el circuit breaker. Con el circuito abierto falla enseguida,
// sin esperar el timeout: así un gimnasio caído no frena a los procesos ni al panel. Solo cuentan como
// falla la red, el timeout y las respuestas que indican que el gimnasio no atiende; un 401 o un 409 no.
func (c *Cliente) llamar(ctx context.Context, metodo, ruta string, cuerpo []byte, claveIdempotencia string) (respuesta, error) {
	res, err := c.circuito.Execute(func() (respuesta, error) {
		pedido, err := http.NewRequestWithContext(ctx, metodo, c.cfg.URLBase+ruta, bytes.NewReader(cuerpo))
		if err != nil {
			return respuesta{}, err
		}
		pedido.Header.Set("X-API-Key", c.cfg.APIKey)
		if cuerpo != nil {
			pedido.Header.Set("Content-Type", "application/json")
		}
		if claveIdempotencia != "" {
			pedido.Header.Set("Idempotency-Key", claveIdempotencia)
		}
		recibida, err := c.http.Do(pedido)
		if err != nil {
			return respuesta{}, err
		}
		defer recibida.Body.Close()
		contenido, _ := io.ReadAll(io.LimitReader(recibida.Body, maximoRespuesta))
		r := respuesta{estado: recibida.StatusCode, cuerpo: contenido}
		if indicaCaida(recibida.StatusCode) {
			return r, &errorDisponibilidad{estado: recibida.StatusCode}
		}
		return r, nil
	})
	if err != nil {
		var caida *errorDisponibilidad
		if !errors.As(err, &caida) {
			observabilidad.LlamadasDependencia.WithLabelValues(dependencia, etiquetaDeError(err)).Inc()
		}
	}
	return res, err
}

func indicaCaida(estado int) bool {
	return estado >= 500 || estado == http.StatusRequestTimeout || estado == http.StatusTooManyRequests || estado == http.StatusNotFound
}

func etiquetaDeError(err error) string {
	if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
		return "circuito_abierto"
	}
	return "error"
}

func contar(ctx context.Context, que resultado, referencia string, estado int, codigo string) {
	etiqueta := map[resultado]string{
		resultadoRecibida: "ok", resultadoRechazada: "rechazada", resultadoConflicto: "conflicto",
		resultadoConfiguracion: "configuracion", resultadoIncierto: "error", resultadoContratoIncompatible: "contrato_incompatible",
	}[que]
	observabilidad.LlamadasDependencia.WithLabelValues(dependencia, etiqueta).Inc()
	if que == resultadoContratoIncompatible {
		slog.WarnContext(ctx, "respuesta del gimnasio fuera de contrato", "error_contrato", "CONTRATO_INCOMPATIBLE", "referencia", referencia, "estado", estado, "codigo", codigo)
	}
}
