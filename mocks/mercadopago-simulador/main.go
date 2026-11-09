// Simulador de Mercado Pago (ADR-014). Imita lo que Ventas usa del proveedor: crear el pago,
// consultar un pago y avisar el resultado con una firma. No procesa dinero ni habla con internet.
//
//   - POST /checkout/preferences: crea el pago y devuelve init_point, la página a la que va el comprador.
//   - GET  /checkout/{id}:        esa página, con botones para aprobar o rechazar.
//   - POST /checkout/{id}/aprobar y /rechazar: resuelve el pago y envía el aviso a notification_url.
//     Con ?duplicar=1 envía el mismo aviso dos veces, para probar la idempotencia.
//   - GET  /v1/payments/{id}:     estado del pago.
//
// Para probar una caída alcanza con detener el contenedor.
package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type preferencia struct {
	ID                string `json:"id"`
	ExternalReference string `json:"external_reference"`
	NotificationURL   string `json:"notification_url"`
	InitPoint         string `json:"init_point"`
	Items             []struct {
		Title     string  `json:"title"`
		Quantity  int     `json:"quantity"`
		UnitPrice float64 `json:"unit_price"`
	} `json:"items"`
}

type pago struct {
	ID                int64  `json:"id"`
	Status            string `json:"status"`
	ExternalReference string `json:"external_reference"`
}

type simulador struct {
	urlPublica string
	secreto    string
	cliente    *http.Client

	mu           sync.Mutex
	preferencias map[string]preferencia
	pagos        map[int64]pago
	siguiente    int64
}

func (s *simulador) crearPreferencia(w http.ResponseWriter, r *http.Request) {
	var p preferencia
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil || p.ExternalReference == "" {
		http.Error(w, `{"message":"external_reference es obligatorio"}`, http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.siguiente++
	p.ID = "pref-" + strconv.FormatInt(s.siguiente, 10)
	p.InitPoint = s.urlPublica + "/checkout/" + p.ID
	s.preferencias[p.ID] = p
	s.mu.Unlock()
	slog.Info("pago creado", "preferencia", p.ID, "venta", p.ExternalReference)
	escribirJSON(w, http.StatusCreated, p)
}

var pagina = template.Must(template.New("checkout").Parse(`<!doctype html>
<html lang="es"><meta charset="utf-8"><title>Simulador de Mercado Pago</title>
<body style="font-family:sans-serif;max-width:32rem;margin:2rem auto">
<h1>Simulador de Mercado Pago</h1>
<p>Pago de prueba: no se cobra nada. Esta página reemplaza al checkout real (ADR-014).</p>
<ul>{{range .Items}}<li>{{.Quantity}} x {{.Title}} — ${{.UnitPrice}}</li>{{end}}</ul>
<form method="post" action="/checkout/{{.ID}}/aprobar"><button>Pagar</button></form>
<p>Para probar qué pasa si Mercado Pago rechaza el pago (por ejemplo, una tarjeta sin fondos):</p>
<form method="post" action="/checkout/{{.ID}}/rechazar"><button>Simular tarjeta rechazada</button></form>
</body></html>`))

// resultado es lo que ve quien aprieta un botón del checkout: el comprador vuelve a la tienda por su cuenta.
var resultado = template.Must(template.New("resultado").Parse(`<!doctype html>
<html lang="es"><meta charset="utf-8"><title>Simulador de Mercado Pago</title>
<body style="font-family:sans-serif;max-width:32rem;margin:2rem auto">
<h1>Pago {{if eq .Status "approved"}}aprobado{{else}}rechazado{{end}}</h1>
<p>Podés cerrar esta pestaña y volver a la tienda.</p>
</body></html>`))

func (s *simulador) mostrarCheckout(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	p, ok := s.preferencias[r.PathValue("id")]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = pagina.Execute(w, p)
}

func (s *simulador) resolver(estado string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		p, ok := s.preferencias[r.PathValue("id")]
		if !ok {
			s.mu.Unlock()
			http.NotFound(w, r)
			return
		}
		s.siguiente++
		nuevo := pago{ID: s.siguiente, Status: estado, ExternalReference: p.ExternalReference}
		s.pagos[nuevo.ID] = nuevo
		s.mu.Unlock()

		veces := 1
		if r.URL.Query().Get("duplicar") == "1" {
			veces = 2
		}
		resultados := make([]int, veces)
		for i := range veces {
			resultados[i] = s.avisar(r.Context(), p.NotificationURL, nuevo.ID, i)
		}
		slog.Info("pago resuelto", "pago", nuevo.ID, "estado", estado, "venta", p.ExternalReference, "respuestas_al_aviso", resultados)
		// Desde el navegador, una página. Desde un script (fetch, curl), el detalle en JSON.
		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_ = resultado.Execute(w, nuevo)
			return
		}
		escribirJSON(w, http.StatusOK, map[string]any{"pago": nuevo, "respuestasAlAviso": resultados})
	}
}

// avisar envía el aviso firmado, como lo haría el proveedor. Devuelve el código HTTP recibido, o 0 si falló.
func (s *simulador) avisar(ctx context.Context, destino string, pagoID int64, intento int) int {
	id := strconv.FormatInt(pagoID, 10)
	pedidoID := fmt.Sprintf("simulador-%d-%d", pagoID, intento)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	cuerpo, _ := json.Marshal(map[string]any{"type": "payment", "data": map[string]any{"id": pagoID}})

	h := hmac.New(sha256.New, []byte(s.secreto))
	fmt.Fprintf(h, "id:%s;request-id:%s;ts:%s;", id, pedidoID, ts)

	pedido, err := http.NewRequestWithContext(ctx, http.MethodPost, destino, bytes.NewReader(cuerpo))
	if err != nil {
		return 0
	}
	pedido.Header.Set("Content-Type", "application/json")
	pedido.Header.Set("X-Request-Id", pedidoID)
	pedido.Header.Set("X-Signature", "ts="+ts+",v1="+hex.EncodeToString(h.Sum(nil)))
	res, err := s.cliente.Do(pedido)
	if err != nil {
		slog.Warn("no se pudo enviar el aviso", "destino", destino, "error", err)
		return 0
	}
	defer res.Body.Close()
	return res.StatusCode
}

func (s *simulador) consultarPago(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	s.mu.Lock()
	p, ok := s.pagos[id]
	s.mu.Unlock()
	if err != nil || !ok {
		http.Error(w, `{"message":"payment not found"}`, http.StatusNotFound)
		return
	}
	escribirJSON(w, http.StatusOK, p)
}

func escribirJSON(w http.ResponseWriter, codigo int, cuerpo any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(codigo)
	_ = json.NewEncoder(w).Encode(cuerpo)
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("servicio", "mercadopago-simulador"))
	s := &simulador{
		urlPublica:   os.Getenv("URL_PUBLICA"),
		secreto:      os.Getenv("MERCADOPAGO_WEBHOOK_SECRET"),
		cliente:      &http.Client{Timeout: 10 * time.Second},
		preferencias: map[string]preferencia{},
		pagos:        map[int64]pago{},
		// Los ids no se repiten entre reinicios: Ventas guarda los ya procesados y tomaría un id
		// reutilizado por un aviso repetido, ignorando un pago nuevo.
		siguiente: time.Now().UnixMilli(),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("POST /checkout/preferences", s.crearPreferencia)
	mux.HandleFunc("GET /checkout/{id}", s.mostrarCheckout)
	mux.HandleFunc("POST /checkout/{id}/aprobar", s.resolver("approved"))
	mux.HandleFunc("POST /checkout/{id}/rechazar", s.resolver("rejected"))
	mux.HandleFunc("GET /v1/payments/{id}", s.consultarPago)

	slog.Info("simulador de Mercado Pago escuchando", "direccion", ":8080")
	servidor := &http.Server{Addr: ":8080", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := servidor.ListenAndServe(); err != nil {
		slog.Error("el simulador terminó con error", "error", err)
		os.Exit(1)
	}
}
