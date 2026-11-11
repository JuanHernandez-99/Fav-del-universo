// Logs en MySQL (ADR-020): cada línea de INFO o más que sale por stdout también se guarda en la tabla app_logs de
// mysql-logs, para ver en Grafana todo lo que pasó con un track id. Este archivo es igual en los cuatro servicios.
//
// La escritura es asíncrona y por tandas: el log se encola y una goroutine lo guarda. Si la cola está llena o la
// base no responde, la línea se descarta (queda en stdout y se cuenta en logs_descartados_total). El servicio nunca
// espera a mysql-logs ni se cae por él.
package observabilidad

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	capacidadCola   = 10_000
	tamanoTanda     = 200
	cadaCuanto      = time.Second
	esperaGuardar   = 3 * time.Second
	esperaAlApagar  = 5 * time.Second
	avisoRepetido   = 30 * time.Second
	largoMaxTrackID = 100
)

var logsDescartados = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "logs_descartados_total",
	Help: "Líneas de log que no se guardaron en mysql-logs, por motivo (cola_llena, base_no_disponible, apagado). Siguen en stdout.",
}, []string{"motivo"})

// RegistroLog es una fila de app_logs: el modelo de la cátedra más la réplica y los atributos de la línea.
type RegistroLog struct {
	ID        uint64    `gorm:"column:id;primaryKey"`
	TrackID   string    `gorm:"column:track_id"`
	Stage     string    `gorm:"column:stage"` // el servicio
	Instancia string    `gorm:"column:instancia"`
	Level     string    `gorm:"column:level"` // info, warn o error
	Message   string    `gorm:"column:message"`
	Atributos string    `gorm:"column:atributos"` // JSON
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (RegistroLog) TableName() string { return "app_logs" }

// IniciarLogsMySQL suma la escritura en app_logs al logger por defecto. Se llama después de ConfigurarLogs.
// Devuelve la función que, al apagar, guarda lo pendiente con un tiempo máximo.
func IniciarLogsMySQL(servicio, instancia string) func() {
	if os.Getenv("LOGS_MYSQL_ACTIVO") == "false" {
		slog.Info("logs en MySQL apagados (LOGS_MYSQL_ACTIVO=false)")
		return func() {}
	}
	cfg := mysql.NewConfig()
	cfg.Net = "tcp"
	cfg.Addr = variableLogs("LOGS_MYSQL_HOST", "mysql-logs:3306")
	cfg.DBName = variableLogs("LOGS_MYSQL_DB", "logs")
	cfg.User = variableLogs("LOGS_MYSQL_USUARIO", "logs_escritor")
	cfg.Passwd = os.Getenv("LOGS_MYSQL_PASSWORD")

	db, err := AbrirBaseLogs(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "logs-mysql: no se pudo preparar la conexión, los logs quedan solo en stdout: %v\n", err)
		return func() {}
	}
	escritor := nuevoEscritor(destinoGORM{db}, capacidadCola, tamanoTanda, cadaCuanto, os.Stderr)
	go escritor.correr()
	slog.SetDefault(slog.New(nuevoManejadorMySQL(slog.Default().Handler(), escritor, servicio, instancia)))
	slog.Info("logs en MySQL activos", "base", cfg.Addr+"/"+cfg.DBName)

	return func() {
		escritor.cerrar(esperaAlApagar)
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
}

// AbrirBaseLogs prepara GORM sin conectarse todavía: el servicio arranca aunque mysql-logs esté caído, y la
// conexión se hace (o se rehace) al guardar cada tanda.
func AbrirBaseLogs(cfg *mysql.Config) (*gorm.DB, error) {
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.Timeout = 2 * time.Second
	cfg.ReadTimeout = esperaGuardar
	cfg.WriteTimeout = esperaGuardar
	// Los mensajes del driver van directo a stderr. Si pasaran por slog, una falla de la base generaría logs
	// que volverían a intentar guardarse en la misma base.
	cfg.Logger = log.New(os.Stderr, "logs-mysql: ", log.LstdFlags)
	conector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, err
	}
	sqlDB := sql.OpenDB(conector)
	sqlDB.SetMaxOpenConns(2)
	sqlDB.SetMaxIdleConns(2)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)
	return gorm.Open(gormmysql.New(gormmysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		Logger:                 logger.Discard, // el logger de GORM tampoco pasa por slog
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
	})
}

func variableLogs(nombre, porDefecto string) string {
	if v := os.Getenv(nombre); v != "" {
		return v
	}
	return porDefecto
}

// ---------- Escritor ----------

type destinoLogs interface {
	Guardar(ctx context.Context, tanda []RegistroLog) error
}

type destinoGORM struct{ db *gorm.DB }

func (d destinoGORM) Guardar(ctx context.Context, tanda []RegistroLog) error {
	return d.db.WithContext(ctx).CreateInBatches(tanda, len(tanda)).Error
}

type escritorLogs struct {
	cola     chan RegistroLog
	destino  destinoLogs
	tandaMax int
	cada     time.Duration
	avisos   io.Writer // stderr: los problemas del escritor nunca pasan por slog

	terminar     chan struct{}
	listo        chan struct{}
	limiteCierre time.Time

	// Solo los usa la goroutine de correr.
	caido         bool
	ultimoAviso   time.Time
	perdidasCaido int
}

func nuevoEscritor(destino destinoLogs, capacidad, tandaMax int, cada time.Duration, avisos io.Writer) *escritorLogs {
	return &escritorLogs{
		cola: make(chan RegistroLog, capacidad), destino: destino, tandaMax: tandaMax, cada: cada, avisos: avisos,
		terminar: make(chan struct{}), listo: make(chan struct{}),
	}
}

// encolar nunca espera: con la cola llena, la línea se descarta.
func (e *escritorLogs) encolar(r RegistroLog) {
	select {
	case e.cola <- r:
	default:
		logsDescartados.WithLabelValues("cola_llena").Inc()
	}
}

// correr junta tandas y las guarda cada `cada` o al llegar a tandaMax.
func (e *escritorLogs) correr() {
	defer close(e.listo)
	reloj := time.NewTicker(e.cada)
	defer reloj.Stop()
	tanda := make([]RegistroLog, 0, e.tandaMax)
	for {
		select {
		case r := <-e.cola:
			tanda = append(tanda, r)
			if len(tanda) >= e.tandaMax {
				e.guardar(context.Background(), tanda)
				tanda = make([]RegistroLog, 0, e.tandaMax)
			}
		case <-reloj.C:
			if len(tanda) > 0 {
				e.guardar(context.Background(), tanda)
				tanda = make([]RegistroLog, 0, e.tandaMax)
			}
		case <-e.terminar:
			e.vaciar(tanda)
			return
		}
	}
}

// vaciar guarda lo que quedó en la cola sin pasar el límite de cierre. Lo que no entra se descarta.
func (e *escritorLogs) vaciar(tanda []RegistroLog) {
	ctx, cancelar := context.WithDeadline(context.Background(), e.limiteCierre)
	defer cancelar()
	for {
		select {
		case r := <-e.cola:
			tanda = append(tanda, r)
			if len(tanda) < e.tandaMax {
				continue
			}
		default:
		}
		if len(tanda) == 0 {
			return
		}
		if ctx.Err() != nil {
			logsDescartados.WithLabelValues("apagado").Add(float64(len(tanda) + len(e.cola)))
			return
		}
		e.guardar(ctx, tanda)
		tanda = make([]RegistroLog, 0, e.tandaMax)
		if len(e.cola) == 0 {
			return
		}
	}
}

func (e *escritorLogs) guardar(ctx context.Context, tanda []RegistroLog) {
	ctx, cancelar := context.WithTimeout(ctx, esperaGuardar)
	defer cancelar()
	err := e.destino.Guardar(ctx, tanda)
	if err == nil {
		if e.caido {
			fmt.Fprintf(e.avisos, "logs-mysql: la base volvió a responder; mientras no respondía se descartaron %d líneas\n", e.perdidasCaido)
			e.caido, e.perdidasCaido = false, 0
		}
		return
	}
	logsDescartados.WithLabelValues("base_no_disponible").Add(float64(len(tanda)))
	e.perdidasCaido += len(tanda)
	// Un aviso al caer y después uno cada avisoRepetido, para no llenar stderr con una línea por segundo.
	if !e.caido || time.Since(e.ultimoAviso) >= avisoRepetido {
		fmt.Fprintf(e.avisos, "logs-mysql: no se pudo guardar en la base; van %d líneas descartadas (siguen en stdout): %v\n", e.perdidasCaido, err)
		e.ultimoAviso = time.Now()
	}
	e.caido = true
}

// cerrar guarda lo pendiente y vuelve a más tardar en `limite`, aunque la base no responda.
func (e *escritorLogs) cerrar(limite time.Duration) {
	e.limiteCierre = time.Now().Add(limite)
	close(e.terminar)
	reloj := time.NewTimer(limite)
	defer reloj.Stop()
	select {
	case <-e.listo:
	case <-reloj.C:
		fmt.Fprintf(e.avisos, "logs-mysql: se apagó sin terminar de guardar los logs pendientes\n")
	}
}

// ---------- Manejador de slog ----------

// manejadorMySQL envuelve al manejador de stdout: cada línea sale por stdout igual que antes y, si es INFO o más,
// además se encola para app_logs.
type manejadorMySQL struct {
	interno             slog.Handler
	escritor            *escritorLogs
	servicio, instancia string
	attrs               []attrConGrupo
	grupos              []string
}

type attrConGrupo struct {
	grupos []string
	attr   slog.Attr
}

func nuevoManejadorMySQL(interno slog.Handler, e *escritorLogs, servicio, instancia string) manejadorMySQL {
	return manejadorMySQL{interno: interno, escritor: e, servicio: servicio, instancia: instancia}
}

func (m manejadorMySQL) Enabled(ctx context.Context, nivel slog.Level) bool {
	return m.interno.Enabled(ctx, nivel) || nivel >= slog.LevelInfo
}

func (m manejadorMySQL) Handle(ctx context.Context, r slog.Record) error {
	var err error
	if m.interno.Enabled(ctx, r.Level) {
		err = m.interno.Handle(ctx, r.Clone())
	}
	if r.Level >= slog.LevelInfo {
		m.escritor.encolar(m.registro(ctx, r))
	}
	return err
}

func (m manejadorMySQL) WithAttrs(attrs []slog.Attr) slog.Handler {
	nuevo := m
	nuevo.interno = m.interno.WithAttrs(attrs)
	nuevo.attrs = append([]attrConGrupo(nil), m.attrs...)
	for _, a := range attrs {
		nuevo.attrs = append(nuevo.attrs, attrConGrupo{m.grupos, a})
	}
	return nuevo
}

func (m manejadorMySQL) WithGroup(nombre string) slog.Handler {
	if nombre == "" {
		return m
	}
	nuevo := m
	nuevo.interno = m.interno.WithGroup(nombre)
	nuevo.grupos = append(append([]string(nil), m.grupos...), nombre)
	return nuevo
}

// registro arma la fila. El track id es el correlation id del pedido; servicio e instancia van en sus columnas.
func (m manejadorMySQL) registro(ctx context.Context, r slog.Record) RegistroLog {
	atributos := map[string]any{}
	for _, a := range m.attrs {
		agregarAtributo(atributos, a.grupos, a.attr)
	}
	r.Attrs(func(a slog.Attr) bool {
		agregarAtributo(atributos, m.grupos, a)
		return true
	})

	trackID := CorrelationID(ctx)
	if v, ok := atributos["correlation_id"].(string); ok && trackID == "" {
		trackID = v
	}
	for _, clave := range []string{"correlation_id", "servicio", "instancia"} {
		delete(atributos, clave)
	}
	if len(trackID) > largoMaxTrackID {
		trackID = trackID[:largoMaxTrackID]
	}

	creado := r.Time
	if creado.IsZero() {
		creado = time.Now()
	}
	return RegistroLog{
		TrackID: trackID, Stage: m.servicio, Instancia: m.instancia, Level: nombreNivel(r.Level),
		Message: r.Message, Atributos: aJSON(atributos), CreatedAt: creado.UTC(),
	}
}

func nombreNivel(nivel slog.Level) string {
	switch {
	case nivel >= slog.LevelError:
		return "error"
	case nivel >= slog.LevelWarn:
		return "warn"
	default:
		return "info"
	}
}

func agregarAtributo(destino map[string]any, grupos []string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return
	}
	for _, g := range grupos {
		hijo, ok := destino[g].(map[string]any)
		if !ok {
			hijo = map[string]any{}
			destino[g] = hijo
		}
		destino = hijo
	}
	if a.Value.Kind() == slog.KindGroup {
		if a.Key == "" { // un grupo sin nombre se aplana
			for _, h := range a.Value.Group() {
				agregarAtributo(destino, nil, h)
			}
			return
		}
		hijo := map[string]any{}
		for _, h := range a.Value.Group() {
			agregarAtributo(hijo, nil, h)
		}
		destino[a.Key] = hijo
		return
	}
	destino[a.Key] = valorJSON(a.Value)
}

func valorJSON(v slog.Value) any {
	switch v.Kind() {
	case slog.KindTime:
		return v.Time().UTC().Format(time.RFC3339Nano)
	case slog.KindDuration:
		return v.Duration().String()
	case slog.KindAny:
		switch x := v.Any().(type) {
		case error:
			return x.Error()
		case fmt.Stringer:
			return x.String()
		}
		return v.Any()
	default:
		return v.Any()
	}
}

func aJSON(atributos map[string]any) string {
	if len(atributos) == 0 {
		return "{}"
	}
	if b, err := json.Marshal(atributos); err == nil {
		return string(b)
	}
	// Algún valor no se puede pasar a JSON: se guarda como texto.
	comoTexto := make(map[string]string, len(atributos))
	for k, v := range atributos {
		comoTexto[k] = strings.TrimSpace(fmt.Sprint(v))
	}
	b, _ := json.Marshal(comoTexto)
	return string(b)
}
