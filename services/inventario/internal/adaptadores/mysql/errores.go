package mysql

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"net"

	mysqldriver "github.com/go-sql-driver/mysql"
)

const (
	codigoServidorApagandose   = 1053
	codigoDemasiadasConexiones = 1040
)

// EsFallaDeConexion dice si err significa que no se pudo usar la base, a diferencia de una consulta que está mal.
func (r *Repositorio) EsFallaDeConexion(err error) bool {
	if errors.Is(err, driver.ErrBadConn) || errors.Is(err, mysqldriver.ErrInvalidConn) || errors.Is(err, sql.ErrConnDone) {
		return true
	}
	var red net.Error
	if errors.As(err, &red) {
		return true
	}
	var e *mysqldriver.MySQLError
	return errors.As(err, &e) && (e.Number == codigoServidorApagandose || e.Number == codigoDemasiadasConexiones)
}
