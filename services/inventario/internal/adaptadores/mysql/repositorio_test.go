package mysql

// Tests de integración contra un MySQL real (ADR-003, validación). Necesitan la variable
// INVENTARIO_TEST_DSN; sin ella se saltean. Con Docker Compose:
//
//	docker compose up -d mysql-inventario
//	INVENTARIO_TEST_DSN="root:cambiar@tcp(localhost:3307)/inventario_test?parseTime=true&loc=UTC&multiStatements=true" go test ./...

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/dominio"
)

func repoDePrueba(t *testing.T) (*Repositorio, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("INVENTARIO_TEST_DSN")
	if dsn == "" {
		t.Skip("INVENTARIO_TEST_DSN no definida: se saltea el test de integración")
	}
	db := baseDePrueba(t, dsn)
	t.Cleanup(func() { db.Close() })
	repo := NuevoRepositorio(db)
	if err := repo.Migrar(context.Background(), false); err != nil {
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
	if cfg.DBName == "inventario" {
		t.Fatal("el DSN de los tests apunta a la base del servicio: usar inventario_test")
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

func varianteNueva(t *testing.T, db *sql.DB, online, fisico int) string {
	t.Helper()
	id := "test-" + uuid.NewString()
	if _, err := db.Exec(`INSERT INTO stock (variante_id, online, fisico) VALUES (?, ?, ?)`, id, online, fisico); err != nil {
		t.Fatal(err)
	}
	return id
}

func stockDe(t *testing.T, repo *Repositorio, id string) dominio.Stock {
	t.Helper()
	s, err := repo.ConsultarStock(context.Background(), []string{id})
	if err != nil || len(s) != 1 {
		t.Fatalf("consultando stock: %v", err)
	}
	return s[0]
}

func nuevaReserva(t *testing.T, items ...dominio.Item) dominio.Reserva {
	t.Helper()
	r, err := dominio.NuevaReserva(uuid.NewString(), uuid.NewString(), "test", items, 20*time.Minute, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Diez compradores quieren la última unidad a la vez: solo uno la reserva y el stock no queda negativo.
func TestReservasConcurrentesSobreLaUltimaUnidad(t *testing.T) {
	repo, db := repoDePrueba(t)
	variante := varianteNueva(t, db, 1, 0)

	var exitos, sinStock int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := repo.CrearReserva(context.Background(), nuevaReserva(t, dominio.Item{VarianteID: variante, Cantidad: 1}))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				exitos++
			case errors.Is(err, dominio.ErrStockInsuficiente):
				sinStock++
			default:
				t.Errorf("error inesperado: %v", err)
			}
		}()
	}
	wg.Wait()

	if exitos != 1 || sinStock != 9 {
		t.Errorf("esperaba 1 reserva y 9 rechazos, hubo %d y %d", exitos, sinStock)
	}
	if s := stockDe(t, repo, variante); s.Reservado != 1 || s.DisponibleOnline() != 0 {
		t.Errorf("stock final %+v", s)
	}
}

// Una reserva de dos variantes donde una no alcanza no aparta ninguna (todo o nada).
func TestReservaTodoONada(t *testing.T) {
	repo, db := repoDePrueba(t)
	alcanza := varianteNueva(t, db, 5, 0)
	noAlcanza := varianteNueva(t, db, 1, 0)

	_, _, err := repo.CrearReserva(context.Background(), nuevaReserva(t,
		dominio.Item{VarianteID: alcanza, Cantidad: 2}, dominio.Item{VarianteID: noAlcanza, Cantidad: 3}))

	var faltante *dominio.ErrorStockInsuficiente
	if !errors.As(err, &faltante) || len(faltante.Faltantes) != 1 || faltante.Faltantes[0].VarianteID != noAlcanza {
		t.Fatalf("esperaba faltante en %s, obtuve %v", noAlcanza, err)
	}
	if s := stockDe(t, repo, alcanza); s.Reservado != 0 {
		t.Errorf("no debió apartarse nada: %+v", s)
	}
}

// El mismo pedido enviado diez veces a la vez con la misma clave crea una sola reserva.
func TestReservaIdempotenteConcurrente(t *testing.T) {
	repo, db := repoDePrueba(t)
	variante := varianteNueva(t, db, 10, 0)
	base := nuevaReserva(t, dominio.Item{VarianteID: variante, Cantidad: 2})

	ids := make(chan string, 10)
	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			intento := base
			intento.ID = uuid.NewString() // cada reintento llega con otro id generado
			r, _, err := repo.CrearReserva(context.Background(), intento)
			if err != nil {
				t.Errorf("reintento: %v", err)
				return
			}
			ids <- r.ID
		}()
	}
	wg.Wait()
	close(ids)

	distintos := map[string]bool{}
	for id := range ids {
		distintos[id] = true
	}
	if len(distintos) != 1 {
		t.Errorf("esperaba una sola reserva, hubo %d", len(distintos))
	}
	if s := stockDe(t, repo, variante); s.Reservado != 2 {
		t.Errorf("se apartó más de una vez: %+v", s)
	}
}

func TestConfirmarYLiberarMuevenElStock(t *testing.T) {
	repo, db := repoDePrueba(t)
	variante := varianteNueva(t, db, 5, 0)
	ctx := context.Background()

	r1, _, _ := repo.CrearReserva(ctx, nuevaReserva(t, dominio.Item{VarianteID: variante, Cantidad: 2}))
	r2, _, _ := repo.CrearReserva(ctx, nuevaReserva(t, dominio.Item{VarianteID: variante, Cantidad: 1}))

	if _, err := repo.ModificarReserva(ctx, r1.ID, func(r *dominio.Reserva) error { return r.Confirmar(time.Now()) }); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ModificarReserva(ctx, r2.ID, func(r *dominio.Reserva) error { return r.Liberar(time.Now()) }); err != nil {
		t.Fatal(err)
	}
	// Repetir la confirmación no descuenta de nuevo.
	if _, err := repo.ModificarReserva(ctx, r1.ID, func(r *dominio.Reserva) error { return r.Confirmar(time.Now()) }); err != nil {
		t.Fatal(err)
	}
	if s := stockDe(t, repo, variante); s.Online != 3 || s.Reservado != 0 {
		t.Errorf("esperaba online 3 y reservado 0: %+v", s)
	}
}

// Una transferencia y una reserva simultáneas sobre las últimas unidades: el online nunca queda
// por debajo de lo reservado y solo una de las dos usa esas unidades (HU-07.5).
func TestTransferenciaYReservaConcurrentes(t *testing.T) {
	repo, db := repoDePrueba(t)
	ctx := context.Background()
	for i := range 20 {
		variante := varianteNueva(t, db, 2, 0)
		var wg sync.WaitGroup
		var errReserva, errTransferencia error
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _, errReserva = repo.CrearReserva(ctx, nuevaReserva(t, dominio.Item{VarianteID: variante, Cantidad: 2}))
		}()
		go func() {
			defer wg.Done()
			t2, _ := dominio.NuevaTransferencia(uuid.NewString(), variante, dominio.CanalOnline, 2, "test")
			_, errTransferencia = repo.Transferir(ctx, t2)
		}()
		wg.Wait()

		if (errReserva == nil) == (errTransferencia == nil) {
			t.Fatalf("vuelta %d: debía ganar exactamente una (reserva: %v, transferencia: %v)", i, errReserva, errTransferencia)
		}
		if s := stockDe(t, repo, variante); s.Online < s.Reservado || s.Online < 0 {
			t.Fatalf("vuelta %d: stock inválido %+v", i, s)
		}
	}
}

func ajuste(t *testing.T, variante string, canal dominio.Canal, cantidad int) dominio.Ajuste {
	t.Helper()
	a, err := dominio.NuevoAjuste(uuid.NewString(), variante, canal, cantidad, dominio.MotivoPerdida, "prueba", "test")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// HU-08: el ajuste suma o resta, queda registrado con su motivo y no se aplica dos veces.
func TestAjusteRegistraElMotivoYEsIdempotente(t *testing.T) {
	repo, db := repoDePrueba(t)
	ctx := context.Background()
	variante := varianteNueva(t, db, 5, 4)
	a := ajuste(t, variante, dominio.CanalFisico, -3)
	for range 2 {
		if s, err := repo.Ajustar(ctx, a); err != nil || s.Fisico != 1 || s.Online != 5 {
			t.Fatalf("ajuste: %+v, %v", s, err)
		}
	}
	var motivo string
	var movimientos int
	if err := db.QueryRow(`SELECT motivo FROM ajustes WHERE clave_idempotencia = ?`, a.ClaveIdempotencia).Scan(&motivo); err != nil || motivo != "PERDIDA" {
		t.Errorf("ajuste registrado: %q, %v", motivo, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM movimientos WHERE referencia = ? AND tipo = 'AJUSTE' AND cantidad = -3`, a.ClaveIdempotencia).Scan(&movimientos); err != nil || movimientos != 1 {
		t.Errorf("movimientos: %d, %v", movimientos, err)
	}
	otro := a
	otro.Motivo = dominio.MotivoFalla
	if _, err := repo.Ajustar(ctx, otro); !errors.Is(err, dominio.ErrClaveReutilizada) {
		t.Errorf("misma clave con otro motivo: %v", err)
	}
}

// HU-08.3: con 5 online y 2 reservadas, restar 4 se rechaza; restar 3 deja 2, todas reservadas.
func TestAjusteNoDejaElOnlinePorDebajoDeLoReservado(t *testing.T) {
	repo, db := repoDePrueba(t)
	ctx := context.Background()
	variante := varianteNueva(t, db, 5, 0)
	if _, _, err := repo.CrearReserva(ctx, nuevaReserva(t, dominio.Item{VarianteID: variante, Cantidad: 2})); err != nil {
		t.Fatal(err)
	}
	var stock *dominio.ErrorStockInsuficiente
	if _, err := repo.Ajustar(ctx, ajuste(t, variante, dominio.CanalOnline, -4)); !errors.As(err, &stock) || stock.Faltantes[0].Disponible != 3 {
		t.Errorf("restar 4: %v", err)
	}
	if s, err := repo.Ajustar(ctx, ajuste(t, variante, dominio.CanalOnline, -3)); err != nil || s.Online != 2 || s.Reservado != 2 {
		t.Errorf("restar 3: %+v, %v", s, err)
	}
}

// El físico no queda negativo por un ajuste [propuesta]. Sumar sí corrige uno negativo (RN-25).
func TestAjusteFisicoNoQuedaNegativoPeroCorrigeUnNegativo(t *testing.T) {
	repo, db := repoDePrueba(t)
	ctx := context.Background()
	variante := varianteNueva(t, db, 0, -2)
	if _, err := repo.Ajustar(ctx, ajuste(t, variante, dominio.CanalFisico, -1)); !errors.Is(err, dominio.ErrStockInsuficiente) {
		t.Errorf("restar con el físico en -2: %v", err)
	}
	s, err := repo.Ajustar(ctx, ajuste(t, variante, dominio.CanalFisico, 5))
	if err != nil || s.Fisico != 3 || s.AConfirmar() {
		t.Errorf("sumar 5: %+v, %v", s, err)
	}
}

func TestAjusteSobreUnaVarianteSinStock(t *testing.T) {
	repo, _ := repoDePrueba(t)
	ctx := context.Background()
	variante := "test-" + uuid.NewString()
	if _, err := repo.Ajustar(ctx, ajuste(t, variante, dominio.CanalOnline, -1)); !errors.Is(err, dominio.ErrVarianteInexistente) {
		t.Errorf("restar a una variante sin stock: %v", err)
	}
	if s, err := repo.Ajustar(ctx, ajuste(t, variante, dominio.CanalOnline, 2)); err != nil || s.Online != 2 {
		t.Errorf("sumar crea la fila: %+v, %v", s, err)
	}
}

// Un ajuste y una reserva sobre las últimas unidades a la vez: el online nunca queda por debajo de lo reservado.
func TestAjusteYReservaConcurrentes(t *testing.T) {
	repo, db := repoDePrueba(t)
	ctx := context.Background()
	for range 20 {
		variante := varianteNueva(t, db, 1, 0)
		var wg sync.WaitGroup
		var errAjuste, errReserva error
		wg.Add(2)
		go func() { defer wg.Done(); _, errAjuste = repo.Ajustar(ctx, ajuste(t, variante, dominio.CanalOnline, -1)) }()
		go func() {
			defer wg.Done()
			_, _, errReserva = repo.CrearReserva(ctx, nuevaReserva(t, dominio.Item{VarianteID: variante, Cantidad: 1}))
		}()
		wg.Wait()
		s := stockDe(t, repo, variante)
		if (errAjuste == nil) == (errReserva == nil) || s.Online < s.Reservado {
			t.Fatalf("ajuste: %v, reserva: %v, stock: %+v", errAjuste, errReserva, s)
		}
	}
}

// HU-09.2: solo las variantes con físico negativo, primero las más negativas.
func TestStockACorregir(t *testing.T) {
	repo, db := repoDePrueba(t)
	ctx := context.Background()
	muyNegativa, negativa := varianteNueva(t, db, 0, -1000), varianteNueva(t, db, 3, -999)
	enCero, positiva := varianteNueva(t, db, 0, 0), varianteNueva(t, db, 0, 5)
	stocks, err := repo.StockACorregir(ctx, 1000)
	if err != nil {
		t.Fatal(err)
	}
	posicion := map[string]int{}
	for i, s := range stocks {
		if s.Fisico >= 0 {
			t.Errorf("incluye una variante sin físico negativo: %+v", s)
		}
		posicion[s.VarianteID] = i
	}
	if _, ok := posicion[enCero]; ok {
		t.Error("incluye una variante en 0")
	}
	if _, ok := posicion[positiva]; ok {
		t.Error("incluye una variante positiva")
	}
	// Orden relativo: la base de los tests conserva las filas de corridas anteriores.
	primera, hayPrimera := posicion[muyNegativa]
	segunda, haySegunda := posicion[negativa]
	if !hayPrimera || !haySegunda || primera >= segunda {
		t.Errorf("orden: %d y %d", primera, segunda)
	}
	if s, _ := repo.StockACorregir(ctx, 1); len(s) != 1 {
		t.Errorf("límite: %d", len(s))
	}
}

func TestTransferenciaNoMueveLoReservado(t *testing.T) {
	repo, db := repoDePrueba(t)
	ctx := context.Background()
	variante := varianteNueva(t, db, 5, 0)
	if _, _, err := repo.CrearReserva(ctx, nuevaReserva(t, dominio.Item{VarianteID: variante, Cantidad: 2})); err != nil {
		t.Fatal(err)
	}
	// HU-07.2: 5 online con 2 reservadas; transferir 4 se rechaza porque solo hay 3 disponibles.
	t1, _ := dominio.NuevaTransferencia(uuid.NewString(), variante, dominio.CanalOnline, 4, "test")
	if _, err := repo.Transferir(ctx, t1); !errors.Is(err, dominio.ErrStockInsuficiente) {
		t.Errorf("esperaba stock insuficiente, obtuve %v", err)
	}
	t2, _ := dominio.NuevaTransferencia(uuid.NewString(), variante, dominio.CanalOnline, 3, "test")
	s, err := repo.Transferir(ctx, t2)
	if err != nil || s.Online != 2 || s.Fisico != 3 {
		t.Errorf("transferencia de 3: %+v, %v", s, err)
	}
	// Repetida con la misma clave, no se aplica dos veces (HU-07.4).
	s, err = repo.Transferir(ctx, t2)
	if err != nil || s.Online != 2 || s.Fisico != 3 {
		t.Errorf("transferencia repetida: %+v, %v", s, err)
	}
}

func TestVencimientoGuardaEventoEnOutbox(t *testing.T) {
	repo, db := repoDePrueba(t)
	ctx := context.Background()
	variante := varianteNueva(t, db, 3, 0)
	r, _, _ := repo.CrearReserva(ctx, nuevaReserva(t, dominio.Item{VarianteID: variante, Cantidad: 3}))

	futuro := time.Now().Add(time.Hour)
	if _, err := repo.ModificarReserva(ctx, r.ID, func(res *dominio.Reserva) error { res.Vencer(futuro); return nil }); err != nil {
		t.Fatal(err)
	}
	if s := stockDe(t, repo, variante); s.Reservado != 0 {
		t.Errorf("el vencimiento debía liberar el stock: %+v", s)
	}
	var eventos int
	if err := db.QueryRow(`SELECT COUNT(*) FROM outbox WHERE tipo = 'ReservaVencida' AND JSON_EXTRACT(datos, '$.reservaId') = ?`, r.ID).Scan(&eventos); err != nil {
		t.Fatal(err)
	}
	if eventos != 1 {
		t.Errorf("esperaba un evento ReservaVencida, hubo %d", eventos)
	}
}

// Una venta del local con la misma clave descuenta una sola vez, y el físico puede quedar negativo (RN-25).
func TestDescontarFisicoEsIdempotenteYPuedeQuedarNegativo(t *testing.T) {
	repo, db := repoDePrueba(t)
	id := varianteNueva(t, db, 0, 1)
	d, err := dominio.NuevoDescuentoFisico(uuid.NewString(), "venta-local", []dominio.Item{{VarianteID: id, Cantidad: 3}}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := repo.DescontarFisico(context.Background(), d); err != nil {
			t.Fatal(err)
		}
	}
	if s := stockDe(t, repo, id); s.Fisico != -2 || !s.AConfirmar() {
		t.Errorf("físico %d, a corregir %v", s.Fisico, s.AConfirmar())
	}

	otro, _ := dominio.NuevoDescuentoFisico(d.ClaveIdempotencia, "venta-local", []dominio.Item{{VarianteID: id, Cantidad: 1}}, "admin")
	if _, err := repo.DescontarFisico(context.Background(), otro); !errors.Is(err, dominio.ErrClaveReutilizada) {
		t.Errorf("misma clave con otro pedido: %v", err)
	}
}

// La venta del local no se bloquea aunque la variante todavía no tenga fila de stock (RN-06).
func TestDescontarFisicoCreaLaVarianteSiNoExiste(t *testing.T) {
	repo, _ := repoDePrueba(t)
	id := "test-" + uuid.NewString()
	d, _ := dominio.NuevoDescuentoFisico(uuid.NewString(), "venta-local", []dominio.Item{{VarianteID: id, Cantidad: 2}}, "")
	if _, err := repo.DescontarFisico(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if s := stockDe(t, repo, id); s.Fisico != -2 {
		t.Errorf("físico %d", s.Fisico)
	}
}

// Si el broker falla, el evento sigue en el outbox; cuando vuelve, se publica una sola vez.
func TestPublicarEventosConservaLoQueNoSePudoPublicar(t *testing.T) {
	repo, db := repoDePrueba(t)
	id := uuid.NewString()
	if _, err := db.Exec(`INSERT INTO outbox (id, tipo, datos, ocurrido_en) VALUES (?, 'ReservaVencida', '{}', '2000-01-01')`, id); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	falla := func(_ context.Context, e dominio.Evento) error {
		if e.ID == id {
			return errors.New("broker caído")
		}
		return nil
	}
	if _, err := repo.PublicarEventos(ctx, 1, falla); err != nil {
		t.Fatal(err)
	}
	var publicado sql.NullTime
	if err := db.QueryRow(`SELECT publicado_en FROM outbox WHERE id = ?`, id).Scan(&publicado); err != nil || publicado.Valid {
		t.Fatalf("no debía marcarse publicado: %v %v", publicado, err)
	}

	veces := 0
	publicar := func(_ context.Context, e dominio.Evento) error {
		if e.ID == id {
			veces++
		}
		return nil
	}
	for range 2 {
		if _, err := repo.PublicarEventos(ctx, 50, publicar); err != nil {
			t.Fatal(err)
		}
	}
	if veces != 1 {
		t.Errorf("publicado %d veces", veces)
	}
}
