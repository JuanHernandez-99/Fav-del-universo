package mysql

// Tests de integración contra un MySQL real. Necesitan la variable VENTAS_TEST_DSN; sin ella se saltean:
//
//	docker compose up -d mysql-ventas
//	VENTAS_TEST_DSN="root:cambiar@tcp(localhost:3308)/ventas_test?parseTime=true&loc=UTC&multiStatements=true" go test ./...

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"

	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/dominio"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/observabilidad"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/puertos"
)

func repoDePrueba(t *testing.T) (*Repositorio, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("VENTAS_TEST_DSN")
	if dsn == "" {
		t.Skip("VENTAS_TEST_DSN no definida: se saltea el test de integración")
	}
	db := baseDePrueba(t, dsn)
	t.Cleanup(func() { db.Close() })
	repo := NuevoRepositorio(db)
	if err := repo.Migrar(context.Background()); err != nil {
		t.Fatal(err)
	}
	return repo, db
}

// baseDePrueba abre una base propia de los tests y la crea si hace falta. No puede ser la del servicio:
// sus procesos corren contra esa base y se llevarían las tareas y los eventos de prueba.
func baseDePrueba(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DBName == "ventas" {
		t.Fatal("el DSN de los tests apunta a la base del servicio: usar ventas_test")
	}
	nombre := cfg.DBName
	cfg.DBName = ""
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := admin.Exec("CREATE DATABASE IF NOT EXISTS " + nombre); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func ventaNueva() dominio.Venta {
	ahora := time.Now().UTC().Truncate(time.Second)
	vence := ahora.Add(20 * time.Minute)
	return dominio.Venta{
		ID: uuid.NewString(), Clave: "web:" + uuid.NewString(), Huella: "huella", Canal: dominio.CanalOnline,
		Estado: dominio.EstadoPendiente, Medio: dominio.MedioMercadoPago, Email: "ana@example.com", ReservaID: uuid.NewString(),
		Items:   []dominio.Item{{VarianteID: "var-remera", Nombre: "Remera", Talle: "M", Color: "Blanco", Cantidad: 2, Precio: 1_500_000}},
		Total:   3_000_000,
		VenceEn: &vence, CreadaEn: ahora, ActualizadaEn: ahora,
	}
}

func crear(t *testing.T, r *Repositorio, v dominio.Venta) dominio.Venta {
	t.Helper()
	g, repetida, err := r.CrearVenta(context.Background(), v, nil, nil)
	if err != nil || repetida {
		t.Fatalf("creando la venta: %v, repetida %v", err, repetida)
	}
	return g
}

func contar(t *testing.T, db *sql.DB, consulta string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(consulta, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestGuardaYLeeLaVentaConSusItems(t *testing.T) {
	r, _ := repoDePrueba(t)
	v := crear(t, r, ventaNueva())
	g, err := r.ObtenerVenta(context.Background(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if g.Total != v.Total || len(g.Items) != 1 || g.Items[0].Precio != 1_500_000 || g.Items[0].Nombre != "Remera" || g.VenceEn == nil {
		t.Errorf("leída: %+v", g)
	}
	if _, err := r.ObtenerVenta(context.Background(), uuid.NewString()); !errors.Is(err, dominio.ErrVentaNoEncontrada) {
		t.Errorf("inexistente: %v", err)
	}
}

// Dos pedidos simultáneos con la misma clave: una sola venta, y los dos reciben la misma.
func TestCrearVentaConcurrenteConLaMismaClave(t *testing.T) {
	r, db := repoDePrueba(t)
	base := ventaNueva()
	var wg sync.WaitGroup
	var repetidas atomic.Int32
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v := base
			v.ID = uuid.NewString()
			g, repetida, err := r.CrearVenta(context.Background(), v, nil, []dominio.Mail{{Tipo: dominio.MailCompraRealizada, Destinatario: "a@b.c", Asunto: "a", Cuerpo: "b"}})
			if err != nil {
				t.Error(err)
				return
			}
			if g.Clave != base.Clave {
				t.Errorf("clave %s", g.Clave)
			}
			if repetida {
				repetidas.Add(1)
			}
		}()
	}
	wg.Wait()
	if n := contar(t, db, `SELECT COUNT(*) FROM ventas WHERE clave_idempotencia = ?`, base.Clave); n != 1 {
		t.Errorf("hay %d ventas", n)
	}
	if repetidas.Load() != 7 {
		t.Errorf("repetidas: %d", repetidas.Load())
	}
}

// Si el cambio falla, no se guarda nada: ni la venta, ni la tarea, ni el mail.
func TestModificarVentaEsAtomico(t *testing.T) {
	r, db := repoDePrueba(t)
	v := crear(t, r, ventaNueva())
	falla := errors.New("regla")
	_, err := r.ModificarVenta(context.Background(), v.ID, func(x *dominio.Venta) ([]dominio.TipoTarea, []dominio.Mail, error) {
		x.Estado = dominio.EstadoCancelada
		return []dominio.TipoTarea{dominio.TareaLiberar}, nil, falla
	})
	if !errors.Is(err, falla) {
		t.Fatal(err)
	}
	g, _ := r.ObtenerVenta(context.Background(), v.ID)
	if g.Estado != dominio.EstadoPendiente || contar(t, db, `SELECT COUNT(*) FROM tareas WHERE venta_id = ?`, v.ID) != 0 {
		t.Errorf("estado %s", g.Estado)
	}
}

// HU-11.6: el mismo aviso entregado muchas veces a la vez aplica el pago una sola vez.
func TestAvisoDePagoRepetidoEnParaleloSeAplicaUnaVez(t *testing.T) {
	r, db := repoDePrueba(t)
	v := crear(t, r, ventaNueva())
	pagoID := uuid.NewString()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := r.RegistrarAvisoPago(context.Background(), pagoID, v.ID, func(x *dominio.Venta) ([]dominio.TipoTarea, []dominio.Mail, error) {
				if x.RegistrarPago(time.Now()) == dominio.PagoAConfirmar {
					return []dominio.TipoTarea{dominio.TareaConfirmar}, nil, nil
				}
				return nil, nil, nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := contar(t, db, `SELECT COUNT(*) FROM tareas WHERE venta_id = ? AND tipo = 'CONFIRMAR'`, v.ID); n != 1 {
		t.Errorf("hay %d tareas de confirmar", n)
	}
	g, _ := r.ObtenerVenta(context.Background(), v.ID)
	if g.PagoAprobadoEn == nil {
		t.Error("falta el pago registrado")
	}
}

// El pago y el vencimiento coinciden: el bloqueo de la fila hace que gane uno y el otro vea el resultado.
func TestPagoYEventoDeVencimientoConcurrentes(t *testing.T) {
	r, _ := repoDePrueba(t)
	for range 10 {
		v := crear(t, r, ventaNueva())
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			err := r.RegistrarAvisoPago(context.Background(), uuid.NewString(), v.ID, func(x *dominio.Venta) ([]dominio.TipoTarea, []dominio.Mail, error) {
				x.RegistrarPago(time.Now())
				return nil, nil, nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			_, err := r.ProcesarEvento(context.Background(), uuid.NewString(), v.ID, func(x *dominio.Venta) ([]dominio.Mail, error) {
				x.Vencer(time.Now())
				return nil, nil
			})
			if err != nil {
				t.Error(err)
			}
		}()
		wg.Wait()
		g, _ := r.ObtenerVenta(context.Background(), v.ID)
		// Si el pago llegó primero sigue pendiente con pago; si llegó después, queda a verificar.
		valido := (g.Estado == dominio.EstadoPendiente && g.PagoAprobadoEn != nil) || g.Estado == dominio.EstadoAVerificar
		if !valido {
			t.Errorf("estado %s, pago %v", g.Estado, g.PagoAprobadoEn)
		}
	}
}

func TestEventoRepetidoSeAplicaUnaVezYSinVentaQuedaProcesado(t *testing.T) {
	r, _ := repoDePrueba(t)
	v := crear(t, r, ventaNueva())
	mensaje := uuid.NewString()
	aplicados := 0
	cambio := func(x *dominio.Venta) ([]dominio.Mail, error) {
		aplicados++
		x.Vencer(time.Now())
		return nil, nil
	}
	if ok, err := r.ProcesarEvento(context.Background(), mensaje, v.ID, cambio); err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	if ok, err := r.ProcesarEvento(context.Background(), mensaje, v.ID, cambio); err != nil || ok {
		t.Fatalf("repetido: %v %v", ok, err)
	}
	if aplicados != 1 {
		t.Errorf("aplicado %d veces", aplicados)
	}
	if ok, err := r.ProcesarEvento(context.Background(), uuid.NewString(), uuid.NewString(), cambio); err != nil || !ok {
		t.Errorf("sin venta: %v %v", ok, err)
	}
}

// Dos réplicas toman tareas a la vez: ninguna tarea se toma dos veces.
func TestTareasNoSeTomanDosVeces(t *testing.T) {
	r, db := repoDePrueba(t)
	if _, err := db.Exec(`UPDATE tareas SET estado = 'COMPLETADA'`); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		v := ventaNueva()
		if _, _, err := r.CrearVenta(context.Background(), v, []dominio.TipoTarea{dominio.TareaConfirmar}, nil); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	vistas := map[string]int{}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tareas, err := r.TomarTareas(context.Background(), time.Now(), time.Minute, 5)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, ta := range tareas {
				vistas[ta.ID]++
			}
		}()
	}
	wg.Wait()
	for id, n := range vistas {
		if n > 1 {
			t.Errorf("la tarea %s se tomó %d veces", id, n)
		}
	}

	// Apartadas: hasta que venza el plazo nadie más las ve, y después sí (la réplica pudo haberse caído).
	if ahora, _ := r.TomarTareas(context.Background(), time.Now(), time.Minute, 50); len(ahora) != 10-len(vistas) {
		t.Errorf("quedaban %d libres, llegaron %d", 10-len(vistas), len(ahora))
	}
	despues, err := r.TomarTareas(context.Background(), time.Now().Add(2*time.Minute), time.Minute, 50)
	if err != nil || len(despues) != 10 {
		t.Errorf("después del plazo: %d, %v", len(despues), err)
	}
	if err := r.CompletarTarea(context.Background(), despues[0].ID); err != nil {
		t.Fatal(err)
	}
	if n := contar(t, db, `SELECT COUNT(*) FROM tareas WHERE id = ? AND estado = 'COMPLETADA'`, despues[0].ID); n != 1 {
		t.Error("la tarea debía quedar completada")
	}
}

func TestCicloDeUnMail(t *testing.T) {
	r, db := repoDePrueba(t)
	if _, err := db.Exec(`UPDATE mails SET estado = 'ENVIADO'`); err != nil {
		t.Fatal(err)
	}
	v := ventaNueva()
	if _, _, err := r.CrearVenta(context.Background(), v, nil, []dominio.Mail{{Tipo: dominio.MailCompraRealizada, Destinatario: "ana@example.com", Asunto: "Compra", Cuerpo: "Hola"}}); err != nil {
		t.Fatal(err)
	}
	mails, err := r.TomarMails(context.Background(), time.Now(), time.Minute, 10)
	if err != nil || len(mails) != 1 {
		t.Fatalf("%d mails: %v", len(mails), err)
	}
	m := mails[0]
	if err := r.ReprogramarMail(context.Background(), m.ID, "smtp caído", time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	mails, _ = r.TomarMails(context.Background(), time.Now(), time.Minute, 10)
	if len(mails) != 1 || mails[0].Intentos != 1 {
		t.Fatalf("reprogramado: %+v", mails)
	}
	if err := r.MarcarMailFallido(context.Background(), m.ID, "agotado"); err != nil {
		t.Fatal(err)
	}
	if n := contar(t, db, `SELECT COUNT(*) FROM mails WHERE id = ? AND estado = 'FALLIDO'`, m.ID); n != 1 {
		t.Error("debía quedar fallido")
	}
}

func TestListarFiltraPorEstadoYComprador(t *testing.T) {
	r, _ := repoDePrueba(t)
	comprador := uuid.NewString()
	v := ventaNueva()
	v.CompradorID = comprador
	crear(t, r, v)
	crear(t, r, ventaNueva())
	mias, err := r.ListarVentas(context.Background(), puertos.FiltroVentas{CompradorID: comprador, Limite: 10})
	if err != nil || len(mias) != 1 || mias[0].ID != v.ID || len(mias[0].Items) != 1 {
		t.Errorf("mías: %+v, %v", mias, err)
	}
	ninguna, err := r.ListarVentas(context.Background(), puertos.FiltroVentas{CompradorID: comprador, Estado: dominio.EstadoPagada, Limite: 10})
	if err != nil || len(ninguna) != 0 {
		t.Errorf("pagadas: %+v, %v", ninguna, err)
	}
}

// La venta guarda el track id del pedido que la creó, y sus tareas y mails lo copian aunque se generen en otro
// pedido, como el aviso de pago (ADR-020). La migración se puede aplicar dos veces.
func TestLaVentaSusTareasYSusMailsGuardanElTrackIDDeLaCompra(t *testing.T) {
	r, _ := repoDePrueba(t)
	if err := r.Migrar(context.Background()); err != nil {
		t.Fatalf("la migración no es idempotente: %v", err)
	}
	ctxCompra := observabilidad.ConCorrelationID(context.Background(), "compra-remera-1")
	v, _, err := r.CrearVenta(ctxCompra, ventaNueva(), nil, []dominio.Mail{{Tipo: dominio.MailCompraRealizada, Destinatario: "ana@example.com", Asunto: "a", Cuerpo: "c"}})
	if err != nil {
		t.Fatal(err)
	}
	ctxAviso := observabilidad.ConCorrelationID(context.Background(), "aviso-mercadopago-9")
	err = r.RegistrarAvisoPago(ctxAviso, "pago-"+uuid.NewString(), v.ID, func(*dominio.Venta) ([]dominio.TipoTarea, []dominio.Mail, error) {
		return []dominio.TipoTarea{dominio.TareaConfirmar}, nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if g, err := r.ObtenerVenta(context.Background(), v.ID); err != nil || g.TrackID != "compra-remera-1" {
		t.Fatalf("venta.TrackID = %q (%v)", g.TrackID, err)
	}
	tareas, err := r.TomarTareas(context.Background(), time.Now().Add(time.Minute), time.Minute, 100)
	if err != nil {
		t.Fatal(err)
	}
	mails, err := r.TomarMails(context.Background(), time.Now().Add(time.Minute), time.Minute, 100)
	if err != nil {
		t.Fatal(err)
	}
	var tareaOK, mailOK bool
	for _, ta := range tareas {
		if ta.VentaID == v.ID {
			tareaOK = ta.TrackID == "compra-remera-1"
		}
	}
	for _, m := range mails {
		if m.VentaID == v.ID {
			mailOK = m.TrackID == "compra-remera-1"
		}
	}
	if !tareaOK || !mailOK {
		t.Errorf("la tarea (%v) y el mail (%v) tienen que llevar el track id de la compra", tareaOK, mailOK)
	}
}
