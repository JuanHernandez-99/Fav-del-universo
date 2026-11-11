package aplicacion

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/dominio"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/observabilidad"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/puertos"
)

// ---------- Dobles de los puertos ----------

type repoFalso struct {
	puertos.Repositorio
	ventas        map[string]dominio.Venta
	tareas        []dominio.TipoTarea
	mails         []dominio.Mail
	pagos         map[string]bool
	mensajes      map[string]bool
	completas     int
	reprogramadas int
	fallaAlCrear  error
	trackIDs      []string // el track id del contexto en cada cambio de una venta
}

func nuevoRepo() *repoFalso {
	return &repoFalso{ventas: map[string]dominio.Venta{}, pagos: map[string]bool{}, mensajes: map[string]bool{}}
}

func (r *repoFalso) CrearVenta(_ context.Context, v dominio.Venta, tareas []dominio.TipoTarea, mails []dominio.Mail) (dominio.Venta, bool, error) {
	if r.fallaAlCrear != nil {
		return dominio.Venta{}, false, r.fallaAlCrear
	}
	if g, ok := r.ventas[v.ID]; ok {
		return g, true, nil
	}
	r.ventas[v.ID] = v
	r.tareas = append(r.tareas, tareas...)
	r.mails = append(r.mails, mails...)
	return v, false, nil
}

func (r *repoFalso) VentaPorClave(_ context.Context, clave string) (dominio.Venta, error) {
	for _, v := range r.ventas {
		if v.Clave == clave {
			return v, nil
		}
	}
	return dominio.Venta{}, dominio.ErrVentaNoEncontrada
}

func (r *repoFalso) ObtenerVenta(_ context.Context, id string) (dominio.Venta, error) {
	v, ok := r.ventas[id]
	if !ok {
		return dominio.Venta{}, dominio.ErrVentaNoEncontrada
	}
	return v, nil
}

func (r *repoFalso) GuardarUrlPago(_ context.Context, id, url string) error {
	v := r.ventas[id]
	v.UrlPago = url
	r.ventas[id] = v
	return nil
}

func (r *repoFalso) ModificarVenta(ctx context.Context, id string, cambio func(*dominio.Venta) ([]dominio.TipoTarea, []dominio.Mail, error)) (dominio.Venta, error) {
	r.trackIDs = append(r.trackIDs, observabilidad.CorrelationID(ctx))
	v, ok := r.ventas[id]
	if !ok {
		return dominio.Venta{}, dominio.ErrVentaNoEncontrada
	}
	tareas, mails, err := cambio(&v)
	if err != nil {
		return dominio.Venta{}, err
	}
	r.ventas[id] = v
	r.tareas = append(r.tareas, tareas...)
	r.mails = append(r.mails, mails...)
	return v, nil
}

func (r *repoFalso) RegistrarAvisoPago(ctx context.Context, pagoID, ventaID string, cambio func(*dominio.Venta) ([]dominio.TipoTarea, []dominio.Mail, error)) error {
	if r.pagos[pagoID] {
		return nil
	}
	if _, err := r.ModificarVenta(ctx, ventaID, cambio); err != nil {
		return err
	}
	r.pagos[pagoID] = true
	return nil
}

func (r *repoFalso) ProcesarEvento(ctx context.Context, mensajeID, ventaID string, cambio func(*dominio.Venta) ([]dominio.Mail, error)) (bool, error) {
	if r.mensajes[mensajeID] {
		return false, nil
	}
	r.mensajes[mensajeID] = true
	_, err := r.ModificarVenta(ctx, ventaID, func(v *dominio.Venta) ([]dominio.TipoTarea, []dominio.Mail, error) {
		m, err := cambio(v)
		return nil, m, err
	})
	if errors.Is(err, dominio.ErrVentaNoEncontrada) {
		return true, nil
	}
	return true, err
}

func (r *repoFalso) CompletarTarea(context.Context, string) error { r.completas++; return nil }
func (r *repoFalso) ReprogramarTarea(context.Context, string, string, time.Time) error {
	r.reprogramadas++
	return nil
}

func (r *repoFalso) unica(t *testing.T) dominio.Venta {
	t.Helper()
	if len(r.ventas) != 1 {
		t.Fatalf("hay %d ventas", len(r.ventas))
	}
	for _, v := range r.ventas {
		return v
	}
	return dominio.Venta{}
}

type catalogoFalso struct {
	detalles []puertos.DetalleVariante
	err      error
	medio    dominio.Medio
}

func (c *catalogoFalso) Variantes(_ context.Context, ids []string, medio dominio.Medio) ([]puertos.DetalleVariante, error) {
	c.medio = medio
	return c.detalles, c.err
}

type inventarioFalso struct {
	reservas     map[string]puertos.Reserva
	errReservar  error
	errConfirmar error
	errLiberar   error
	errDescontar error
	claves       []string
	referencias  []string
	vigencias    []time.Duration
	confirmadas  int
	liberadas    int
	descuentos   []string
}

func nuevoInventario() *inventarioFalso {
	return &inventarioFalso{reservas: map[string]puertos.Reserva{}}
}

func (i *inventarioFalso) Reservar(_ context.Context, clave, referencia string, _ []dominio.LineaPedido, vigencia time.Duration) (puertos.Reserva, error) {
	i.claves, i.referencias, i.vigencias = append(i.claves, clave), append(i.referencias, referencia), append(i.vigencias, vigencia)
	if i.errReservar != nil {
		return puertos.Reserva{}, i.errReservar
	}
	if r, ok := i.reservas[clave]; ok {
		return r, nil
	}
	r := puertos.Reserva{ID: fmt.Sprintf("reserva-%d", len(i.reservas)+1), Estado: puertos.ReservaActiva, VenceEn: time.Now().Add(vigencia)}
	i.reservas[clave] = r
	return r, nil
}

func (i *inventarioFalso) Confirmar(context.Context, string) error {
	i.confirmadas++
	return i.errConfirmar
}
func (i *inventarioFalso) Liberar(context.Context, string) error { i.liberadas++; return i.errLiberar }
func (i *inventarioFalso) DescontarFisico(_ context.Context, clave, _ string, _ []dominio.LineaPedido) error {
	i.descuentos = append(i.descuentos, clave)
	return i.errDescontar
}

type pagosFalso struct {
	errCrear    error
	creados     int
	pago        puertos.Pago
	errConsulta error
	firmaOK     bool
}

func (p *pagosFalso) CrearPago(context.Context, dominio.Venta) (string, error) {
	p.creados++
	return "http://simulador/checkout/1", p.errCrear
}
func (p *pagosFalso) ConsultarPago(context.Context, string) (puertos.Pago, error) {
	return p.pago, p.errConsulta
}
func (p *pagosFalso) VerificarFirma(string, string, string) bool { return p.firmaOK }

type entorno struct {
	s    *Servicio
	repo *repoFalso
	cat  *catalogoFalso
	inv  *inventarioFalso
	mp   *pagosFalso
}

func armar() *entorno {
	e := &entorno{
		repo: nuevoRepo(),
		cat: &catalogoFalso{detalles: []puertos.DetalleVariante{
			{VarianteID: "var-remera", Nombre: "Remera básica", Talle: "M", Color: "Blanco", Publicado: true, Precio: 1_500_000},
		}},
		inv: nuevoInventario(),
		mp:  &pagosFalso{firmaOK: true},
	}
	e.s = NuevoServicio(e.repo, e.cat, e.inv, e.mp, nil, "admin@fav.example")
	return e
}

func compra(medio dominio.Medio) PedidoCompra {
	return PedidoCompra{Clave: "clave-compra-1", Email: "ana@example.com", Medio: medio,
		Lineas: []dominio.LineaPedido{{VarianteID: "var-remera", Cantidad: 3}}}
}

// ---------- Compra ----------

func TestComprarConMercadoPagoReservaVeinteMinutosYCreaElPago(t *testing.T) {
	e := armar()
	v, err := e.s.Comprar(context.Background(), compra(dominio.MedioMercadoPago))
	if err != nil {
		t.Fatal(err)
	}
	if v.Estado != dominio.EstadoPendiente || v.Total != 4_500_000 || v.ReservaID == "" {
		t.Errorf("venta: %+v", v)
	}
	if e.inv.vigencias[0] != 20*time.Minute {
		t.Errorf("vigencia %v (RN-08)", e.inv.vigencias[0])
	}
	if e.inv.referencias[0] != v.ID {
		t.Errorf("la referencia de la reserva debe ser la venta: %s", e.inv.referencias[0])
	}
	if v.UrlPago == "" || e.repo.ventas[v.ID].UrlPago == "" {
		t.Error("falta guardar la URL de pago")
	}
	if e.cat.medio != dominio.MedioMercadoPago {
		t.Errorf("el precio debe pedirse para el medio de pago: %s", e.cat.medio)
	}
}

func TestComprarConTransferenciaReservaCuarentaYOchoHorasYAvisaAlAdmin(t *testing.T) {
	e := armar()
	v, err := e.s.Comprar(context.Background(), compra(dominio.MedioTransferencia))
	if err != nil {
		t.Fatal(err)
	}
	if e.inv.vigencias[0] != 48*time.Hour || e.mp.creados != 0 {
		t.Errorf("vigencia %v, pagos creados %d", e.inv.vigencias[0], e.mp.creados)
	}
	if len(e.repo.mails) != 2 {
		t.Errorf("mails: %+v", e.repo.mails)
	}
	_ = v
}

// HU-11.8: un reintento por un error de red no crea otra venta ni otra reserva.
func TestComprarDosVecesConLaMismaClaveEsLaMismaVenta(t *testing.T) {
	e := armar()
	a, err := e.s.Comprar(context.Background(), compra(dominio.MedioMercadoPago))
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.s.Comprar(context.Background(), compra(dominio.MedioMercadoPago))
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID || len(e.repo.ventas) != 1 || len(e.inv.claves) != 1 || e.mp.creados != 1 {
		t.Errorf("ids %s/%s, ventas %d, reservas %d, pagos %d", a.ID, b.ID, len(e.repo.ventas), len(e.inv.claves), e.mp.creados)
	}
}

func TestLaMismaClaveConOtroPedidoSeRechaza(t *testing.T) {
	e := armar()
	if _, err := e.s.Comprar(context.Background(), compra(dominio.MedioMercadoPago)); err != nil {
		t.Fatal(err)
	}
	otro := compra(dominio.MedioMercadoPago)
	otro.Lineas[0].Cantidad = 1
	if _, err := e.s.Comprar(context.Background(), otro); !errors.Is(err, dominio.ErrClaveReutilizada) {
		t.Errorf("error: %v", err)
	}
}

// HU-11.9 y ADR-014: si Inventario no responde, no queda ninguna venta a medias.
func TestSiInventarioFallaNoQuedaVenta(t *testing.T) {
	e := armar()
	e.inv.errReservar = dominio.ErrInventarioNoDisponible
	if _, err := e.s.Comprar(context.Background(), compra(dominio.MedioMercadoPago)); !errors.Is(err, dominio.ErrInventarioNoDisponible) {
		t.Errorf("error: %v", err)
	}
	if len(e.repo.ventas) != 0 || e.mp.creados != 0 {
		t.Errorf("quedaron %d ventas y %d pagos", len(e.repo.ventas), e.mp.creados)
	}
}

// ADR-014: Ventas se cae entre reservar y guardar. El reintento usa la misma clave y recupera la reserva.
func TestReintentoDespuesDeCaerseEntreReservarYGuardar(t *testing.T) {
	e := armar()
	e.repo.fallaAlCrear = errors.New("se cortó la base")
	if _, err := e.s.Comprar(context.Background(), compra(dominio.MedioTransferencia)); err == nil {
		t.Fatal("debía fallar")
	}
	e.repo.fallaAlCrear = nil
	v, err := e.s.Comprar(context.Background(), compra(dominio.MedioTransferencia))
	if err != nil {
		t.Fatal(err)
	}
	if len(e.inv.reservas) != 1 || e.inv.claves[0] != e.inv.claves[1] || e.inv.referencias[0] != e.inv.referencias[1] {
		t.Errorf("reservas %d, claves %v, referencias %v", len(e.inv.reservas), e.inv.claves, e.inv.referencias)
	}
	if v.ReservaID != "reserva-1" {
		t.Errorf("reserva %s", v.ReservaID)
	}
}

func TestReintentoConLaReservaYaVencidaSeRechaza(t *testing.T) {
	e := armar()
	e.repo.fallaAlCrear = errors.New("se cortó la base")
	_, _ = e.s.Comprar(context.Background(), compra(dominio.MedioTransferencia))
	e.repo.fallaAlCrear = nil
	for clave, r := range e.inv.reservas {
		r.Estado = "VENCIDA"
		e.inv.reservas[clave] = r
	}
	if _, err := e.s.Comprar(context.Background(), compra(dominio.MedioTransferencia)); !errors.Is(err, dominio.ErrClaveReutilizada) {
		t.Errorf("error: %v", err)
	}
	if len(e.repo.ventas) != 0 {
		t.Error("no debe crear una venta con la reserva vencida")
	}
}

// Si Mercado Pago falla, la venta queda creada y el reintento retoma solo el pago.
func TestSiMercadoPagoFallaElReintentoRetomaElPago(t *testing.T) {
	e := armar()
	e.mp.errCrear = dominio.ErrPagoNoDisponible
	if _, err := e.s.Comprar(context.Background(), compra(dominio.MedioMercadoPago)); !errors.Is(err, dominio.ErrPagoNoDisponible) {
		t.Fatalf("error: %v", err)
	}
	e.mp.errCrear = nil
	v, err := e.s.Comprar(context.Background(), compra(dominio.MedioMercadoPago))
	if err != nil {
		t.Fatal(err)
	}
	if v.UrlPago == "" || len(e.inv.claves) != 1 || len(e.repo.ventas) != 1 {
		t.Errorf("url %q, reservas %d, ventas %d", v.UrlPago, len(e.inv.claves), len(e.repo.ventas))
	}
}

func TestVarianteNoPublicadaSeRechazaEnLaCompra(t *testing.T) {
	e := armar()
	e.cat.detalles[0].Publicado = false
	var inexistente *dominio.ErrorVarianteInexistente
	if _, err := e.s.Comprar(context.Background(), compra(dominio.MedioMercadoPago)); !errors.As(err, &inexistente) {
		t.Errorf("error: %v", err)
	}
	if len(e.inv.claves) != 0 {
		t.Error("no debe reservar")
	}
}

// Catálogo conoce la variante pero Inventario todavía no tiene su fila: para el comprador es falta de stock.
func TestVarianteSinFilaDeStockEsStockInsuficiente(t *testing.T) {
	e := armar()
	e.inv.errReservar = &dominio.ErrorVarianteInexistente{Variantes: []string{"var-remera"}}
	var stock *dominio.ErrorStockInsuficiente
	_, err := e.s.Comprar(context.Background(), compra(dominio.MedioMercadoPago))
	if !errors.As(err, &stock) || stock.Faltantes[0].Solicitada != 3 {
		t.Errorf("error: %v", err)
	}
}

func TestPedidoExternoNoDejaEmailYVaPorTransferencia(t *testing.T) {
	e := armar()
	v, err := e.s.PedirExterno(context.Background(), "clave-grupo-1", "OC-1", []dominio.LineaPedido{{VarianteID: "var-remera", Cantidad: 3}})
	if err != nil {
		t.Fatal(err)
	}
	if v.Canal != dominio.CanalGrupoExterno || v.Medio != dominio.MedioTransferencia || v.Email != "" || v.ReferenciaExterna != "OC-1" {
		t.Errorf("venta: %+v", v)
	}
	if e.inv.vigencias[0] != 48*time.Hour {
		t.Errorf("vigencia %v (HU-26)", e.inv.vigencias[0])
	}
	if len(e.repo.mails) != 1 || e.repo.mails[0].Destinatario != "admin@fav.example" {
		t.Errorf("mails: %+v", e.repo.mails)
	}
}

// ---------- Pagos ----------

func avisoAprobado(e *entorno, ventaID string) puertos.Pago {
	return puertos.Pago{ID: "pago-1", VentaID: ventaID, Estado: puertos.PagoAprobado}
}

func TestAvisoConFirmaInvalidaSeRechaza(t *testing.T) {
	e := armar()
	e.mp.firmaOK = false
	if err := e.s.AvisoPago(context.Background(), "x", "y", "pago-1"); !errors.Is(err, dominio.ErrFirmaInvalida) {
		t.Errorf("error: %v", err)
	}
}

func TestAvisoAprobadoDejaLaTareaDeConfirmarYRepetidoNoHaceNada(t *testing.T) {
	e := armar()
	v, _ := e.s.Comprar(context.Background(), compra(dominio.MedioMercadoPago))
	e.mp.pago = avisoAprobado(e, v.ID)
	for range 2 {
		if err := e.s.AvisoPago(context.Background(), "f", "r", "pago-1"); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.repo.tareas) != 1 || e.repo.tareas[0] != dominio.TareaConfirmar {
		t.Errorf("tareas: %v (HU-11.6)", e.repo.tareas)
	}
	if e.repo.ventas[v.ID].Estado != dominio.EstadoPendiente {
		t.Error("hasta que confirme Inventario sigue pendiente")
	}
}

func TestAvisoRechazadoLiberaLaReserva(t *testing.T) {
	e := armar()
	v, _ := e.s.Comprar(context.Background(), compra(dominio.MedioMercadoPago))
	e.mp.pago = puertos.Pago{ID: "pago-2", VentaID: v.ID, Estado: puertos.PagoRechazado}
	if err := e.s.AvisoPago(context.Background(), "f", "r", "pago-2"); err != nil {
		t.Fatal(err)
	}
	if e.repo.ventas[v.ID].Estado != dominio.EstadoCancelada || len(e.repo.tareas) != 1 || e.repo.tareas[0] != dominio.TareaLiberar {
		t.Errorf("estado %s, tareas %v (HU-11.4)", e.repo.ventas[v.ID].Estado, e.repo.tareas)
	}
}

func TestAvisoSinResultadoNoHaceNada(t *testing.T) {
	e := armar()
	v, _ := e.s.Comprar(context.Background(), compra(dominio.MedioMercadoPago))
	e.mp.pago = puertos.Pago{ID: "pago-3", VentaID: v.ID, Estado: puertos.PagoPendiente}
	if err := e.s.AvisoPago(context.Background(), "f", "r", "pago-3"); err != nil {
		t.Fatal(err)
	}
	if len(e.repo.tareas) != 0 || e.repo.pagos["pago-3"] {
		t.Error("un pago pendiente no se registra: llegará otro aviso con el mismo id")
	}
}

func TestAvisoConMercadoPagoCaidoSeReintenta(t *testing.T) {
	e := armar()
	e.mp.errConsulta = dominio.ErrPagoNoDisponible
	if err := e.s.AvisoPago(context.Background(), "f", "r", "pago-1"); !errors.Is(err, dominio.ErrPagoNoDisponible) {
		t.Errorf("error: %v", err)
	}
}

func TestAvisoDeVentaInexistenteNoFalla(t *testing.T) {
	e := armar()
	e.mp.pago = avisoAprobado(e, "no-existe")
	if err := e.s.AvisoPago(context.Background(), "f", "r", "pago-1"); err != nil {
		t.Errorf("error: %v", err)
	}
}

func TestConfirmarTransferencia(t *testing.T) {
	e := armar()
	v, _ := e.s.Comprar(context.Background(), compra(dominio.MedioTransferencia))
	for range 2 { // HU-15.3
		if _, err := e.s.ConfirmarTransferencia(context.Background(), v.ID); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.repo.tareas) != 1 {
		t.Errorf("tareas: %v", e.repo.tareas)
	}
}

func TestConfirmarTransferenciaDeVentaVencidaQuedaAVerificar(t *testing.T) {
	e := armar()
	v, _ := e.s.Comprar(context.Background(), compra(dominio.MedioTransferencia))
	e.repo.mails = nil
	if err := e.s.ProcesarReservaVencida(context.Background(), "m1", v.ReservaID, v.ID); err != nil {
		t.Fatal(err)
	}
	got, err := e.s.ConfirmarTransferencia(context.Background(), v.ID)
	if err != nil || got.Estado != dominio.EstadoAVerificar {
		t.Errorf("estado %s, error %v (HU-15.2)", got.Estado, err)
	}
	if len(e.repo.mails) != 1 || e.repo.mails[0].Tipo != dominio.MailAVerificar {
		t.Errorf("mails: %+v (HU-21.3)", e.repo.mails)
	}
}

func TestConfirmarTransferenciaDeUnaVentaConMercadoPagoSeRechaza(t *testing.T) {
	e := armar()
	v, _ := e.s.Comprar(context.Background(), compra(dominio.MedioMercadoPago))
	if _, err := e.s.ConfirmarTransferencia(context.Background(), v.ID); !errors.Is(err, dominio.ErrTransicionInvalida) {
		t.Errorf("error: %v", err)
	}
}

// ---------- Tareas ----------

func ventaConPago(t *testing.T, e *entorno) dominio.Venta {
	t.Helper()
	v, err := e.s.Comprar(context.Background(), compra(dominio.MedioMercadoPago))
	if err != nil {
		t.Fatal(err)
	}
	e.mp.pago = avisoAprobado(e, v.ID)
	if err := e.s.AvisoPago(context.Background(), "f", "r", "pago-1"); err != nil {
		t.Fatal(err)
	}
	e.repo.mails = nil
	return e.repo.ventas[v.ID]
}

func TestTareaConfirmarPasaLaVentaAPagadaYEnviaMails(t *testing.T) {
	e := armar()
	v := ventaConPago(t, e)
	if hecho, err := e.s.EjecutarTarea(context.Background(), dominio.Tarea{ID: "t1", VentaID: v.ID, Tipo: dominio.TareaConfirmar}); err != nil || !hecho {
		t.Fatalf("%v %v", hecho, err)
	}
	if e.repo.ventas[v.ID].Estado != dominio.EstadoPagada || len(e.repo.mails) != 2 || e.repo.completas != 1 {
		t.Errorf("estado %s, mails %d, completas %d", e.repo.ventas[v.ID].Estado, len(e.repo.mails), e.repo.completas)
	}
	// Se cayó después de pagar y antes de completar la tarea: el reintento no repite los mails.
	if _, err := e.s.EjecutarTarea(context.Background(), dominio.Tarea{ID: "t1", VentaID: v.ID, Tipo: dominio.TareaConfirmar}); err != nil {
		t.Fatal(err)
	}
	if len(e.repo.mails) != 2 {
		t.Errorf("mails repetidos: %d", len(e.repo.mails))
	}
}

// RN-14 y ADR-005 punto 4: Inventario decide; si la reserva venció, la venta queda a verificar.
func TestTareaConfirmarConReservaVencidaQuedaAVerificar(t *testing.T) {
	for _, errInventario := range []error{dominio.ErrReservaVencida, dominio.ErrReservaLiberada, dominio.ErrReservaNoEncontrada} {
		e := armar()
		v := ventaConPago(t, e)
		e.inv.errConfirmar = errInventario
		if _, err := e.s.EjecutarTarea(context.Background(), dominio.Tarea{ID: "t1", VentaID: v.ID, Tipo: dominio.TareaConfirmar}); err != nil {
			t.Fatal(err)
		}
		if e.repo.ventas[v.ID].Estado != dominio.EstadoAVerificar || len(e.repo.mails) != 1 || e.repo.completas != 1 {
			t.Errorf("%v: estado %s, mails %d", errInventario, e.repo.ventas[v.ID].Estado, len(e.repo.mails))
		}
	}
}

// CU-01 8a: con Inventario caído la tarea se reprograma y la venta sigue pendiente.
func TestTareaConInventarioCaidoSeReprograma(t *testing.T) {
	e := armar()
	v := ventaConPago(t, e)
	e.inv.errConfirmar = dominio.ErrInventarioNoDisponible
	hecho, err := e.s.EjecutarTarea(context.Background(), dominio.Tarea{ID: "t1", VentaID: v.ID, Tipo: dominio.TareaConfirmar})
	if err != nil || hecho {
		t.Fatalf("%v %v", hecho, err)
	}
	if e.repo.reprogramadas != 1 || e.repo.completas != 0 || e.repo.ventas[v.ID].Estado != dominio.EstadoPendiente {
		t.Errorf("reprogramadas %d, completas %d, estado %s", e.repo.reprogramadas, e.repo.completas, e.repo.ventas[v.ID].Estado)
	}
}

func TestTareaLiberar(t *testing.T) {
	e := armar()
	v, _ := e.s.Comprar(context.Background(), compra(dominio.MedioTransferencia))
	if _, err := e.s.Cancelar(context.Background(), v.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.s.EjecutarTarea(context.Background(), dominio.Tarea{ID: "t1", VentaID: v.ID, Tipo: dominio.TareaLiberar}); err != nil {
		t.Fatal(err)
	}
	if e.inv.liberadas != 1 || e.repo.completas != 1 {
		t.Errorf("liberadas %d, completas %d", e.inv.liberadas, e.repo.completas)
	}
}

// ---------- Cancelar y evento ----------

func TestCancelarVentaAjenaRespondeComoInexistente(t *testing.T) {
	e := armar()
	p := compra(dominio.MedioTransferencia)
	p.CompradorID = "usuario-1"
	v, _ := e.s.Comprar(context.Background(), p)
	if _, err := e.s.Cancelar(context.Background(), v.ID, "usuario-2"); !errors.Is(err, dominio.ErrVentaNoEncontrada) {
		t.Errorf("error: %v", err)
	}
	if got, err := e.s.Cancelar(context.Background(), v.ID, "usuario-1"); err != nil || got.Estado != dominio.EstadoCancelada {
		t.Errorf("dueño: %s %v", got.Estado, err)
	}
}

func TestCancelarDosVecesLiberaUnaSolaVez(t *testing.T) {
	e := armar()
	v, _ := e.s.Comprar(context.Background(), compra(dominio.MedioTransferencia))
	for range 2 {
		if _, err := e.s.Cancelar(context.Background(), v.ID, ""); err != nil {
			t.Fatal(err)
		}
	}
	if len(e.repo.tareas) != 1 {
		t.Errorf("tareas: %v", e.repo.tareas)
	}
}

func TestEventoReservaVencidaVenceLaVentaUnaSolaVez(t *testing.T) {
	e := armar()
	v, _ := e.s.Comprar(context.Background(), compra(dominio.MedioMercadoPago))
	for range 2 {
		if err := e.s.ProcesarReservaVencida(context.Background(), "mensaje-1", v.ReservaID, v.ID); err != nil {
			t.Fatal(err)
		}
	}
	if e.repo.ventas[v.ID].Estado != dominio.EstadoVencida {
		t.Errorf("estado: %s", e.repo.ventas[v.ID].Estado)
	}
}

func TestEventoDeOtraReservaOSinVentaNoHaceNada(t *testing.T) {
	e := armar()
	v, _ := e.s.Comprar(context.Background(), compra(dominio.MedioMercadoPago))
	if err := e.s.ProcesarReservaVencida(context.Background(), "m1", "otra-reserva", v.ID); err != nil {
		t.Fatal(err)
	}
	if e.repo.ventas[v.ID].Estado != dominio.EstadoPendiente {
		t.Errorf("estado: %s", e.repo.ventas[v.ID].Estado)
	}
	if err := e.s.ProcesarReservaVencida(context.Background(), "m2", "r", "venta-que-no-existe"); err != nil {
		t.Errorf("sin venta: %v", err)
	}
}

// ---------- Venta del local ----------

func pedidoLocal() PedidoLocal {
	monto := int64(1_000_000)
	return PedidoLocal{Clave: "clave-local-1", Medio: dominio.MedioEfectivo, Monto: &monto, Lineas: []dominio.LineaPedido{{VarianteID: "var-remera", Cantidad: 1}}}
}

// HU-17: registra lo cobrado (no el precio de lista) y deja la tarea de descontar el físico.
func TestVentaLocalGuardaElMontoCobradoYDescuentaUnaVez(t *testing.T) {
	e := armar()
	for range 2 {
		if _, err := e.s.RegistrarVentaLocal(context.Background(), pedidoLocal()); err != nil {
			t.Fatal(err)
		}
	}
	v := e.repo.unica(t)
	if v.Canal != dominio.CanalLocal || v.Total != 1_000_000 || v.Estado != dominio.EstadoRegistrada {
		t.Errorf("venta: %+v", v)
	}
	if len(e.repo.tareas) != 1 || e.repo.tareas[0] != dominio.TareaDescontar {
		t.Errorf("tareas: %v", e.repo.tareas)
	}
	if _, err := e.s.EjecutarTarea(context.Background(), dominio.Tarea{ID: "t", VentaID: v.ID, Tipo: dominio.TareaDescontar}); err != nil {
		t.Fatal(err)
	}
	if len(e.inv.descuentos) != 1 {
		t.Errorf("descuentos: %v", e.inv.descuentos)
	}
}

// RN-04: publicado solo habilita la web; el local vende igual.
func TestVentaLocalAceptaVarianteNoPublicada(t *testing.T) {
	e := armar()
	e.cat.detalles[0].Publicado = false
	if _, err := e.s.RegistrarVentaLocal(context.Background(), pedidoLocal()); err != nil {
		t.Errorf("error: %v", err)
	}
}

func TestVentaLocalSigueConCatalogoCaido(t *testing.T) {
	e := armar()
	e.cat.err = dominio.ErrCatalogoNoDisponible
	e.cat.detalles = nil
	v, err := e.s.RegistrarVentaLocal(context.Background(), pedidoLocal())
	if err != nil || v.Items[0].VarianteID != "var-remera" {
		t.Errorf("venta %+v, error %v", v, err)
	}
}

func TestSugerirMontoUsaElPrecioDelMedio(t *testing.T) {
	e := armar()
	monto, err := e.s.SugerirMonto(context.Background(), dominio.MedioEfectivo, []dominio.LineaPedido{{VarianteID: "var-remera", Cantidad: 2}})
	if err != nil || monto != 3_000_000 || e.cat.medio != dominio.MedioEfectivo {
		t.Errorf("monto %d, medio %s, error %v", monto, e.cat.medio, err)
	}
}

func TestCompraDeOtroCompradorNoSeVe(t *testing.T) {
	e := armar()
	p := compra(dominio.MedioTransferencia)
	p.CompradorID = "usuario-1"
	v, _ := e.s.Comprar(context.Background(), p)
	if _, err := e.s.CompraDe(context.Background(), v.ID, "usuario-2"); !errors.Is(err, dominio.ErrVentaNoEncontrada) {
		t.Errorf("error: %v", err)
	}
	if _, err := e.s.CompraDe(context.Background(), v.ID, "usuario-1"); err != nil {
		t.Errorf("dueño: %v", err)
	}
}

// ---------- Track id de la venta (ADR-020) ----------

// compraConTrackID crea una venta como si viniera del pedido "compra-remera-1". El repositorio real guarda el
// track id del contexto al crearla; el doble no, así que se lo pone el test.
func compraConTrackID(t *testing.T, e *entorno, medio dominio.Medio) dominio.Venta {
	t.Helper()
	v, err := e.s.Comprar(context.Background(), compra(medio))
	if err != nil {
		t.Fatal(err)
	}
	v.TrackID = "compra-remera-1"
	e.repo.ventas[v.ID] = v
	e.repo.trackIDs = nil
	return v
}

func TestElAvisoDePagoSigueConElTrackIDDeLaCompra(t *testing.T) {
	e := armar()
	v := compraConTrackID(t, e, dominio.MedioMercadoPago)
	e.mp.pago = avisoAprobado(e, v.ID)

	ctxAviso := observabilidad.ConCorrelationID(context.Background(), "aviso-mercadopago-9")
	if err := e.s.AvisoPago(ctxAviso, "f", "r", "pago-1"); err != nil {
		t.Fatal(err)
	}
	if len(e.repo.trackIDs) != 1 || e.repo.trackIDs[0] != "compra-remera-1" {
		t.Errorf("el pago se registró con %v, esperaba el track id de la compra", e.repo.trackIDs)
	}
}

func TestLaTransferenciaConfirmadaSigueConElTrackIDDeLaCompra(t *testing.T) {
	e := armar()
	v := compraConTrackID(t, e, dominio.MedioTransferencia)

	ctxAdmin := observabilidad.ConCorrelationID(context.Background(), "panel-admin-3")
	if _, err := e.s.ConfirmarTransferencia(ctxAdmin, v.ID); err != nil {
		t.Fatal(err)
	}
	if len(e.repo.trackIDs) != 1 || e.repo.trackIDs[0] != "compra-remera-1" {
		t.Errorf("la transferencia se registró con %v, esperaba el track id de la compra", e.repo.trackIDs)
	}
}

// Una venta guardada antes de ADR-020 no tiene track id: el pago sigue con el del pedido que llegó.
func TestUnaVentaSinTrackIDSigueConElDelPedido(t *testing.T) {
	e := armar()
	v, _ := e.s.Comprar(context.Background(), compra(dominio.MedioMercadoPago))
	e.repo.trackIDs = nil
	e.mp.pago = avisoAprobado(e, v.ID)

	ctxAviso := observabilidad.ConCorrelationID(context.Background(), "aviso-mercadopago-9")
	if err := e.s.AvisoPago(ctxAviso, "f", "r", "pago-1"); err != nil {
		t.Fatal(err)
	}
	if len(e.repo.trackIDs) != 1 || e.repo.trackIDs[0] != "aviso-mercadopago-9" {
		t.Errorf("trackIDs = %v", e.repo.trackIDs)
	}
}
