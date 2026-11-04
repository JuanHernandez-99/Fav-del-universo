package repository

// Tests de integración contra un MySQL real. Necesitan la variable USUARIOS_TEST_DSN; sin ella se saltean:
//
//	docker compose up -d mysql-usuarios
//	USUARIOS_TEST_DSN="root:cambiar@tcp(localhost:3309)/usuarios_test?parseTime=true&loc=UTC&multiStatements=true" go test ./...

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

	"github.com/JuanHernandez-99/Fav-del-universo/services/usuarios/internal/modelo"
)

func repoDePrueba(t *testing.T) *Repositorio {
	t.Helper()
	dsn := os.Getenv("USUARIOS_TEST_DSN")
	if dsn == "" {
		t.Skip("USUARIOS_TEST_DSN no definida: se saltea el test de integración")
	}
	cfg, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DBName == "usuarios" {
		t.Fatal("el DSN de los tests apunta a la base del servicio: usar usuarios_test")
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
	t.Cleanup(func() { db.Close() })
	repo := Nuevo(db)
	if err := repo.Migrar(context.Background()); err != nil {
		t.Fatal(err)
	}
	return repo
}

func usuarioNuevo(email string) modelo.Usuario {
	return modelo.Usuario{ID: uuid.NewString(), Email: email, Hash: "$2a$12$hash", Rol: modelo.RolComprador, CreadoEn: time.Now().UTC().Truncate(time.Second)}
}

func TestGuardaYLee(t *testing.T) {
	r := repoDePrueba(t)
	email := uuid.NewString() + "@example.com"
	nuevo := usuarioNuevo(email)
	if err := r.Insertar(context.Background(), nuevo); err != nil {
		t.Fatal(err)
	}
	leido, err := r.PorEmail(context.Background(), email)
	if err != nil || leido.ID != nuevo.ID || leido.Hash != nuevo.Hash || leido.Rol != modelo.RolComprador {
		t.Errorf("%+v, %v", leido, err)
	}
	if _, err := r.PorEmail(context.Background(), "nadie-"+email); !errors.Is(err, modelo.ErrUsuarioNoEncontrado) {
		t.Errorf("inexistente: %v", err)
	}
}

// HU-18.1: ocho registros simultáneos con el mismo email crean una sola cuenta. Lo decide la base.
func TestRegistrosSimultaneosConElMismoEmail(t *testing.T) {
	r := repoDePrueba(t)
	email := uuid.NewString() + "@example.com"
	var creados, repetidos atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			switch err := r.Insertar(context.Background(), usuarioNuevo(email)); {
			case err == nil:
				creados.Add(1)
			case errors.Is(err, modelo.ErrEmailEnUso):
				repetidos.Add(1)
			default:
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if creados.Load() != 1 || repetidos.Load() != 7 {
		t.Errorf("creados %d, repetidos %d", creados.Load(), repetidos.Load())
	}
}
