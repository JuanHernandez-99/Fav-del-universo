package mysql

// Tests de integración contra un MySQL real (ADR-003, validación). Necesitan la variable
// INVENTARIO_TEST_DSN; sin ella se saltean. Con Docker Compose:
//
//	docker compose up -d mysql-inventario
//	INVENTARIO_TEST_DSN="inventario:cambiar@tcp(localhost:3307)/inventario?parseTime=true&loc=UTC&multiStatements=true" go test ./...

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/dominio"
)

func repoDePrueba(t *testing.T) (*Repositorio, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("INVENTARIO_TEST_DSN")
	if dsn == "" {
		t.Skip("INVENTARIO_TEST_DSN no definida: se saltea el test de integración")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	repo := NuevoRepositorio(db)
	if err := repo.Migrar(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	return repo, db
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
