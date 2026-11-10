package handler

import (
	"net/http"
	"slices"
	"strings"
)

// ConCORS deja que una página de otro origen (Swagger UI) llame a la API desde el navegador.
// origenes es la lista separada por comas de CORS_ORIGENES. Vacía no hace nada (D16): el gateway
// de FAV se encarga de CORS y si el servicio también mandara las cabeceras, el navegador
// rechazaría la respuesta.
//
// Envuelve al router y no es una ruta más: el preflight es un OPTIONS que ninguna de las rutas
// ("POST /v1/...") atendería.
func ConCORS(h http.Handler, origenes string) http.Handler {
	var permitidos []string
	for _, o := range strings.Split(origenes, ",") {
		if o = strings.TrimSpace(o); o != "" {
			permitidos = append(permitidos, o)
		}
	}
	if len(permitidos) == 0 {
		return h
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origen := r.Header.Get("Origin")
		if origen == "" || !slices.Contains(permitidos, origen) {
			h.ServeHTTP(w, r)
			return
		}
		w.Header().Add("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Origin", origen)

		// Preflight: el navegador pregunta si puede hacer el pedido antes de hacerlo.
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}
