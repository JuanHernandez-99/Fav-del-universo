package aplicacion

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/dominio"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/puertos"
)

var t0 = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

type relojFijo struct{ t time.Time }

func (r *relojFijo) Ahora() time.Time { return r.t }

type idsSecuencia struct{ n int }

func (g *idsSecuencia) Nuevo() string { g.n++; return fmt.Sprintf("id-%d", g.n) }

// repoFalso guarda las reservas en memoria. Los métodos que un test no usa
// quedan sin implementar (la interfaz embebida es nil y entra en pánico).
type repoFalso struct {
	puertos.RepositorioInventario
	reservas map[string]dominio.Reserva
	eventos  []dominio.Evento
}

func (r *repoFalso) GuardarReservaSiAlcanza(_ context.Context, res dominio.Reserva, _, _ string) (dominio.Reserva, error) {
	r.reservas[res.ID] = res
	return res, nil
}

func (r *repoFalso) ObtenerReserva(_ context.Context, id string) (dominio.Reserva, error) {
	res, ok := r.reservas[id]
	if !ok {
		return dominio.Reserva{}, dominio.ErrReservaInexistente
	}
	return res, nil
}

func (r *repoFalso) ReservasParaVencer(_ context.Context, ahora time.Time, _ int) ([]string, error) {
	var ids []string
	for id, res := range r.reservas {
		if res.DebeVencer(ahora) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (r *repoFalso) VencerSiCorresponde(_ context.Context, id string, _ time.Time, e dominio.Evento) (bool, error) {
	res := r.reservas[id]
	res.Estado = dominio.EstadoVencida
	r.reservas[id] = res
	r.eventos = append(r.eventos, e)
	return true, nil
}

func TestVencerPendientesArmaElEventoConLaCorrelacion(t *testing.T) {
	repo := &repoFalso{reservas: map[string]dominio.Reserva{}}
	reloj := &relojFijo{t: t0}
	svc := NuevoServicio(repo, reloj, &idsSecuencia{})

	r, err := svc.Reservar(context.Background(), PedidoReserva{
		ClaveIdempotencia: "k1", Referencia: "venta-1", VencimientoMinutos: 20, CorrelacionID: "corr-1",
		Items: []dominio.Item{{VarianteID: "var-a", Cantidad: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.CorrelacionID != "corr-1" {
		t.Errorf("la reserva debe guardar la correlación del pedido; vino %q", r.CorrelacionID)
	}

	reloj.t = t0.Add(20 * time.Minute)
	n, err := svc.VencerPendientes(context.Background(), 10)
	if err != nil || n != 1 {
		t.Fatalf("vencidas=%d err=%v; se esperaba 1", n, err)
	}
	if len(repo.eventos) != 1 {
		t.Fatalf("eventos=%d; se esperaba 1", len(repo.eventos))
	}
	e := repo.eventos[0]
	datos, ok := e.Datos.(dominio.ReservaVencida)
	switch {
	case e.Tipo != dominio.TipoReservaVencida, e.Version != 1, e.ID == "", e.ID == r.ID:
		t.Errorf("sobre del evento incorrecto: %+v", e)
	case e.CorrelacionID != "corr-1", !e.OcurridoEn.Equal(reloj.t):
		t.Errorf("correlación u hora incorrectas: %+v", e)
	case !ok || datos.ReservaID != r.ID || datos.Referencia != "venta-1":
		t.Errorf("datos del evento incorrectos: %+v", e.Datos)
	}
}

func TestVencerPendientesSaltaLasQueYaNoCorresponden(t *testing.T) {
	// La lista de candidatas puede estar desactualizada: si al releerla la reserva
	// ya se confirmó, no se vence ni se genera evento.
	confirmada := dominio.Reserva{ID: "r1", Estado: dominio.EstadoConfirmada, VenceEn: t0}
	repo := &repoListaVieja{repoFalso{reservas: map[string]dominio.Reserva{"r1": confirmada}}}
	svc := NuevoServicio(repo, &relojFijo{t: t0.Add(time.Hour)}, &idsSecuencia{})

	n, err := svc.VencerPendientes(context.Background(), 10)
	if err != nil || n != 0 || len(repo.eventos) != 0 {
		t.Errorf("vencidas=%d eventos=%d err=%v; no debía vencer nada", n, len(repo.eventos), err)
	}
}

type repoListaVieja struct{ repoFalso }

func (r *repoListaVieja) ReservasParaVencer(context.Context, time.Time, int) ([]string, error) {
	return []string{"r1"}, nil
}

// ---------- Relevo del outbox ----------

type outboxFalso struct {
	pendientes []puertos.MensajeOutbox
	publicados []string
}

func (o *outboxFalso) Pendientes(_ context.Context, limite int) ([]puertos.MensajeOutbox, error) {
	var r []puertos.MensajeOutbox
	for _, m := range o.pendientes {
		if !o.publicado(m.ID) && len(r) < limite {
			r = append(r, m)
		}
	}
	return r, nil
}

func (o *outboxFalso) MarcarPublicado(_ context.Context, id string, _ time.Time) error {
	o.publicados = append(o.publicados, id)
	return nil
}

func (o *outboxFalso) publicado(id string) bool {
	for _, p := range o.publicados {
		if p == id {
			return true
		}
	}
	return false
}

type publicadorFalso struct {
	recibidos []string
	fallarEn  string // id del mensaje que falla
}

func (p *publicadorFalso) Publicar(_ context.Context, m puertos.MensajeOutbox) error {
	if m.ID == p.fallarEn {
		return errors.New("broker caído")
	}
	p.recibidos = append(p.recibidos, m.ID)
	return nil
}

func TestRelevoPublicaEnOrdenYSeDetieneAnteUnError(t *testing.T) {
	ob := &outboxFalso{pendientes: []puertos.MensajeOutbox{{ID: "e1"}, {ID: "e2"}, {ID: "e3"}}}
	pub := &publicadorFalso{fallarEn: "e2"}
	relevo := NuevoRelevo(ob, pub, &relojFijo{t: t0})

	n, err := relevo.PublicarPendientes(context.Background(), 10)
	if err == nil || n != 1 {
		t.Fatalf("publicados=%d err=%v; se esperaba 1 y un error", n, err)
	}
	if fmt.Sprint(pub.recibidos) != "[e1]" {
		t.Errorf("no debe publicar después del que falló, para no desordenar: %v", pub.recibidos)
	}

	pub.fallarEn = "" // el broker vuelve
	n, err = relevo.PublicarPendientes(context.Background(), 10)
	if err != nil || n != 2 {
		t.Fatalf("publicados=%d err=%v; se esperaban los 2 que faltaban", n, err)
	}
	if fmt.Sprint(pub.recibidos) != "[e1 e2 e3]" || fmt.Sprint(ob.publicados) != "[e1 e2 e3]" {
		t.Errorf("recibidos=%v marcados=%v", pub.recibidos, ob.publicados)
	}
	if n, _ := relevo.PublicarPendientes(context.Background(), 10); n != 0 {
		t.Errorf("sin pendientes no debe publicar nada; publicó %d", n)
	}
}
