package observabilidad

// Test de integración contra un MySQL real (ADR-020): el esquema de observabilidad/mysql-logs y el modelo de GORM
// tienen que coincidir. Necesita LOGS_MYSQL_TEST_DSN; sin ella se saltea. Con Docker Compose:
//
//	docker compose up -d mysql-logs
//	LOGS_MYSQL_TEST_DSN="root:cambiar@tcp(localhost:3310)/logs_test" go test ./internal/observabilidad/

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

const esquemaLogs = "../../../../observabilidad/mysql-logs/001_app_logs.sql"

func TestLogsMySQLGuardaLaLineaConSuTrackID(t *testing.T) {
	dsn := os.Getenv("LOGS_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("LOGS_MYSQL_TEST_DSN no definida: se saltea el test de integración")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DBName == "logs" {
		t.Fatal("el DSN de los tests apunta a la base de logs real: usar logs_test")
	}
	prepararBaseDeLogs(t, *cfg)

	db, err := AbrirBaseLogs(cfg.Clone())
	if err != nil {
		t.Fatal(err)
	}
	e := nuevoEscritor(destinoGORM{db}, 100, 200, 20*time.Millisecond, os.Stderr)
	go e.correr()
	logger := slog.New(nuevoManejadorMySQL(slog.DiscardHandler, e, "catalogo", "replica-test"))

	trackID := "test-integracion-" + time.Now().Format("150405.000000")
	ctx := ConCorrelationID(context.Background(), trackID)
	logger.InfoContext(ctx, "pedido http", "ruta", "/v1/tienda/productos", "codigo", 200)
	logger.ErrorContext(ctx, "no se pudo leer la remera", "producto_id", "prd-remera-saturno")
	e.cerrar(5 * time.Second)

	cfgLectura := cfg.Clone()
	cfgLectura.ParseTime = true
	lectura := abrir(t, cfgLectura.FormatDSN())
	filas, err := lectura.Query(`SELECT stage, instancia, level, message, JSON_EXTRACT(atributos, '$.ruta'), created_at
		FROM app_logs WHERE track_id = ? ORDER BY id`, trackID)
	if err != nil {
		t.Fatal(err)
	}
	defer filas.Close()
	var niveles []string
	for filas.Next() {
		var stage, instancia, nivel, mensaje string
		var ruta sql.NullString
		var creado time.Time
		if err := filas.Scan(&stage, &instancia, &nivel, &mensaje, &ruta, &creado); err != nil {
			t.Fatal(err)
		}
		if stage != "catalogo" || instancia != "replica-test" {
			t.Errorf("stage/instancia = %s/%s", stage, instancia)
		}
		if nivel == "info" && ruta.String != `"/v1/tienda/productos"` {
			t.Errorf("atributos.ruta = %q", ruta.String)
		}
		if time.Since(creado) > time.Minute || time.Since(creado) < -time.Minute {
			t.Errorf("created_at = %v: tiene que ser la hora UTC de la línea", creado)
		}
		niveles = append(niveles, nivel)
	}
	if len(niveles) != 2 || niveles[0] != "info" || niveles[1] != "error" {
		t.Fatalf("niveles guardados = %v, esperaba [info error]", niveles)
	}
}

// prepararBaseDeLogs crea la base de prueba y le aplica el mismo esquema que usa mysql-logs al crearse.
func prepararBaseDeLogs(t *testing.T, cfg mysql.Config) {
	t.Helper()
	esquema, err := os.ReadFile(esquemaLogs)
	if err != nil {
		t.Fatal(err)
	}
	nombre := cfg.DBName
	cfg.DBName = ""
	admin := abrir(t, cfg.FormatDSN())
	if _, err := admin.Exec("CREATE DATABASE IF NOT EXISTS " + nombre); err != nil {
		t.Fatal(err)
	}
	cfg.DBName = nombre
	cfg.MultiStatements = true
	base := abrir(t, cfg.FormatDSN())
	if _, err := base.Exec(string(esquema)); err != nil {
		t.Fatal(err)
	}
}

func abrir(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
