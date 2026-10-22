// Package mysql implementa el puerto Repositorio sobre MySQL. Las invariantes de stock se cumplen
// con actualizaciones condicionales dentro de una transacción: la base decide la carrera entre
// dos operaciones sobre las mismas unidades.
package mysql

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/dominio"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/observabilidad"
)

//go:embed sql/*.sql
var scripts embed.FS

const codigoClaveDuplicada = 1062

type Repositorio struct {
	db *sql.DB
}

func NuevoRepositorio(db *sql.DB) *Repositorio { return &Repositorio{db: db} }

// Migrar aplica el esquema. Con datosDeEjemplo, carga también el stock inicial para las pruebas.
func (r *Repositorio) Migrar(ctx context.Context, datosDeEjemplo bool) error {
	archivos, err := fs.Glob(scripts, "sql/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(archivos)
	for _, archivo := range archivos {
		if strings.Contains(archivo, "datos_de_ejemplo") && !datosDeEjemplo {
			continue
		}
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

// ---------- Reservas ----------

func (r *Repositorio) CrearReserva(ctx context.Context, res dominio.Reserva) (dominio.Reserva, bool, error) {
	if existente, err := r.reservaPorClave(ctx, r.db, res.ClaveIdempotencia); err == nil {
		return existente, true, nil
	} else if !errors.Is(err, dominio.ErrReservaNoEncontrada) {
		return dominio.Reserva{}, false, err
	}

	err := r.enTransaccion(ctx, func(tx *sql.Tx) error {
		// Primero la reserva: si otro pedido con la misma clave entró a la vez, la restricción única lo frena.
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO reservas (id, clave_idempotencia, huella, referencia, estado, vence_en, creada_en, actualizada_en)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			res.ID, res.ClaveIdempotencia, res.Huella, res.Referencia, res.Estado, res.VenceEn, res.CreadaEn, res.ActualizadaEn); err != nil {
			return err
		}

		// Los ítems vienen ordenados por variante: todas las reservas bloquean las filas en el mismo orden.
		var faltantes []dominio.Faltante
		var inexistentes []string
		for _, it := range res.Items {
			resultado, err := tx.ExecContext(ctx,
				`UPDATE stock SET reservado = reservado + ?, actualizado_en = NOW()
				 WHERE variante_id = ? AND online - reservado >= ?`,
				it.Cantidad, it.VarianteID, it.Cantidad)
			if err != nil {
				return err
			}
			if n, _ := resultado.RowsAffected(); n == 1 {
				continue
			}
			// No alcanzó: se sigue revisando el resto para informar todos los faltantes juntos.
			disponible, existe, err := disponibleOnline(ctx, tx, it.VarianteID)
			if err != nil {
				return err
			}
			if !existe {
				inexistentes = append(inexistentes, it.VarianteID)
			} else {
				faltantes = append(faltantes, dominio.Faltante{VarianteID: it.VarianteID, Solicitada: it.Cantidad, Disponible: disponible})
			}
		}
		if len(inexistentes) > 0 {
			return &dominio.ErrorVarianteInexistente{Variantes: inexistentes}
		}
		if len(faltantes) > 0 {
			return &dominio.ErrorStockInsuficiente{Faltantes: faltantes}
		}

		for _, it := range res.Items {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO reserva_items (reserva_id, variante_id, cantidad) VALUES (?, ?, ?)`,
				res.ID, it.VarianteID, it.Cantidad); err != nil {
				return err
			}
		}
		return nil
	})

	if esClaveDuplicada(err) {
		existente, errLeer := r.reservaPorClave(ctx, r.db, res.ClaveIdempotencia)
		return existente, true, errLeer
	}
	if err != nil {
		return dominio.Reserva{}, false, err
	}
	return res, false, nil
}

func (r *Repositorio) ObtenerReserva(ctx context.Context, id string) (dominio.Reserva, error) {
	return r.reserva(ctx, r.db, `WHERE id = ?`, id)
}

func (r *Repositorio) ModificarReserva(ctx context.Context, id string, transicion func(*dominio.Reserva) error) (dominio.Reserva, error) {
	var resultado dominio.Reserva
	var errNegocio error

	err := r.enTransaccion(ctx, func(tx *sql.Tx) error {
		res, err := r.reserva(ctx, tx, `WHERE id = ? FOR UPDATE`, id)
		if err != nil {
			return err
		}
		anterior := res.Estado
		errNegocio = transicion(&res)
		resultado = res
		if res.Estado == anterior {
			return nil
		}

		if _, err := tx.ExecContext(ctx, `UPDATE reservas SET estado = ?, actualizada_en = ? WHERE id = ?`,
			res.Estado, res.ActualizadaEn, res.ID); err != nil {
			return err
		}
		for _, it := range res.Items {
			if err := moverStockDeReserva(ctx, tx, res, it); err != nil {
				return err
			}
		}
		if res.Estado == dominio.ReservaVencida {
			return guardarEvento(ctx, tx, "ReservaVencida", map[string]any{"reservaId": res.ID, "referencia": res.Referencia}, res.ActualizadaEn)
		}
		return nil
	})
	if err != nil {
		return dominio.Reserva{}, err
	}
	return resultado, errNegocio
}

// moverStockDeReserva aplica el efecto de la transición sobre una variante.
func moverStockDeReserva(ctx context.Context, tx *sql.Tx, res dominio.Reserva, it dominio.Item) error {
	var err error
	switch res.Estado {
	case dominio.ReservaConfirmada:
		// Se descuenta definitivamente: sale del online y deja de estar reservado.
		_, err = tx.ExecContext(ctx,
			`UPDATE stock SET online = online - ?, reservado = reservado - ?, actualizado_en = NOW() WHERE variante_id = ?`,
			it.Cantidad, it.Cantidad, it.VarianteID)
		if err == nil {
			err = registrarMovimiento(ctx, tx, it.VarianteID, "VENTA_ONLINE", dominio.CanalOnline, -it.Cantidad, res.ID, "")
		}
	case dominio.ReservaLiberada, dominio.ReservaVencida:
		// Las unidades nunca salieron del online: solo dejan de estar apartadas.
		_, err = tx.ExecContext(ctx,
			`UPDATE stock SET reservado = reservado - ?, actualizado_en = NOW() WHERE variante_id = ?`,
			it.Cantidad, it.VarianteID)
	default:
		err = fmt.Errorf("transición inesperada a %s", res.Estado)
	}
	return err
}

func (r *Repositorio) ReservasVencidas(ctx context.Context, ahora time.Time, limite int) ([]string, error) {
	filas, err := r.db.QueryContext(ctx,
		`SELECT id FROM reservas WHERE estado = 'ACTIVA' AND vence_en <= ? ORDER BY vence_en LIMIT ?`, ahora.UTC(), limite)
	if err != nil {
		return nil, err
	}
	defer filas.Close()
	var ids []string
	for filas.Next() {
		var id string
		if err := filas.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, filas.Err()
}

type consultor interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func (r *Repositorio) reservaPorClave(ctx context.Context, c consultor, clave string) (dominio.Reserva, error) {
	return r.reserva(ctx, c, `WHERE clave_idempotencia = ?`, clave)
}

func (r *Repositorio) reserva(ctx context.Context, c consultor, condicion string, arg any) (dominio.Reserva, error) {
	var res dominio.Reserva
	err := c.QueryRowContext(ctx,
		`SELECT id, clave_idempotencia, huella, referencia, estado, vence_en, creada_en, actualizada_en FROM reservas `+condicion, arg).
		Scan(&res.ID, &res.ClaveIdempotencia, &res.Huella, &res.Referencia, &res.Estado, &res.VenceEn, &res.CreadaEn, &res.ActualizadaEn)
	if errors.Is(err, sql.ErrNoRows) {
		return dominio.Reserva{}, dominio.ErrReservaNoEncontrada
	}
	if err != nil {
		return dominio.Reserva{}, err
	}

	filas, err := c.QueryContext(ctx, `SELECT variante_id, cantidad FROM reserva_items WHERE reserva_id = ? ORDER BY variante_id`, res.ID)
	if err != nil {
		return dominio.Reserva{}, err
	}
	defer filas.Close()
	for filas.Next() {
		var it dominio.Item
		if err := filas.Scan(&it.VarianteID, &it.Cantidad); err != nil {
			return dominio.Reserva{}, err
		}
		res.Items = append(res.Items, it)
	}
	return res, filas.Err()
}

// ---------- Stock ----------

func (r *Repositorio) ConsultarStock(ctx context.Context, ids []string) ([]dominio.Stock, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	filas, err := r.db.QueryContext(ctx,
		`SELECT variante_id, online, fisico, reservado, actualizado_en FROM stock
		 WHERE variante_id IN (?`+strings.Repeat(",?", len(ids)-1)+`) ORDER BY variante_id`, args...)
	if err != nil {
		return nil, err
	}
	defer filas.Close()
	var stocks []dominio.Stock
	for filas.Next() {
		var s dominio.Stock
		if err := filas.Scan(&s.VarianteID, &s.Online, &s.Fisico, &s.Reservado, &s.ActualizadoEn); err != nil {
			return nil, err
		}
		stocks = append(stocks, s)
	}
	return stocks, filas.Err()
}

// Ingresar suma unidades a un canal. Si la variante no existe, la da de alta.
func (r *Repositorio) Ingresar(ctx context.Context, i dominio.Ingreso) (dominio.Stock, error) {
	return r.operacionIdempotente(ctx, i.ClaveIdempotencia, "INGRESO", i.Huella(), i.VarianteID, func(tx *sql.Tx) error {
		columna := columnaDe(i.Canal)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO stock (variante_id, `+columna+`) VALUES (?, ?)
			 ON DUPLICATE KEY UPDATE `+columna+` = `+columna+` + VALUES(`+columna+`), actualizado_en = NOW()`,
			i.VarianteID, i.Cantidad); err != nil {
			return err
		}
		return registrarMovimiento(ctx, tx, i.VarianteID, "INGRESO", i.Canal, i.Cantidad, i.ClaveIdempotencia, i.Usuario)
	})
}

// Transferir mueve unidades entre canales con una sola actualización condicional: o se mueve todo
// o nada, y el origen nunca queda por debajo de lo disponible (RN-10).
func (r *Repositorio) Transferir(ctx context.Context, t dominio.Transferencia) (dominio.Stock, error) {
	return r.operacionIdempotente(ctx, t.ClaveIdempotencia, "TRANSFERENCIA", t.Huella(), t.VarianteID, func(tx *sql.Tx) error {
		var consulta string
		if t.Origen == dominio.CanalOnline {
			consulta = `UPDATE stock SET online = online - ?, fisico = fisico + ?, actualizado_en = NOW()
			            WHERE variante_id = ? AND online - reservado >= ?`
		} else {
			consulta = `UPDATE stock SET fisico = fisico - ?, online = online + ?, actualizado_en = NOW()
			            WHERE variante_id = ? AND fisico >= ?`
		}
		resultado, err := tx.ExecContext(ctx, consulta, t.Cantidad, t.Cantidad, t.VarianteID, t.Cantidad)
		if err != nil {
			return err
		}
		if n, _ := resultado.RowsAffected(); n == 0 {
			return faltanteDe(ctx, tx, t.VarianteID, t.Origen, t.Cantidad)
		}
		if err := registrarMovimiento(ctx, tx, t.VarianteID, "TRANSFERENCIA", t.Origen, -t.Cantidad, t.ClaveIdempotencia, t.Usuario); err != nil {
			return err
		}
		return registrarMovimiento(ctx, tx, t.VarianteID, "TRANSFERENCIA", t.Destino(), t.Cantidad, t.ClaveIdempotencia, t.Usuario)
	})
}

// operacionIdempotente registra la clave en la misma transacción que la operación.
// Un reintento con la misma clave no vuelve a aplicarla y devuelve el stock actual.
func (r *Repositorio) operacionIdempotente(ctx context.Context, clave, tipo, huella, varianteID string, aplicar func(*sql.Tx) error) (dominio.Stock, error) {
	err := r.enTransaccion(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO operaciones (clave_idempotencia, tipo, huella, variante_id) VALUES (?, ?, ?, ?)`,
			clave, tipo, huella, varianteID); err != nil {
			return err
		}
		return aplicar(tx)
	})
	if esClaveDuplicada(err) {
		var huellaGuardada string
		if err := r.db.QueryRowContext(ctx, `SELECT huella FROM operaciones WHERE clave_idempotencia = ?`, clave).Scan(&huellaGuardada); err != nil {
			return dominio.Stock{}, err
		}
		if huellaGuardada != huella {
			return dominio.Stock{}, dominio.ErrClaveReutilizada
		}
	} else if err != nil {
		return dominio.Stock{}, err
	}
	stocks, err := r.ConsultarStock(ctx, []string{varianteID})
	if err != nil {
		return dominio.Stock{}, err
	}
	if len(stocks) == 0 {
		return dominio.Stock{}, &dominio.ErrorVarianteInexistente{Variantes: []string{varianteID}}
	}
	return stocks[0], nil
}

// ---------- Reposiciones al gimnasio ----------

func (r *Repositorio) CrearReposicion(ctx context.Context, rep dominio.Reposicion) (dominio.Reposicion, bool, error) {
	err := r.enTransaccion(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO reposiciones (id, clave_idempotencia, variante_id, cantidad, estado, proximo_intento, usuario, creada_en, actualizada_en)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			rep.ID, rep.ClaveIdempotencia, rep.VarianteID, rep.Cantidad, rep.Estado, rep.ProximoIntento, rep.Usuario, rep.CreadaEn, rep.CreadaEn); err != nil {
			return err
		}
		resultado, err := tx.ExecContext(ctx,
			`UPDATE stock SET fisico = fisico - ?, actualizado_en = NOW() WHERE variante_id = ? AND fisico >= ?`,
			rep.Cantidad, rep.VarianteID, rep.Cantidad)
		if err != nil {
			return err
		}
		if n, _ := resultado.RowsAffected(); n == 0 {
			return faltanteDe(ctx, tx, rep.VarianteID, dominio.CanalFisico, rep.Cantidad)
		}
		return registrarMovimiento(ctx, tx, rep.VarianteID, "CONSIGNACION", dominio.CanalFisico, -rep.Cantidad, rep.ID, rep.Usuario)
	})
	if esClaveDuplicada(err) {
		existente, err := r.reposicion(ctx, r.db, `WHERE clave_idempotencia = ?`, rep.ClaveIdempotencia)
		return existente, true, err
	}
	return rep, false, err
}

func (r *Repositorio) TomarReposiciones(ctx context.Context, ahora time.Time, plazo time.Duration, limite int) ([]dominio.Reposicion, error) {
	var tomadas []dominio.Reposicion
	err := r.enTransaccion(ctx, func(tx *sql.Tx) error {
		// SKIP LOCKED: si otra réplica está tomando las mismas filas, las saltea en lugar de esperar.
		filas, err := tx.QueryContext(ctx,
			`SELECT `+columnasReposicion+` FROM reposiciones
			 WHERE estado = 'PENDIENTE' AND proximo_intento <= ?
			 ORDER BY proximo_intento LIMIT ? FOR UPDATE SKIP LOCKED`, ahora.UTC(), limite)
		if err != nil {
			return err
		}
		for filas.Next() {
			rep, err := escanearReposicion(filas)
			if err != nil {
				filas.Close()
				return err
			}
			tomadas = append(tomadas, rep)
		}
		filas.Close()
		// Mientras se envían, nadie más las toma. Si este proceso se cae, quedan libres al vencer el plazo.
		for _, rep := range tomadas {
			if _, err := tx.ExecContext(ctx, `UPDATE reposiciones SET proximo_intento = ? WHERE id = ?`, ahora.Add(plazo).UTC(), rep.ID); err != nil {
				return err
			}
		}
		return nil
	})
	return tomadas, err
}

func (r *Repositorio) MarcarReposicionEnviada(ctx context.Context, id string, ahora time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE reposiciones SET estado = 'ENVIADA', intentos = intentos + 1, ultimo_error = '', actualizada_en = ?
		 WHERE id = ? AND estado = 'PENDIENTE'`, ahora.UTC(), id)
	return err
}

func (r *Repositorio) RechazarReposicion(ctx context.Context, id, motivo string, ahora time.Time) error {
	return r.enTransaccion(ctx, func(tx *sql.Tx) error {
		rep, err := r.reposicion(ctx, tx, `WHERE id = ? FOR UPDATE`, id)
		if err != nil || rep.Estado != dominio.ReposicionPendiente {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE reposiciones SET estado = 'RECHAZADA', intentos = intentos + 1, ultimo_error = ?, actualizada_en = ? WHERE id = ?`,
			truncar(motivo, 500), ahora.UTC(), id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE stock SET fisico = fisico + ?, actualizado_en = NOW() WHERE variante_id = ?`,
			rep.Cantidad, rep.VarianteID); err != nil {
			return err
		}
		return registrarMovimiento(ctx, tx, rep.VarianteID, "CONSIGNACION_RECHAZADA", dominio.CanalFisico, rep.Cantidad, rep.ID, "")
	})
}

func (r *Repositorio) ReprogramarReposicion(ctx context.Context, id, motivo string, proximo time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE reposiciones SET intentos = intentos + 1, ultimo_error = ?, proximo_intento = ?, actualizada_en = NOW()
		 WHERE id = ? AND estado = 'PENDIENTE'`, truncar(motivo, 500), proximo.UTC(), id)
	return err
}

const columnasReposicion = `id, clave_idempotencia, variante_id, cantidad, estado, intentos, ultimo_error, proximo_intento, usuario, creada_en`

type escaneable interface{ Scan(dest ...any) error }

func escanearReposicion(f escaneable) (dominio.Reposicion, error) {
	var rep dominio.Reposicion
	err := f.Scan(&rep.ID, &rep.ClaveIdempotencia, &rep.VarianteID, &rep.Cantidad, &rep.Estado,
		&rep.Intentos, &rep.UltimoError, &rep.ProximoIntento, &rep.Usuario, &rep.CreadaEn)
	return rep, err
}

func (r *Repositorio) reposicion(ctx context.Context, c consultor, condicion string, arg any) (dominio.Reposicion, error) {
	rep, err := escanearReposicion(c.QueryRowContext(ctx, `SELECT `+columnasReposicion+` FROM reposiciones `+condicion, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return dominio.Reposicion{}, fmt.Errorf("reposición no encontrada: %w", err)
	}
	return rep, err
}

// ---------- Auxiliares ----------

func (r *Repositorio) enTransaccion(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func disponibleOnline(ctx context.Context, tx *sql.Tx, varianteID string) (int, bool, error) {
	var disponible int
	err := tx.QueryRowContext(ctx, `SELECT online - reservado FROM stock WHERE variante_id = ?`, varianteID).Scan(&disponible)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return disponible, err == nil, err
}

// faltanteDe arma el error de negocio cuando una actualización condicional no afectó ninguna fila.
func faltanteDe(ctx context.Context, tx *sql.Tx, varianteID string, canal dominio.Canal, solicitada int) error {
	consulta := `SELECT online - reservado FROM stock WHERE variante_id = ?`
	if canal == dominio.CanalFisico {
		consulta = `SELECT fisico FROM stock WHERE variante_id = ?`
	}
	var disponible int
	err := tx.QueryRowContext(ctx, consulta, varianteID).Scan(&disponible)
	if errors.Is(err, sql.ErrNoRows) {
		return &dominio.ErrorVarianteInexistente{Variantes: []string{varianteID}}
	}
	if err != nil {
		return err
	}
	return &dominio.ErrorStockInsuficiente{Faltantes: []dominio.Faltante{{VarianteID: varianteID, Solicitada: solicitada, Disponible: max(disponible, 0)}}}
}

func registrarMovimiento(ctx context.Context, tx *sql.Tx, varianteID, tipo string, canal dominio.Canal, cantidad int, referencia, usuario string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO movimientos (variante_id, tipo, canal, cantidad, referencia, usuario) VALUES (?, ?, ?, ?, ?, ?)`,
		varianteID, tipo, canal, cantidad, referencia, truncar(usuario, 100))
	return err
}

func guardarEvento(ctx context.Context, tx *sql.Tx, tipo string, datos any, ocurridoEn time.Time) error {
	contenido, err := json.Marshal(datos)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		`INSERT INTO outbox (id, tipo, datos, correlation_id, ocurrido_en) VALUES (?, ?, ?, ?, ?)`,
		uuid.NewString(), tipo, contenido, observabilidad.CorrelationID(ctx), ocurridoEn)
	return err
}

func columnaDe(c dominio.Canal) string {
	if c == dominio.CanalOnline {
		return "online"
	}
	return "fisico"
}

func esClaveDuplicada(err error) bool {
	var e *mysqldriver.MySQLError
	return errors.As(err, &e) && e.Number == codigoClaveDuplicada
}

func truncar(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
