package observabilidad

// Tests del escritor de logs en MySQL (ADR-020), con un destino falso en lugar de GORM. Mismo archivo en los
// cuatro servicios.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// destinoFalso guarda las tandas que recibe. Con falla, las rechaza; con bloquear, no vuelve nunca (ni con el ctx
// vencido), como una base colgada.
type destinoFalso struct {
	mu       sync.Mutex
	tandas   [][]RegistroLog
	falla    error
	bloquear bool
}

func (d *destinoFalso) Guardar(ctx context.Context, tanda []RegistroLog) error {
	if d.bloquear {
		select {}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tandas = append(d.tandas, append([]RegistroLog(nil), tanda...))
	return d.falla
}

func (d *destinoFalso) tamanos() []int {
	d.mu.Lock()
	defer d.mu.Unlock()
	var n []int
	for _, t := range d.tandas {
		n = append(n, len(t))
	}
	return n
}

func (d *destinoFalso) recibidas() int {
	total := 0
	for _, n := range d.tamanos() {
		total += n
	}
	return total
}

// avisosSeguros es un buffer que se puede leer mientras el escritor escribe.
type avisosSeguros struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (a *avisosSeguros) Write(p []byte) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.b.Write(p)
}

func (a *avisosSeguros) String() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.b.String()
}

func lineaDeRemera(i int) RegistroLog {
	return RegistroLog{Stage: "catalogo", Level: "info", Message: "pedido http", TrackID: "compra-remera", ID: uint64(i)}
}

func esperar(t *testing.T, condicion func() bool) {
	t.Helper()
	limite := time.Now().Add(2 * time.Second)
	for !condicion() {
		if time.Now().After(limite) {
			t.Fatal("la condición no se cumplió a tiempo")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestEscritorArmaTandasPorTamanoYAlCerrar(t *testing.T) {
	destino := &destinoFalso{}
	e := nuevoEscritor(destino, 100, 3, time.Hour, &avisosSeguros{}) // sin reloj: solo cuenta el tamaño
	go e.correr()

	for i := range 7 {
		e.encolar(lineaDeRemera(i))
	}
	esperar(t, func() bool { return len(destino.tamanos()) == 2 })
	e.cerrar(time.Second)

	if got := destino.tamanos(); len(got) != 3 || got[0] != 3 || got[1] != 3 || got[2] != 1 {
		t.Fatalf("tandas = %v, esperaba [3 3 1]: dos llenas y la última al cerrar", got)
	}
}

func TestEscritorGuardaCadaIntervaloAunqueLaTandaNoEsteLlena(t *testing.T) {
	destino := &destinoFalso{}
	e := nuevoEscritor(destino, 100, 200, 20*time.Millisecond, &avisosSeguros{})
	go e.correr()
	defer e.cerrar(time.Second)

	e.encolar(lineaDeRemera(1))
	e.encolar(lineaDeRemera(2))
	esperar(t, func() bool { return destino.recibidas() == 2 })
}

func TestEscritorConLaBaseCaidaNoBloqueaYDescarta(t *testing.T) {
	destino := &destinoFalso{falla: errors.New("dial tcp mysql-logs:3306: connection refused")}
	avisos := &avisosSeguros{}
	antes := testutil.ToFloat64(logsDescartados.WithLabelValues("base_no_disponible"))
	e := nuevoEscritor(destino, 10_000, 200, 10*time.Millisecond, avisos)
	go e.correr()

	inicio := time.Now()
	for i := range 1000 {
		e.encolar(lineaDeRemera(i))
	}
	if d := time.Since(inicio); d > 200*time.Millisecond {
		t.Fatalf("encolar 1000 líneas tardó %v: no debería esperar a la base", d)
	}
	esperar(t, func() bool { return destino.recibidas() == 1000 })
	e.cerrar(time.Second)

	if got := testutil.ToFloat64(logsDescartados.WithLabelValues("base_no_disponible")) - antes; got != 1000 {
		t.Errorf("descartadas por base caída = %v, esperaba 1000", got)
	}
	if !strings.Contains(avisos.String(), "no se pudo guardar en la base") {
		t.Errorf("esperaba un aviso por stderr, hubo: %q", avisos.String())
	}
	if n := strings.Count(avisos.String(), "no se pudo guardar"); n != 1 {
		t.Errorf("hubo %d avisos: con la base caída se avisa una vez y después cada %v", n, avisoRepetido)
	}
}

func TestEscritorAvisaCuandoLaBaseVuelve(t *testing.T) {
	destino := &destinoFalso{falla: errors.New("connection refused")}
	avisos := &avisosSeguros{}
	e := nuevoEscritor(destino, 100, 1, time.Hour, avisos)
	go e.correr()

	e.encolar(lineaDeRemera(1))
	esperar(t, func() bool { return destino.recibidas() == 1 })
	destino.mu.Lock()
	destino.falla = nil
	destino.mu.Unlock()
	e.encolar(lineaDeRemera(2))
	esperar(t, func() bool { return destino.recibidas() == 2 })
	e.cerrar(time.Second)

	if !strings.Contains(avisos.String(), "volvió a responder; mientras no respondía se descartaron 1 líneas") {
		t.Errorf("avisos = %q", avisos.String())
	}
}

func TestEscritorConLaColaLlenaCuentaDescartes(t *testing.T) {
	antes := testutil.ToFloat64(logsDescartados.WithLabelValues("cola_llena"))
	e := nuevoEscritor(&destinoFalso{}, 2, 200, time.Hour, &avisosSeguros{}) // sin correr: nadie vacía la cola

	inicio := time.Now()
	for i := range 5 {
		e.encolar(lineaDeRemera(i))
	}
	if d := time.Since(inicio); d > 50*time.Millisecond {
		t.Fatalf("con la cola llena encolar tardó %v: tiene que descartar sin esperar", d)
	}
	if got := testutil.ToFloat64(logsDescartados.WithLabelValues("cola_llena")) - antes; got != 3 {
		t.Errorf("descartadas por cola llena = %v, esperaba 3", got)
	}
}

func TestCerrarRespetaElTiempoMaximoConLaBaseColgada(t *testing.T) {
	avisos := &avisosSeguros{}
	e := nuevoEscritor(&destinoFalso{bloquear: true}, 100, 200, time.Hour, avisos)
	go e.correr()
	e.encolar(lineaDeRemera(1))

	inicio := time.Now()
	e.cerrar(100 * time.Millisecond)
	if d := time.Since(inicio); d > 500*time.Millisecond {
		t.Fatalf("cerrar tardó %v con un límite de 100 ms", d)
	}
	if !strings.Contains(avisos.String(), "sin terminar de guardar") {
		t.Errorf("esperaba el aviso de cierre incompleto, hubo: %q", avisos.String())
	}
}

func TestCerrarDescartaLoQueNoEntraEnElTiempo(t *testing.T) {
	antes := testutil.ToFloat64(logsDescartados.WithLabelValues("apagado"))
	destino := &destinoFalso{}
	e := nuevoEscritor(destino, 100, 200, time.Hour, &avisosSeguros{})
	e.encolar(lineaDeRemera(1))
	e.encolar(lineaDeRemera(2))
	go e.correr()

	e.limiteCierre = time.Now().Add(-time.Second) // ya vencido: no hay tiempo para guardar
	close(e.terminar)
	<-e.listo

	if destino.recibidas() != 0 {
		t.Errorf("con el límite vencido no debería guardar, guardó %d", destino.recibidas())
	}
	if got := testutil.ToFloat64(logsDescartados.WithLabelValues("apagado")) - antes; got != 2 {
		t.Errorf("descartadas al apagar = %v, esperaba 2", got)
	}
}

// Los avisos del escritor van a su propio io.Writer, nunca a slog: si pasaran por slog, cada falla generaría una
// línea nueva para la misma base caída y el escritor se alimentaría a sí mismo.
func TestLosErroresDelEscritorNoVuelvenAEntrarAlEscritor(t *testing.T) {
	destino := &destinoFalso{falla: errors.New("connection refused")}
	avisos := &avisosSeguros{}
	e := nuevoEscritor(destino, 100, 1, 10*time.Millisecond, avisos)
	go e.correr()

	var stdout bytes.Buffer
	logger := slog.New(nuevoManejadorMySQL(slog.NewJSONHandler(&stdout, nil), e, "catalogo", "replica-1"))
	anterior := slog.Default()
	slog.SetDefault(logger)
	defer slog.SetDefault(anterior)

	logger.Info("pedido http", "ruta", "/v1/tienda/productos")
	esperar(t, func() bool { return destino.recibidas() == 1 })
	time.Sleep(50 * time.Millisecond) // varios ciclos del reloj
	e.cerrar(time.Second)

	if got := destino.recibidas(); got != 1 {
		t.Errorf("el escritor recibió %d líneas: sus propios errores volvieron a entrar", got)
	}
	if strings.Contains(stdout.String(), "no se pudo guardar") {
		t.Error("el aviso del escritor salió por slog")
	}
	if !strings.Contains(avisos.String(), "no se pudo guardar") {
		t.Error("el aviso del escritor tenía que salir por stderr")
	}
}

func TestManejadorArmaLaFilaConTrackIDYAtributos(t *testing.T) {
	destino := &destinoFalso{}
	e := nuevoEscritor(destino, 100, 200, time.Hour, &avisosSeguros{})
	go e.correr()

	var stdout bytes.Buffer
	interno := manejadorConContexto{slog.NewJSONHandler(&stdout, nil)}
	logger := slog.New(nuevoManejadorMySQL(interno, e, "catalogo", "replica-1")).With("servicio", "catalogo", "instancia", "replica-1")
	ctx := ConCorrelationID(context.Background(), "compra-remera-42")

	logger.DebugContext(ctx, "detalle que no se guarda")
	logger.WarnContext(ctx, "no se pudo indexar; se reintenta", "producto_id", "prd-remera-saturno", "intento", 2,
		"error", errors.New("elasticsearch no responde"), slog.Group("variante", "talle", "M", "color", "Lila"))
	e.cerrar(time.Second)

	if destino.recibidas() != 1 {
		t.Fatalf("se guardaron %d filas, esperaba 1 (DEBUG no se guarda)", destino.recibidas())
	}
	fila := destino.tandas[0][0]
	if fila.TrackID != "compra-remera-42" || fila.Stage != "catalogo" || fila.Instancia != "replica-1" || fila.Level != "warn" ||
		fila.Message != "no se pudo indexar; se reintenta" || fila.CreatedAt.Location() != time.UTC {
		t.Errorf("fila = %+v", fila)
	}
	var atributos map[string]any
	if err := json.Unmarshal([]byte(fila.Atributos), &atributos); err != nil {
		t.Fatalf("atributos no es JSON: %q", fila.Atributos)
	}
	if atributos["producto_id"] != "prd-remera-saturno" || atributos["error"] != "elasticsearch no responde" || atributos["intento"] != float64(2) {
		t.Errorf("atributos = %v", atributos)
	}
	if v, _ := atributos["variante"].(map[string]any); v["talle"] != "M" {
		t.Errorf("el grupo variante no quedó anidado: %v", atributos)
	}
	for _, clave := range []string{"correlation_id", "servicio", "instancia"} {
		if _, esta := atributos[clave]; esta {
			t.Errorf("%s tiene su columna y no debería repetirse en atributos", clave)
		}
	}
	if !strings.Contains(stdout.String(), `"correlation_id":"compra-remera-42"`) {
		t.Errorf("la línea tiene que seguir saliendo por stdout igual que antes: %s", stdout.String())
	}
}
