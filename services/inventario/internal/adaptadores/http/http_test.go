package http

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/aplicacion"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/dominio"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/puertos"
)

type relojFijo struct{}

func (relojFijo) Ahora() time.Time { return time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC) }

type idFijo struct{}

func (idFijo) Nuevo() string { return "r1" }

// repoFalso registra la reserva que le llega; el resto de los métodos no se usa.
type repoFalso struct {
	puertos.RepositorioInventario
	guardada dominio.Reserva
}

func (r *repoFalso) GuardarReservaSiAlcanza(_ context.Context, res dominio.Reserva, _, _ string) (dominio.Reserva, error) {
	r.guardada = res
	return res, nil
}

func TestReservarAceptaCamposDesconocidosYGuardaLaCorrelacion(t *testing.T) {
	repo := &repoFalso{}
	h := NuevoHandler(aplicacion.NuevoServicio(repo, relojFijo{}, idFijo{}), slog.New(slog.NewTextHandler(io.Discard, nil)))

	// "campoNuevo" simula un campo agregado por una versión menor posterior:
	// la compatibilidad aditiva exige ignorarlo, no responder 400.
	cuerpo := `{"referencia":"venta-1","vencimientoMinutos":20,"campoNuevo":true,
		"items":[{"varianteId":"var-a","cantidad":1,"otroCampo":"x"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/reservas", strings.NewReader(cuerpo))
	req.Header.Set("Idempotency-Key", "k1")
	req.Header.Set("X-Correlation-Id", "corr-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d cuerpo=%s; se esperaba 201", rec.Code, rec.Body)
	}
	if repo.guardada.CorrelacionID != "corr-1" {
		t.Errorf("la reserva debe guardar el X-Correlation-Id; vino %q", repo.guardada.CorrelacionID)
	}
	if got := rec.Header().Get("X-Correlation-Id"); got != "corr-1" {
		t.Errorf("X-Correlation-Id de la respuesta=%q", got)
	}
}
