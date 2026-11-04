// Package repository es el único lugar que habla con MySQL.
package repository

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/JuanHernandez-99/Fav-del-universo/services/usuarios/internal/modelo"
)

//go:embed sql/*.sql
var scripts embed.FS

const codigoClaveDuplicada = 1062

type Repositorio struct {
	db *sql.DB
}

func Nuevo(db *sql.DB) *Repositorio { return &Repositorio{db: db} }

func (r *Repositorio) Migrar(ctx context.Context) error {
	archivos, err := fs.Glob(scripts, "sql/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(archivos)
	for _, archivo := range archivos {
		contenido, err := scripts.ReadFile(archivo)
		if err != nil {
			return err
		}
		if _, err := r.db.ExecContext(ctx, string(contenido)); err != nil {
			return fmt.Errorf("aplicando %s: %w", archivo, err)
		}
	}
	return nil
}

func (r *Repositorio) Ping(ctx context.Context) error { return r.db.PingContext(ctx) }

// Insertar no pregunta antes si el email existe: inserta y deja que la restricción única decida. Así dos
// registros simultáneos con el mismo email no pueden pasar los dos.
func (r *Repositorio) Insertar(ctx context.Context, u modelo.Usuario) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO usuarios (id, email, hash, rol, creado_en) VALUES (?, ?, ?, ?, ?)`,
		u.ID, u.Email, u.Hash, u.Rol, u.CreadoEn)
	var e *mysqldriver.MySQLError
	if errors.As(err, &e) && e.Number == codigoClaveDuplicada {
		return modelo.ErrEmailEnUso
	}
	return err
}

func (r *Repositorio) PorEmail(ctx context.Context, email string) (modelo.Usuario, error) {
	var u modelo.Usuario
	err := r.db.QueryRowContext(ctx,
		`SELECT id, email, hash, rol, creado_en FROM usuarios WHERE email = ?`, email).
		Scan(&u.ID, &u.Email, &u.Hash, &u.Rol, &u.CreadoEn)
	if errors.Is(err, sql.ErrNoRows) {
		return modelo.Usuario{}, modelo.ErrUsuarioNoEncontrado
	}
	return u, err
}
