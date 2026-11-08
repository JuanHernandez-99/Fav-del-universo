package gimnasio

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/dominio"
)

const idValido = "2ea07178-156c-41a8-b682-c2849ff687ca"

func clienteContra(t *testing.T, atender http.HandlerFunc) *Cliente {
	t.Helper()
	servidor := httptest.NewServer(atender)
	t.Cleanup(servidor.Close)
	return NuevoCliente(Config{URLBase: servidor.URL, APIKey: "clave-de-prueba", ProveedorID: "fav", Timeout: 200 * time.Millisecond})
}

func reposicionDePrueba() dominio.Reposicion {
	return dominio.Reposicion{ID: "entrega-0001", ClaveIdempotencia: "clave-0001", VarianteID: "var-remera-basica-blanca-m", Cantidad: 5}
}

func TestClasificaCadaRespuestaDelGimnasio(t *testing.T) {
	error409 := func(codigo string) string { return `{"codigo":"` + codigo + `","mensaje":"x"}` }
	casos := []struct {
		nombre string
		estado int
		cuerpo string
		espera error // nil con idEsperado es éxito; errIncierto es cualquier error reintentable
	}{
		{"recibida", 201, `{"reposicionId":"` + idValido + `","estado":"RECIBIDA"}`, nil},
		{"400 solicitud", 400, error409("SOLICITUD_INVALIDA"), dominio.ErrReposicionRechazada},
		{"400 idempotencia", 400, error409("IDEMPOTENCIA_INVALIDA"), dominio.ErrReposicionRechazada},
		{"413", 413, error409("SOLICITUD_DEMASIADO_GRANDE"), dominio.ErrReposicionRechazada},
		{"415", 415, error409("TIPO_CONTENIDO_NO_ADMITIDO"), dominio.ErrReposicionRechazada},
		{"422", 422, error409("STOCK_NO_REPRESENTABLE"), dominio.ErrReposicionRechazada},
		{"401", 401, error409("NO_AUTENTICADO"), dominio.ErrGimnasioConfiguracion},
		{"403", 403, error409("PROVEEDOR_NO_AUTORIZADO"), dominio.ErrGimnasioConfiguracion},
		{"409 clave", 409, error409("CLAVE_REUTILIZADA"), dominio.ErrReposicionConflicto},
		{"409 referencia", 409, error409("REFERENCIA_REUTILIZADA"), dominio.ErrReposicionConflicto},
		{"400 sin código, de un proxy", 400, `<html>bad request</html>`, errIncierto},
		{"404 de ruta", 404, `not found`, errIncierto},
		{"408", 408, ``, errIncierto},
		{"429", 429, ``, errIncierto},
		{"500", 500, error409("ERROR_INTERNO"), errIncierto},
		{"503", 503, error409("SERVICIO_NO_DISPONIBLE"), errIncierto},
		{"201 sin id", 201, `{"estado":"RECIBIDA"}`, errIncierto},
		{"201 con otro estado", 201, `{"reposicionId":"` + idValido + `","estado":"OTRO"}`, errIncierto},
		{"201 con id que no es uuid", 201, `{"reposicionId":"abc","estado":"RECIBIDA"}`, errIncierto},
		{"200 en lugar de 201", 200, `{"reposicionId":"` + idValido + `","estado":"RECIBIDA"}`, errIncierto},
		{"redirección", 302, ``, errIncierto},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			cliente := clienteContra(t, func(w http.ResponseWriter, _ *http.Request) {
				if c.estado == 302 {
					w.Header().Set("Location", "/otra")
				}
				w.WriteHeader(c.estado)
				_, _ = w.Write([]byte(c.cuerpo))
			})
			id, err := cliente.EnviarReposicion(context.Background(), reposicionDePrueba())
			switch c.espera {
			case nil:
				if err != nil || id != idValido {
					t.Fatalf("id %q, error %v", id, err)
				}
			case errIncierto:
				if err == nil || esDecisionDefinitiva(err) {
					t.Fatalf("tenía que ser incierto y dio: %v", err)
				}
			default:
				if !errors.Is(err, c.espera) {
					t.Fatalf("se esperaba %v y dio %v", c.espera, err)
				}
			}
		})
	}
}

var errIncierto = errors.New("incierto")

func esDecisionDefinitiva(err error) bool {
	return errors.Is(err, dominio.ErrReposicionRechazada) || errors.Is(err, dominio.ErrReposicionConflicto) || errors.Is(err, dominio.ErrGimnasioConfiguracion)
}

func TestUnTimeoutEsIncierto(t *testing.T) {
	cliente := clienteContra(t, func(http.ResponseWriter, *http.Request) { time.Sleep(400 * time.Millisecond) })
	_, err := cliente.EnviarReposicion(context.Background(), reposicionDePrueba())
	if err == nil || esDecisionDefinitiva(err) {
		t.Fatalf("tenía que ser incierto y dio: %v", err)
	}
}

func TestReintentarMandaLaMismaClaveYElMismoCuerpo(t *testing.T) {
	type envio struct{ clave, cuerpo string }
	var envios []envio
	cliente := clienteContra(t, func(w http.ResponseWriter, r *http.Request) {
		cuerpo := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(cuerpo)
		envios = append(envios, envio{r.Header.Get("Idempotency-Key"), string(cuerpo)})
		w.WriteHeader(503)
	})
	for range 2 {
		_, _ = cliente.EnviarReposicion(context.Background(), reposicionDePrueba())
	}
	if len(envios) != 2 || envios[0] != envios[1] || envios[0].clave != "clave-0001" {
		t.Fatalf("los reintentos cambiaron: %+v", envios)
	}
}

func TestLosRechazosDeNegocioNoAbrenElCircuito(t *testing.T) {
	cliente := clienteContra(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"codigo":"REFERENCIA_REUTILIZADA","mensaje":"x"}`))
	})
	for range 8 {
		if _, err := cliente.EnviarReposicion(context.Background(), reposicionDePrueba()); !errors.Is(err, dominio.ErrReposicionConflicto) {
			t.Fatalf("el circuito no tenía que abrirse: %v", err)
		}
	}
}

func TestLasCaidasAbrenElCircuito(t *testing.T) {
	llamadas := 0
	cliente := clienteContra(t, func(w http.ResponseWriter, _ *http.Request) { llamadas++; w.WriteHeader(503) })
	for range 8 {
		_, _ = cliente.EnviarReposicion(context.Background(), reposicionDePrueba())
	}
	if llamadas != 5 {
		t.Fatalf("con el circuito abierto no se llama al gimnasio: hubo %d llamadas", llamadas)
	}
}

func TestStockVacioEsDistintoDeUnaFalla(t *testing.T) {
	casos := []struct {
		nombre string
		estado int
		cuerpo string
		falla  bool
	}{
		{"sin recepciones", 200, `{"proveedorId":"fav","items":[]}`, false},
		{"cuerpo vacío", 200, `{}`, true},
		{"sin items", 200, `{"proveedorId":"fav"}`, true},
		{"otro proveedor", 200, `{"proveedorId":"otro","items":[]}`, true},
		{"campo renombrado", 200, `{"proveedorId":"fav","items":[{"codigo":"v","unidadesDisponibles":1,"unidadesVendidas":0}]}`, true},
		{"sin vendidas", 200, `{"proveedorId":"fav","items":[{"productoCodigo":"v","unidadesDisponibles":1}]}`, true},
		{"negativo", 200, `{"proveedorId":"fav","items":[{"productoCodigo":"v","unidadesDisponibles":-1,"unidadesVendidas":0}]}`, true},
		{"503", 503, `{"codigo":"SERVICIO_NO_DISPONIBLE","mensaje":"x"}`, true},
		{"401", 401, `{"codigo":"NO_AUTENTICADO","mensaje":"x"}`, true},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			cliente := clienteContra(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(c.estado)
				_, _ = w.Write([]byte(c.cuerpo))
			})
			stock, err := cliente.ConsultarStock(context.Background())
			if c.falla {
				if !errors.Is(err, dominio.ErrProveedorNoDisponible) {
					t.Fatalf("una falla tiene que informarse como no disponible: %v, %v", stock, err)
				}
				return
			}
			if err != nil || stock == nil || len(stock) != 0 {
				t.Fatalf("items vacío es una lista vacía: %v, %v", stock, err)
			}
		})
	}
}
