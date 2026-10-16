package mysql_test

// Tests contra una base MySQL real. Se ejecutan solo si está definida
// INVENTARIO_TEST_DSN, por ejemplo:
//
//	INVENTARIO_TEST_DSN='inventario:clave@tcp(127.0.0.1:3306)/inventario_test?parseTime=true&loc=UTC&multiStatements=true' go test ./...
//
// La base indicada se borra y se recrea en cada test.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/adaptadores"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/adaptadores/mysql"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/aplicacion"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/dominio"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/puertos"
)

type relojFijo struct {
	mu sync.Mutex
	t  time.Time
}

func (r *relojFijo) Ahora() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.t }
func (r *relojFijo) Avanzar(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.t = r.t.Add(d)
}

type publicadorFalso struct{ recibidos []puertos.MensajeOutbox }

func (p *publicadorFalso) Publicar(_ context.Context, m puertos.MensajeOutbox) error {
	p.recibidos = append(p.recibidos, m)
	return nil
}

func preparar(t *testing.T, stock map[string]int) (*aplicacion.Servicio, *sql.DB, *relojFijo) {
	t.Helper()
	dsn := os.Getenv("INVENTARIO_TEST_DSN")
	if dsn == "" {
		t.Skip("INVENTARIO_TEST_DSN no definida: se omiten los tests contra MySQL")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(40)
	t.Cleanup(func() { db.Close() })

	esquema, err := os.ReadFile("../../../migraciones/001_esquema.sql")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, tabla := range []string{"outbox", "claves_idempotencia", "reserva_items", "reservas", "stock"} {
		if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS "+tabla); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, string(esquema)); err != nil {
		t.Fatal(err)
	}
	for variante, cantidad := range stock {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO stock (variante_id, canal, cantidad) VALUES (?, 'ONLINE', ?), (?, 'FISICO', 0)`,
			variante, cantidad, variante); err != nil {
			t.Fatal(err)
		}
	}
	reloj := &relojFijo{t: time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)}
	return aplicacion.NuevoServicio(mysql.NuevoRepositorio(db), reloj, adaptadores.GeneradorUUID{}), db, reloj
}

func stockOnline(t *testing.T, db *sql.DB, variante string) (cantidad, reservado int) {
	t.Helper()
	err := db.QueryRow(`SELECT cantidad, reservado FROM stock WHERE variante_id = ? AND canal = 'ONLINE'`, variante).
		Scan(&cantidad, &reservado)
	if err != nil {
		t.Fatal(err)
	}
	return
}

func pedido(clave string, items ...dominio.Item) aplicacion.PedidoReserva {
	return aplicacion.PedidoReserva{ClaveIdempotencia: clave, Referencia: "venta-" + clave, VencimientoMinutos: 20,
		CorrelacionID: "corr-" + clave, Items: items}
}

// Muchas compras simultáneas sobre pocas unidades: nunca se reserva de más.
func TestReservasConcurrentesNoVendenDeMas(t *testing.T) {
	svc, db, _ := preparar(t, map[string]int{"var-a": 5})
	const compradores = 40

	var wg sync.WaitGroup
	var mu sync.Mutex
	exitos, sinStock := 0, 0
	for i := 0; i < compradores; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := svc.Reservar(context.Background(), pedido(fmt.Sprintf("k%d", i), dominio.Item{VarianteID: "var-a", Cantidad: 1}))
			var stock *dominio.ErrStockInsuficiente
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				exitos++
			case errors.As(err, &stock):
				sinStock++
			default:
				t.Errorf("error inesperado: %v", err)
			}
		}(i)
	}
	wg.Wait()

	if exitos != 5 || sinStock != compradores-5 {
		t.Errorf("éxitos=%d sin stock=%d; se esperaban 5 y %d", exitos, sinStock, compradores-5)
	}
	if cantidad, reservado := stockOnline(t, db, "var-a"); cantidad != 5 || reservado != 5 {
		t.Errorf("stock cantidad=%d reservado=%d; se esperaba 5 y 5", cantidad, reservado)
	}
}

// Si una variante no alcanza, no se aparta ninguna (todo o nada).
func TestReservaTodoONada(t *testing.T) {
	svc, db, _ := preparar(t, map[string]int{"var-a": 3, "var-b": 1})

	_, err := svc.Reservar(context.Background(), pedido("k1",
		dominio.Item{VarianteID: "var-a", Cantidad: 2}, dominio.Item{VarianteID: "var-b", Cantidad: 2}))
	var stock *dominio.ErrStockInsuficiente
	if !errors.As(err, &stock) || len(stock.Faltantes) != 1 || stock.Faltantes[0].VarianteID != "var-b" {
		t.Fatalf("se esperaba stock insuficiente en var-b, vino %v", err)
	}
	if _, reservado := stockOnline(t, db, "var-a"); reservado != 0 {
		t.Errorf("var-a quedó con %d reservadas; no debía apartarse nada", reservado)
	}

	_, err = svc.Reservar(context.Background(), pedido("k2", dominio.Item{VarianteID: "var-x", Cantidad: 1}))
	var inex *dominio.ErrVarianteInexistente
	if !errors.As(err, &inex) {
		t.Errorf("se esperaba variante inexistente, vino %v", err)
	}
}

// Reintentos simultáneos con la misma clave crean una sola reserva.
func TestReservaIdempotente(t *testing.T) {
	svc, db, _ := preparar(t, map[string]int{"var-a": 10})

	var wg sync.WaitGroup
	ids := make([]string, 10)
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, err := svc.Reservar(context.Background(), pedido("misma-clave", dominio.Item{VarianteID: "var-a", Cantidad: 2}))
			if err != nil {
				t.Errorf("reintento %d: %v", i, err)
				return
			}
			ids[i] = r.ID
		}(i)
	}
	wg.Wait()
	for _, id := range ids {
		if id != ids[0] {
			t.Fatalf("los reintentos devolvieron reservas distintas: %v", ids)
		}
	}
	if _, reservado := stockOnline(t, db, "var-a"); reservado != 2 {
		t.Errorf("reservado=%d; se esperaba 2 (una sola reserva)", reservado)
	}

	_, err := svc.Reservar(context.Background(), pedido("misma-clave", dominio.Item{VarianteID: "var-a", Cantidad: 3}))
	if !errors.Is(err, dominio.ErrClaveReutilizada) {
		t.Errorf("misma clave con otro pedido: se esperaba ErrClaveReutilizada, vino %v", err)
	}
}

// Confirmar descuenta en forma definitiva; confirmar de nuevo no descuenta otra vez.
func TestConfirmarDescuentaUnaVez(t *testing.T) {
	svc, db, _ := preparar(t, map[string]int{"var-a": 5})
	r, err := svc.Reservar(context.Background(), pedido("k1", dominio.Item{VarianteID: "var-a", Cantidad: 2}))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		c, err := svc.Confirmar(context.Background(), r.ID)
		if err != nil || c.Estado != dominio.EstadoConfirmada {
			t.Fatalf("confirmación %d: estado=%s err=%v", i+1, c.Estado, err)
		}
	}
	if cantidad, reservado := stockOnline(t, db, "var-a"); cantidad != 3 || reservado != 0 {
		t.Errorf("cantidad=%d reservado=%d; se esperaba 3 y 0", cantidad, reservado)
	}
	if _, err := svc.Liberar(context.Background(), r.ID); !errors.Is(err, dominio.ErrReservaConfirmada) {
		t.Errorf("liberar una confirmada: se esperaba ErrReservaConfirmada, vino %v", err)
	}
}

// Una reserva vencida no se puede confirmar; el vencimiento devuelve las unidades
// y deja el evento ReservaVencida en el outbox.
func TestVencimiento(t *testing.T) {
	svc, db, reloj := preparar(t, map[string]int{"var-a": 1})
	r, err := svc.Reservar(context.Background(), pedido("k1", dominio.Item{VarianteID: "var-a", Cantidad: 1}))
	if err != nil {
		t.Fatal(err)
	}
	reloj.Avanzar(21 * time.Minute)

	if _, err := svc.Confirmar(context.Background(), r.ID); !errors.Is(err, dominio.ErrReservaVencida) {
		t.Errorf("confirmar vencida: se esperaba ErrReservaVencida, vino %v", err)
	}
	n, err := svc.VencerPendientes(context.Background(), 100)
	if err != nil || n != 1 {
		t.Fatalf("vencidas=%d err=%v; se esperaba 1", n, err)
	}
	if _, reservado := stockOnline(t, db, "var-a"); reservado != 0 {
		t.Errorf("reservado=%d; el vencimiento debía devolver la unidad", reservado)
	}
	// El outbox tiene el sobre completo del evento (ADR-005 §5).
	repo := mysql.NuevoRepositorio(db)
	pendientes, err := repo.Pendientes(context.Background(), 10)
	if err != nil || len(pendientes) != 1 {
		t.Fatalf("pendientes=%d err=%v; se esperaba 1", len(pendientes), err)
	}
	m := pendientes[0]
	var sobre struct {
		ID, Tipo, CorrelationID string
		Version                 int
		OcurridoEn              time.Time
		Datos                   struct {
			ReservaID, Referencia string
			VencidaEn             time.Time
		}
	}
	if err := json.Unmarshal(m.Cuerpo, &sobre); err != nil {
		t.Fatal(err)
	}
	if sobre.ID != m.ID || sobre.Tipo != "ReservaVencida" || m.Tipo != "ReservaVencida" || sobre.Version != 1 ||
		sobre.CorrelationID != "corr-k1" || m.CorrelacionID != "corr-k1" || !sobre.OcurridoEn.Equal(reloj.Ahora()) ||
		sobre.Datos.ReservaID != r.ID || sobre.Datos.Referencia != "venta-k1" || sobre.Datos.VencidaEn.IsZero() {
		t.Errorf("evento incorrecto: %s", m.Cuerpo)
	}
	if n, _ := svc.VencerPendientes(context.Background(), 100); n != 0 {
		t.Errorf("vencer de nuevo no debe tener efecto; vencidas=%d", n)
	}

	// El relevo lo publica una vez y lo marca.
	pub := &publicadorFalso{}
	relevo := aplicacion.NuevoRelevo(repo, pub, reloj)
	if n, err := relevo.PublicarPendientes(context.Background(), 10); n != 1 || err != nil {
		t.Fatalf("publicados=%d err=%v; se esperaba 1", n, err)
	}
	if n, _ := relevo.PublicarPendientes(context.Background(), 10); n != 0 || len(pub.recibidos) != 1 {
		t.Errorf("un evento ya publicado no se vuelve a publicar; recibidos=%d", len(pub.recibidos))
	}
	// La unidad liberada se puede volver a reservar.
	if _, err := svc.Reservar(context.Background(), pedido("k2", dominio.Item{VarianteID: "var-a", Cantidad: 1})); err != nil {
		t.Errorf("la unidad liberada debía poder reservarse: %v", err)
	}
}

// Confirmación y vencimiento simultáneos: gana una sola, y el stock queda coherente.
func TestCarreraConfirmarVencer(t *testing.T) {
	for i := 0; i < 20; i++ {
		svc, db, reloj := preparar(t, map[string]int{"var-a": 1})
		r, err := svc.Reservar(context.Background(), pedido("k", dominio.Item{VarianteID: "var-a", Cantidad: 1}))
		if err != nil {
			t.Fatal(err)
		}
		reloj.Avanzar(20*time.Minute - time.Millisecond)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); svc.Confirmar(context.Background(), r.ID) }() //nolint:errcheck
		go func() {
			defer wg.Done()
			reloj.Avanzar(time.Millisecond)
			svc.VencerPendientes(context.Background(), 10)
		}() //nolint:errcheck
		wg.Wait()

		final, _ := svc.ObtenerReserva(context.Background(), r.ID)
		cantidad, reservado := stockOnline(t, db, "var-a")
		switch final.Estado {
		case dominio.EstadoConfirmada:
			if cantidad != 0 || reservado != 0 {
				t.Fatalf("confirmada pero stock cantidad=%d reservado=%d", cantidad, reservado)
			}
		case dominio.EstadoVencida, dominio.EstadoActiva:
			if cantidad != 1 {
				t.Fatalf("%s pero cantidad=%d", final.Estado, cantidad)
			}
		default:
			t.Fatalf("estado inesperado %s", final.Estado)
		}
		if reservado < 0 {
			t.Fatalf("reservado negativo: %d", reservado)
		}
	}
}
