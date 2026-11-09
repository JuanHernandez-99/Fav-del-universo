package mysql

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
)

func TestEsFallaDeConexion(t *testing.T) {
	casos := []struct {
		nombre string
		err    error
		quiere bool
	}{
		{"conexión rechazada", &net.OpError{Op: "dial", Err: errors.New("connection refused")}, true},
		{"conexión inválida", mysqldriver.ErrInvalidConn, true},
		{"conexión mala", driver.ErrBadConn, true},
		{"conexión cerrada", sql.ErrConnDone, true},
		{"envuelta", fmt.Errorf("reservar: %w", mysqldriver.ErrInvalidConn), true},
		{"servidor apagándose", &mysqldriver.MySQLError{Number: codigoServidorApagandose}, true},
		{"demasiadas conexiones", &mysqldriver.MySQLError{Number: codigoDemasiadasConexiones}, true},
		{"clave duplicada", &mysqldriver.MySQLError{Number: codigoClaveDuplicada}, false},
		{"sin filas", sql.ErrNoRows, false},
		{"error cualquiera", errors.New("consulta mal escrita"), false},
	}
	r := &Repositorio{}
	for _, c := range casos {
		if r.EsFallaDeConexion(c.err) != c.quiere {
			t.Errorf("%s: se esperaba %v", c.nombre, c.quiere)
		}
	}
}
