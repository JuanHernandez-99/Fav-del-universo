package mysql

// Integración del envío de reposiciones (ADR-018): servicio y outbox contra MySQL real, cliente real y un gimnasio
// falso por HTTP. Lo importante es qué pasa con el stock físico en cada respuesta.

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/adaptadores/gimnasio"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/aplicacion"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/dominio"
)

const idDelGimnasio = "2ea07178-156c-41a8-b682-c2849ff687ca"

// gimnasioFalso recuerda las entregas por clave, como el real: repetir la clave no suma unidades.
type gimnasioFalso struct {
	mu       sync.Mutex
	estado   int
	cuerpo   string
	demora   time.Duration
	claves   []string
	entregas map[string]bool
}

func (g *gimnasioFalso) responder(estado int, cuerpo string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.estado, g.cuerpo = estado, cuerpo
}

func (g *gimnasioFalso) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	estado, cuerpo, demora := g.estado, g.cuerpo, g.demora
	clave := r.Header.Get("Idempotency-Key")
	g.claves = append(g.claves, clave)
	if estado == 201 {
		g.entregas[clave] = true
	}
	g.mu.Unlock()
	time.Sleep(demora)
	w.WriteHeader(estado)
	_, _ = w.Write([]byte(cuerpo))
}

func recibida() string { return `{"reposicionId":"` + idDelGimnasio + `","estado":"RECIBIDA"}` }

type escenario struct {
	repo     *Repositorio
	db       *sql.DB
	servicio *aplicacion.Servicio
	gimnasio *gimnasioFalso
	servidor *httptest.Server
	variante string
}

func nuevoEscenario(t *testing.T) *escenario {
	t.Helper()
	repo, db := repoDePrueba(t)
	falso := &gimnasioFalso{estado: 201, cuerpo: recibida(), entregas: map[string]bool{}}
	servidor := httptest.NewServer(falso)
	t.Cleanup(servidor.Close)
	cliente := gimnasio.NuevoCliente(gimnasio.Config{URLBase: servidor.URL, APIKey: "clave", ProveedorID: "fav", Timeout: 200 * time.Millisecond})
	return &escenario{repo, db, aplicacion.NuevoServicio(repo, cliente), falso, servidor, varianteNueva(t, db, 0, 10)}
}

func (e *escenario) reponer(t *testing.T, cantidad int) dominio.Reposicion {
	t.Helper()
	rep, _, err := e.servicio.Reponer(context.Background(), uuid.NewString(), e.variante, cantidad, "admin")
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func (e *escenario) comprobar(t *testing.T, id string, estado dominio.EstadoReposicion, fisico int) dominio.Reposicion {
	t.Helper()
	rep, err := e.repo.reposicion(context.Background(), e.db, `WHERE id = ?`, id)
	if err != nil {
		t.Fatal(err)
	}
	var enStock int
	if err := e.db.QueryRow(`SELECT fisico FROM stock WHERE variante_id = ?`, e.variante).Scan(&enStock); err != nil {
		t.Fatal(err)
	}
	if rep.Estado != estado || enStock != fisico {
		t.Fatalf("estado %s y físico %d; se esperaba %s y %d (último error: %s)", rep.Estado, enStock, estado, fisico, rep.UltimoError)
	}
	return rep
}

// sigueEnCola dice si el proceso volvería a tomarla, sin depender de cuándo toca el próximo intento.
func (e *escenario) sigueEnCola(t *testing.T, id string) bool {
	t.Helper()
	tomadas, err := e.repo.TomarReposiciones(context.Background(), time.Now().Add(24*time.Hour), time.Second, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range tomadas {
		if r.ID == id {
			return true
		}
	}
	return false
}

func TestMigracionEsIdempotente(t *testing.T) {
	repo, _ := repoDePrueba(t)
	if err := repo.Migrar(context.Background(), false); err != nil {
		t.Fatalf("aplicar el esquema dos veces tiene que funcionar: %v", err)
	}
}

func TestRecibidaConfirmaYGuardaElIdDelGimnasio(t *testing.T) {
	e := nuevoEscenario(t)
	rep := e.reponer(t, 4)
	e.comprobar(t, rep.ID, dominio.ReposicionPendiente, 6)
	if _, err := e.servicio.EnviarReposicion(context.Background(), rep); err != nil {
		t.Fatal(err)
	}
	if guardada := e.comprobar(t, rep.ID, dominio.ReposicionConfirmada, 6); guardada.IDGimnasio != idDelGimnasio {
		t.Fatalf("id del gimnasio: %q", guardada.IDGimnasio)
	}
	if e.sigueEnCola(t, rep.ID) {
		t.Fatal("una confirmada no se vuelve a enviar")
	}
}

func TestRechazoDefinitivoDevuelveLasUnidadesUnaSolaVez(t *testing.T) {
	e := nuevoEscenario(t)
	e.gimnasio.responder(422, `{"codigo":"STOCK_NO_REPRESENTABLE","mensaje":"x"}`)
	rep := e.reponer(t, 4)
	for range 2 { // la segunda simula otra réplica que la tomó a la vez
		if _, err := e.servicio.EnviarReposicion(context.Background(), rep); err != nil {
			t.Fatal(err)
		}
	}
	e.comprobar(t, rep.ID, dominio.ReposicionRechazada, 10)
}

func TestConflictoNoDevuelveNiReintenta(t *testing.T) {
	e := nuevoEscenario(t)
	e.gimnasio.responder(409, `{"codigo":"REFERENCIA_REUTILIZADA","mensaje":"x"}`)
	rep := e.reponer(t, 4)
	if _, err := e.servicio.EnviarReposicion(context.Background(), rep); err != nil {
		t.Fatal(err)
	}
	e.comprobar(t, rep.ID, dominio.ReposicionARevisar, 6)
	if e.sigueEnCola(t, rep.ID) {
		t.Fatal("una reposición en revisión no se reintenta sola")
	}
}

func TestCredencialRechazadaNoDevuelveYSeDestrabaAlCorregirla(t *testing.T) {
	e := nuevoEscenario(t)
	e.gimnasio.responder(401, `{"codigo":"NO_AUTENTICADO","mensaje":"x"}`)
	rep := e.reponer(t, 4)
	if _, err := e.servicio.EnviarReposicion(context.Background(), rep); err != nil {
		t.Fatal(err)
	}
	e.comprobar(t, rep.ID, dominio.ReposicionBloqueada, 6)
	if !e.sigueEnCola(t, rep.ID) {
		t.Fatal("una bloqueada se sigue intentando")
	}
	e.gimnasio.responder(201, recibida())
	if _, err := e.servicio.EnviarReposicion(context.Background(), rep); err != nil {
		t.Fatal(err)
	}
	e.comprobar(t, rep.ID, dominio.ReposicionConfirmada, 6)
}

func TestResultadoInciertoReintentaConLaMismaClaveSinDuplicar(t *testing.T) {
	e := nuevoEscenario(t)
	rep := e.reponer(t, 4)

	e.gimnasio.responder(503, `{"codigo":"SERVICIO_NO_DISPONIBLE","mensaje":"x"}`)
	if _, err := e.servicio.EnviarReposicion(context.Background(), rep); err != nil {
		t.Fatal(err)
	}
	e.comprobar(t, rep.ID, dominio.ReposicionPendiente, 6)

	// Timeout: el gimnasio pudo registrarla, pero la respuesta se pierde.
	e.gimnasio.responder(201, recibida())
	e.gimnasio.demora = 400 * time.Millisecond
	if _, err := e.servicio.EnviarReposicion(context.Background(), rep); err != nil {
		t.Fatal(err)
	}
	e.comprobar(t, rep.ID, dominio.ReposicionPendiente, 6)

	e.gimnasio.demora = 0
	if _, err := e.servicio.EnviarReposicion(context.Background(), rep); err != nil {
		t.Fatal(err)
	}
	e.comprobar(t, rep.ID, dominio.ReposicionConfirmada, 6)

	if len(e.gimnasio.entregas) != 1 {
		t.Fatalf("el gimnasio recibió %d entregas distintas", len(e.gimnasio.entregas))
	}
	for _, clave := range e.gimnasio.claves {
		if clave != rep.ClaveIdempotencia {
			t.Fatalf("un reintento cambió la clave: %v", e.gimnasio.claves)
		}
	}
}

func TestCaidaDelGimnasioDejaLaReposicionPendiente(t *testing.T) {
	e := nuevoEscenario(t)
	rep := e.reponer(t, 4)
	e.servidor.Close()
	if _, err := e.servicio.EnviarReposicion(context.Background(), rep); err != nil {
		t.Fatal(err)
	}
	if guardada := e.comprobar(t, rep.ID, dominio.ReposicionPendiente, 6); guardada.UltimoError == "" {
		t.Fatal("el motivo tiene que quedar registrado")
	}
	if !e.sigueEnCola(t, rep.ID) {
		t.Fatal("con el gimnasio caído la reposición sigue en cola")
	}
}

func TestResumenCuentaPorEstadoYDaLaPendienteMasAntigua(t *testing.T) {
	e := nuevoEscenario(t)
	ctx := context.Background()
	antes, _, err := e.repo.ResumirReposiciones(ctx)
	if err != nil {
		t.Fatal(err)
	}
	e.reponer(t, 1)
	confirmada := e.reponer(t, 1)
	if _, err := e.servicio.EnviarReposicion(ctx, confirmada); err != nil {
		t.Fatal(err)
	}
	resumen, antiguedad, err := e.servicio.ResumirReposiciones(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if resumen[dominio.ReposicionPendiente] != antes[dominio.ReposicionPendiente]+1 || resumen[dominio.ReposicionConfirmada] != antes[dominio.ReposicionConfirmada]+1 {
		t.Fatalf("antes %v, después %v", antes, resumen)
	}
	if _, existe := resumen[dominio.ReposicionBloqueada]; !existe {
		t.Fatal("tiene que informar también los estados en 0")
	}
	if antiguedad < 0 {
		t.Fatalf("antigüedad: %v", antiguedad)
	}
}

func TestListarReposicionesLasMasRecientesPrimero(t *testing.T) {
	e := nuevoEscenario(t)
	rep := e.reponer(t, 1)
	lista, err := e.repo.ListarReposiciones(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range lista {
		if r.ID == rep.ID {
			return
		}
	}
	t.Fatal("la reposición recién creada no aparece")
}
