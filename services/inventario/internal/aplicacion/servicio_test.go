package aplicacion

// Tests de los casos de uso con dobles de los puertos: prueban las reglas que decide la aplicación (idempotencia,
// carrera entre pago y vencimiento, qué hacer con cada respuesta del gimnasio). Que "reservar si alcanza" sea atómico
// lo prueba el adaptador contra MySQL real (adaptadores/mysql), porque esa garantía la da la base.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/dominio"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/puertos"
)

var inicio = time.Date(2026, 12, 15, 10, 0, 0, 0, time.UTC) // diciembre: pico de ventas (SPEC §3)

// ---------- Dobles de los puertos ----------

// repoFalso guarda en memoria lo que necesitan estos tests. Los métodos que no implementa vienen del Repositorio
// embebido (nil): si un caso de uso los llamara, el test falla con un panic, que es lo que se quiere.
type repoFalso struct {
	puertos.Repositorio

	reservas        map[string]dominio.Reserva // por id
	reservaPorClave map[string]string
	creaciones      int

	stocks []dominio.Stock

	reposiciones     map[string]dominio.Reposicion // por clave
	llamadasGimnasio []string                      // qué resolvió EnviarReposicion: "confirmar:<id>", "rechazar:<id>"…
	proximoIntento   time.Time

	cantidadesPorEstado map[dominio.EstadoReposicion]int
	pendienteMasAntigua time.Time
	escrituras          int
}

func nuevoRepo() *repoFalso {
	return &repoFalso{
		reservas:        map[string]dominio.Reserva{},
		reservaPorClave: map[string]string{},
		reposiciones:    map[string]dominio.Reposicion{},
	}
}

func (f *repoFalso) CrearReserva(_ context.Context, r dominio.Reserva) (dominio.Reserva, bool, error) {
	f.creaciones++
	if id, ok := f.reservaPorClave[r.ClaveIdempotencia]; ok {
		return f.reservas[id], true, nil
	}
	f.reservas[r.ID] = r
	f.reservaPorClave[r.ClaveIdempotencia] = r.ID
	return r, false, nil
}

// ModificarReserva guarda el cambio aunque la transición devuelva error, como el adaptador real: así una reserva
// que vence al intentar confirmarla queda vencida.
func (f *repoFalso) ModificarReserva(_ context.Context, id string, transicion func(*dominio.Reserva) error) (dominio.Reserva, error) {
	r, ok := f.reservas[id]
	if !ok {
		return dominio.Reserva{}, dominio.ErrReservaNoEncontrada
	}
	err := transicion(&r)
	f.reservas[id] = r
	return r, err
}

func (f *repoFalso) ConsultarStock(context.Context, []string) ([]dominio.Stock, error) {
	return f.stocks, nil
}

func (f *repoFalso) StockACorregir(_ context.Context, limite int) ([]dominio.Stock, error) {
	return f.stocks[:min(limite, len(f.stocks))], nil
}

func (f *repoFalso) Ingresar(context.Context, dominio.Ingreso) (dominio.Stock, error) {
	f.escrituras++
	return dominio.Stock{}, nil
}

func (f *repoFalso) Ajustar(context.Context, dominio.Ajuste) (dominio.Stock, error) {
	f.escrituras++
	return dominio.Stock{}, nil
}

func (f *repoFalso) CrearReposicion(_ context.Context, r dominio.Reposicion) (dominio.Reposicion, bool, error) {
	if guardada, ok := f.reposiciones[r.ClaveIdempotencia]; ok {
		return guardada, true, nil
	}
	f.reposiciones[r.ClaveIdempotencia] = r
	return r, false, nil
}

func (f *repoFalso) ConfirmarReposicion(_ context.Context, id, idGimnasio string, _ time.Time) error {
	f.llamadasGimnasio = append(f.llamadasGimnasio, "confirmar:"+id+":"+idGimnasio)
	return nil
}

func (f *repoFalso) RechazarReposicion(_ context.Context, id, _ string, _ time.Time) error {
	f.llamadasGimnasio = append(f.llamadasGimnasio, "rechazar:"+id)
	return nil
}

func (f *repoFalso) MarcarReposicionARevisar(_ context.Context, id, _ string, _ time.Time) error {
	f.llamadasGimnasio = append(f.llamadasGimnasio, "revisar:"+id)
	return nil
}

func (f *repoFalso) BloquearReposicion(_ context.Context, id, _ string, proximo time.Time) error {
	f.llamadasGimnasio = append(f.llamadasGimnasio, "bloquear:"+id)
	f.proximoIntento = proximo
	return nil
}

func (f *repoFalso) ReprogramarReposicion(_ context.Context, id, _ string, proximo time.Time) error {
	f.llamadasGimnasio = append(f.llamadasGimnasio, "reprogramar:"+id)
	f.proximoIntento = proximo
	return nil
}

func (f *repoFalso) ResumirReposiciones(context.Context) (map[dominio.EstadoReposicion]int, time.Time, error) {
	return f.cantidadesPorEstado, f.pendienteMasAntigua, nil
}

type gimnasioFalso struct {
	idGimnasio string
	err        error
}

func (g gimnasioFalso) EnviarReposicion(context.Context, dominio.Reposicion) (string, error) {
	return g.idGimnasio, g.err
}

func (g gimnasioFalso) ConsultarStock(context.Context) ([]dominio.StockConsignacion, error) {
	return nil, g.err
}

// servicioDePrueba arma el servicio con un reloj que el test puede adelantar e ids predecibles.
func servicioDePrueba(repo *repoFalso, gim gimnasioFalso) (*Servicio, *time.Time) {
	reloj := inicio
	s := NuevoServicio(repo, gim)
	s.ahora = func() time.Time { return reloj }
	n := 0
	s.nuevoID = func() string { n++; return "id-" + strconv.Itoa(n) }
	return s, &reloj
}

func pedidoRemera(clave string, cantidad int) PedidoReserva {
	return PedidoReserva{
		ClaveIdempotencia: clave,
		Referencia:        "venta-diciembre-1",
		Items:             []dominio.Item{{VarianteID: "var-remera-lila-m", Cantidad: cantidad}},
		Vigencia:          15 * time.Minute,
	}
}

// ---------- Reservas ----------

func TestReservarDosVecesConLaMismaClaveDevuelveLaMismaReserva(t *testing.T) {
	repo := nuevoRepo()
	s, _ := servicioDePrueba(repo, gimnasioFalso{})
	ctx := context.Background()

	primera, repetida, err := s.Reservar(ctx, pedidoRemera("compra-remera-001", 2))
	if err != nil || repetida {
		t.Fatalf("primera reserva: repetida=%v, err=%v", repetida, err)
	}
	segunda, repetida, err := s.Reservar(ctx, pedidoRemera("compra-remera-001", 2))
	if err != nil || !repetida {
		t.Fatalf("reintento: repetida=%v, err=%v", repetida, err)
	}
	if segunda.ID != primera.ID {
		t.Errorf("el reintento devolvió otra reserva: %s y %s", primera.ID, segunda.ID)
	}
	if len(repo.reservas) != 1 {
		t.Errorf("quedaron %d reservas guardadas, esperaba 1", len(repo.reservas))
	}
}

func TestReservarConUnaClaveYaUsadaPorOtroPedidoSeRechaza(t *testing.T) {
	repo := nuevoRepo()
	s, _ := servicioDePrueba(repo, gimnasioFalso{})
	ctx := context.Background()

	if _, _, err := s.Reservar(ctx, pedidoRemera("compra-remera-001", 2)); err != nil {
		t.Fatal(err)
	}
	// Misma clave, otra cantidad de remeras: no puede devolver la reserva de 2 como si fuera la de 3.
	_, _, err := s.Reservar(ctx, pedidoRemera("compra-remera-001", 3))
	if !errors.Is(err, dominio.ErrClaveReutilizada) {
		t.Fatalf("esperaba ErrClaveReutilizada, fue %v", err)
	}
}

func TestReservarUnPedidoInvalidoNoLlegaALaBase(t *testing.T) {
	repo := nuevoRepo()
	s, _ := servicioDePrueba(repo, gimnasioFalso{})

	_, _, err := s.Reservar(context.Background(), pedidoRemera("compra-remera-002", 0))
	if !errors.Is(err, dominio.ErrPedidoInvalido) {
		t.Fatalf("esperaba ErrPedidoInvalido, fue %v", err)
	}
	if repo.creaciones != 0 {
		t.Errorf("un pedido inválido llegó al repositorio %d veces", repo.creaciones)
	}
}

func TestPagoQueLlegaDespuesDelVencimientoDejaLaReservaVencida(t *testing.T) {
	repo := nuevoRepo()
	s, reloj := servicioDePrueba(repo, gimnasioFalso{})
	ctx := context.Background()
	r, _, err := s.Reservar(ctx, pedidoRemera("compra-remera-003", 1))
	if err != nil {
		t.Fatal(err)
	}

	*reloj = inicio.Add(16 * time.Minute) // el pago se aprobó, pero la reserva vencía a los 15
	_, err = s.Confirmar(ctx, r.ID)
	if !errors.Is(err, dominio.ErrReservaVencida) {
		t.Fatalf("esperaba ErrReservaVencida, fue %v", err)
	}
	if estado := repo.reservas[r.ID].Estado; estado != dominio.ReservaVencida {
		t.Errorf("la reserva quedó %s: Inventario decide la carrera y tiene que quedar vencida", estado)
	}
}

func TestConfirmarATiempoYReintentarDaElMismoResultado(t *testing.T) {
	repo := nuevoRepo()
	s, reloj := servicioDePrueba(repo, gimnasioFalso{})
	ctx := context.Background()
	r, _, _ := s.Reservar(ctx, pedidoRemera("compra-remera-004", 1))

	*reloj = inicio.Add(10 * time.Minute)
	for intento := 1; intento <= 2; intento++ {
		confirmada, err := s.Confirmar(ctx, r.ID)
		if err != nil || confirmada.Estado != dominio.ReservaConfirmada {
			t.Fatalf("intento %d: estado %s, err %v", intento, confirmada.Estado, err)
		}
	}
}

func TestVencerReservaSoloCuandoPasoElPlazoYUnaSolaVez(t *testing.T) {
	repo := nuevoRepo()
	s, reloj := servicioDePrueba(repo, gimnasioFalso{})
	ctx := context.Background()
	r, _, _ := s.Reservar(ctx, pedidoRemera("compra-remera-005", 1))

	*reloj = inicio.Add(14 * time.Minute)
	if cambio, err := s.VencerReserva(ctx, r.ID); err != nil || cambio {
		t.Fatalf("antes del plazo: cambio=%v, err=%v", cambio, err)
	}

	*reloj = inicio.Add(15 * time.Minute)
	if cambio, err := s.VencerReserva(ctx, r.ID); err != nil || !cambio {
		t.Fatalf("al cumplirse el plazo: cambio=%v, err=%v", cambio, err)
	}
	// Otra réplica la procesa después: no hay un segundo cambio, así no se libera el stock dos veces.
	if cambio, err := s.VencerReserva(ctx, r.ID); err != nil || cambio {
		t.Fatalf("segunda vez: cambio=%v, err=%v", cambio, err)
	}
}

func TestVencerNoTocaUnaReservaConfirmada(t *testing.T) {
	repo := nuevoRepo()
	s, reloj := servicioDePrueba(repo, gimnasioFalso{})
	ctx := context.Background()
	r, _, _ := s.Reservar(ctx, pedidoRemera("compra-remera-006", 1))
	if _, err := s.Confirmar(ctx, r.ID); err != nil {
		t.Fatal(err)
	}

	*reloj = inicio.Add(time.Hour)
	if cambio, _ := s.VencerReserva(ctx, r.ID); cambio {
		t.Error("una reserva pagada no puede vencer")
	}
	if estado := repo.reservas[r.ID].Estado; estado != dominio.ReservaConfirmada {
		t.Errorf("estado %s", estado)
	}
}

func TestLiberarUnaReservaConfirmadaSeRechaza(t *testing.T) {
	repo := nuevoRepo()
	s, _ := servicioDePrueba(repo, gimnasioFalso{})
	ctx := context.Background()
	r, _, _ := s.Reservar(ctx, pedidoRemera("compra-remera-007", 1))
	_, _ = s.Confirmar(ctx, r.ID)

	if _, err := s.Liberar(ctx, r.ID); !errors.Is(err, dominio.ErrReservaYaConfirmada) {
		t.Fatalf("esperaba ErrReservaYaConfirmada, fue %v", err)
	}
}

func TestOperarSobreUnaReservaQueNoExiste(t *testing.T) {
	s, _ := servicioDePrueba(nuevoRepo(), gimnasioFalso{})
	if _, err := s.Confirmar(context.Background(), "no-existe"); !errors.Is(err, dominio.ErrReservaNoEncontrada) {
		t.Fatalf("esperaba ErrReservaNoEncontrada, fue %v", err)
	}
}

// ---------- Stock ----------

func TestDisponibilidadEnElOrdenPedidoYNuncaNegativa(t *testing.T) {
	repo := nuevoRepo()
	repo.stocks = []dominio.Stock{
		{VarianteID: "var-remera-lila-m", Online: 5, Reservado: 2},
		{VarianteID: "var-buzo-gris-l", Online: 1, Reservado: 3}, // un ajuste dejó menos online que lo reservado
	}
	s, _ := servicioDePrueba(repo, gimnasioFalso{})

	pedidas := []string{"var-top-negro-s", "var-remera-lila-m", "var-buzo-gris-l"}
	got, err := s.ConsultarDisponibilidad(context.Background(), pedidas)
	if err != nil {
		t.Fatal(err)
	}
	want := []dominio.Disponibilidad{
		{VarianteID: "var-top-negro-s", Disponible: 0}, // sin fila de stock: 0, no un error
		{VarianteID: "var-remera-lila-m", Disponible: 3},
		{VarianteID: "var-buzo-gris-l", Disponible: 0}, // el comprador nunca ve unidades negativas
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("disponibilidad\n got  %v\n want %v", got, want)
	}
}

func TestStockACorregirAvisaCuandoHayMasDeLasQueDevuelve(t *testing.T) {
	repo := nuevoRepo()
	for i := range MaximoACorregir + 1 {
		repo.stocks = append(repo.stocks, dominio.Stock{VarianteID: fmt.Sprintf("var-%d", i), Fisico: -1})
	}
	s, _ := servicioDePrueba(repo, gimnasioFalso{})

	lista, hayMas, err := s.StockACorregir(context.Background())
	if err != nil || len(lista) != MaximoACorregir || !hayMas {
		t.Fatalf("con %d a corregir: devolvió %d, hayMas=%v, err=%v", MaximoACorregir+1, len(lista), hayMas, err)
	}

	repo.stocks = repo.stocks[:3]
	lista, hayMas, _ = s.StockACorregir(context.Background())
	if len(lista) != 3 || hayMas {
		t.Fatalf("con 3 a corregir: devolvió %d, hayMas=%v", len(lista), hayMas)
	}
}

func TestMovimientosDeStockInvalidosNoLleganALaBase(t *testing.T) {
	repo := nuevoRepo()
	s, _ := servicioDePrueba(repo, gimnasioFalso{})
	ctx := context.Background()

	if _, err := s.Ingresar(ctx, "ingreso-0001", "var-remera-lila-m", dominio.CanalOnline, 0, "admin"); !errors.Is(err, dominio.ErrPedidoInvalido) {
		t.Errorf("ingreso de 0 unidades: %v", err)
	}
	if _, err := s.Ingresar(ctx, "ingreso-0002", "var-remera-lila-m", "DEPOSITO", 5, "admin"); !errors.Is(err, dominio.ErrPedidoInvalido) {
		t.Errorf("ingreso a un canal que no existe: %v", err)
	}
	if _, err := s.Ajustar(ctx, "ajuste-00001", "var-remera-lila-m", dominio.CanalFisico, -1, "INVENTADO", "", "admin"); !errors.Is(err, dominio.ErrPedidoInvalido) {
		t.Errorf("ajuste con un motivo que no existe: %v", err)
	}
	if repo.escrituras != 0 {
		t.Errorf("%d movimientos inválidos llegaron al repositorio", repo.escrituras)
	}
}

// ---------- Reposiciones al gimnasio (ADR-018) ----------

func TestReponerEsIdempotentePorClave(t *testing.T) {
	repo := nuevoRepo()
	s, _ := servicioDePrueba(repo, gimnasioFalso{})
	ctx := context.Background()

	primera, repetida, err := s.Reponer(ctx, "reposicion-gim-01", "var-calza-negra-m", 4, "admin")
	if err != nil || repetida {
		t.Fatalf("primera: repetida=%v, err=%v", repetida, err)
	}
	segunda, repetida, err := s.Reponer(ctx, "reposicion-gim-01", "var-calza-negra-m", 4, "admin")
	if err != nil || !repetida || segunda.ID != primera.ID {
		t.Fatalf("reintento: id %s (esperaba %s), repetida=%v, err=%v", segunda.ID, primera.ID, repetida, err)
	}
	if _, _, err := s.Reponer(ctx, "reposicion-gim-01", "var-calza-negra-m", 6, "admin"); !errors.Is(err, dominio.ErrClaveReutilizada) {
		t.Fatalf("misma clave con otra cantidad: %v", err)
	}
}

// Solo un rechazo definitivo devuelve las unidades al stock físico: en los demás casos las calzas pudieron haber
// llegado al gimnasio, y devolverlas contaría dos veces la misma mercadería.
func TestEnviarReposicionDecideSegunLaRespuestaDelGimnasio(t *testing.T) {
	casos := []struct {
		nombre      string
		err         error
		hecho       bool
		resolucion  string
		esperaDesde time.Duration // proximo intento, desde ahora (solo bloqueada y reprogramada)
		esperaHasta time.Duration
	}{
		{nombre: "201: confirmada", err: nil, hecho: true, resolucion: "confirmar:rep-1:gim-77"},
		{nombre: "rechazo definitivo: vuelven las unidades", err: fmt.Errorf("%w: 422", dominio.ErrReposicionRechazada), resolucion: "rechazar:rep-1"},
		{nombre: "409: a revisar, sin devolver", err: dominio.ErrReposicionConflicto, resolucion: "revisar:rep-1"},
		{nombre: "401: bloqueada, reintento en 5 minutos", err: dominio.ErrGimnasioConfiguracion, resolucion: "bloquear:rep-1",
			esperaDesde: dominio.EsperaBloqueada, esperaHasta: dominio.EsperaBloqueada},
		// Tercer intento: espera base de 20 s, más hasta un 20 % de variación.
		{nombre: "timeout: incierto, se reintenta", err: errors.New("context deadline exceeded"), resolucion: "reprogramar:rep-1",
			esperaDesde: 20 * time.Second, esperaHasta: 24 * time.Second},
	}
	for _, c := range casos {
		t.Run(c.nombre, func(t *testing.T) {
			repo := nuevoRepo()
			s, _ := servicioDePrueba(repo, gimnasioFalso{idGimnasio: "gim-77", err: c.err})
			reposicion := dominio.Reposicion{ID: "rep-1", VarianteID: "var-calza-negra-m", Cantidad: 4, Intentos: 2}

			hecho, err := s.EnviarReposicion(context.Background(), reposicion)
			if err != nil {
				t.Fatalf("error inesperado: %v", err)
			}
			if hecho != c.hecho {
				t.Errorf("hecho = %v, esperaba %v", hecho, c.hecho)
			}
			if len(repo.llamadasGimnasio) != 1 || repo.llamadasGimnasio[0] != c.resolucion {
				t.Fatalf("resolvió %v, esperaba [%s]", repo.llamadasGimnasio, c.resolucion)
			}
			if c.esperaHasta > 0 {
				espera := repo.proximoIntento.Sub(inicio)
				if espera < c.esperaDesde || espera > c.esperaHasta {
					t.Errorf("próximo intento en %v, esperaba entre %v y %v", espera, c.esperaDesde, c.esperaHasta)
				}
			}
		})
	}
}

func TestResumenDeReposicionesIncluyeTodosLosEstados(t *testing.T) {
	repo := nuevoRepo()
	repo.cantidadesPorEstado = map[dominio.EstadoReposicion]int{dominio.ReposicionPendiente: 2}
	repo.pendienteMasAntigua = inicio.Add(-12 * time.Minute)
	s, _ := servicioDePrueba(repo, gimnasioFalso{})

	resumen, espera, err := s.ResumirReposiciones(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(resumen) != len(dominio.EstadosReposicion) {
		t.Errorf("el resumen tiene %d estados, esperaba %d: las alertas necesitan ver también los que están en 0", len(resumen), len(dominio.EstadosReposicion))
	}
	if resumen[dominio.ReposicionPendiente] != 2 || resumen[dominio.ReposicionBloqueada] != 0 {
		t.Errorf("resumen %v", resumen)
	}
	if espera != 12*time.Minute {
		t.Errorf("la pendiente más antigua espera hace %v, esperaba 12m", espera)
	}

	repo.pendienteMasAntigua = time.Time{} // sin pendientes
	if _, espera, _ = s.ResumirReposiciones(context.Background()); espera != 0 {
		t.Errorf("sin pendientes, la espera tiene que ser 0: %v", espera)
	}
}
