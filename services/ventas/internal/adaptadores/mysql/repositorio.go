// Package mysql implementa el puerto Repositorio sobre MySQL. Cada transición bloquea la fila de la
// venta y guarda en la misma transacción las tareas y los mails que genera.
package mysql

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"

	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/dominio"
	"github.com/JuanHernandez-99/Fav-del-universo/services/ventas/internal/puertos"
)

//go:embed sql/*.sql
var scripts embed.FS

const codigoClaveDuplicada = 1062

type Repositorio struct {
	db *sql.DB
}

func NuevoRepositorio(db *sql.DB) *Repositorio { return &Repositorio{db: db} }

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

// ---------- Ventas ----------

func (r *Repositorio) CrearVenta(ctx context.Context, v dominio.Venta, tareas []dominio.TipoTarea, mails []dominio.Mail) (dominio.Venta, bool, error) {
	err := r.enTransaccion(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO ventas (id, clave_idempotencia, huella, canal, estado, medio, comprador_id, email, referencia_externa,
			                     reserva_id, total, url_pago, vence_en, creada_en, actualizada_en)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			v.ID, v.Clave, v.Huella, v.Canal, v.Estado, v.Medio, v.CompradorID, v.Email, v.ReferenciaExterna,
			v.ReservaID, v.Total, v.UrlPago, v.VenceEn, v.CreadaEn, v.ActualizadaEn); err != nil {
			return err
		}
		for _, it := range v.Items {
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO venta_items (venta_id, variante_id, nombre, talle, color, cantidad, precio) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				v.ID, it.VarianteID, it.Nombre, it.Talle, it.Color, it.Cantidad, it.Precio); err != nil {
				return err
			}
		}
		return guardarEfectos(ctx, tx, v.ID, tareas, mails)
	})
	if esClaveDuplicada(err) {
		existente, errLeer := r.VentaPorClave(ctx, v.Clave)
		return existente, true, errLeer
	}
	if err != nil {
		return dominio.Venta{}, false, err
	}
	return v, false, nil
}

func (r *Repositorio) VentaPorClave(ctx context.Context, clave string) (dominio.Venta, error) {
	return r.venta(ctx, r.db, `WHERE clave_idempotencia = ?`, clave)
}

func (r *Repositorio) ObtenerVenta(ctx context.Context, id string) (dominio.Venta, error) {
	return r.venta(ctx, r.db, `WHERE id = ?`, id)
}

func (r *Repositorio) GuardarUrlPago(ctx context.Context, id, url string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE ventas SET url_pago = ? WHERE id = ?`, url, id)
	return err
}

func (r *Repositorio) ModificarVenta(ctx context.Context, id string, cambio func(*dominio.Venta) ([]dominio.TipoTarea, []dominio.Mail, error)) (dominio.Venta, error) {
	var resultado dominio.Venta
	err := r.enTransaccion(ctx, func(tx *sql.Tx) error {
		v, err := aplicarCambio(ctx, r, tx, id, cambio)
		resultado = v
		return err
	})
	return resultado, err
}

func (r *Repositorio) RegistrarAvisoPago(ctx context.Context, pagoID, ventaID string, cambio func(*dominio.Venta) ([]dominio.TipoTarea, []dominio.Mail, error)) error {
	err := r.enTransaccion(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO pagos (pago_id, venta_id) VALUES (?, ?)`, pagoID, ventaID); err != nil {
			return err
		}
		_, err := aplicarCambio(ctx, r, tx, ventaID, cambio)
		return err
	})
	if esClaveDuplicada(err) {
		return nil // aviso repetido
	}
	return err
}

func (r *Repositorio) ProcesarEvento(ctx context.Context, mensajeID, ventaID string, cambio func(*dominio.Venta) ([]dominio.Mail, error)) (bool, error) {
	err := r.enTransaccion(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO mensajes_procesados (mensaje_id) VALUES (?)`, mensajeID); err != nil {
			return err
		}
		_, err := aplicarCambio(ctx, r, tx, ventaID, func(v *dominio.Venta) ([]dominio.TipoTarea, []dominio.Mail, error) {
			mails, err := cambio(v)
			return nil, mails, err
		})
		if errors.Is(err, dominio.ErrVentaNoEncontrada) {
			return nil // el mensaje queda procesado: reintentarlo no va a crear la venta
		}
		return err
	})
	if esClaveDuplicada(err) {
		return false, nil
	}
	return err == nil, err
}

// aplicarCambio bloquea la venta, aplica el cambio y guarda el resultado con sus tareas y mails.
func aplicarCambio(ctx context.Context, r *Repositorio, tx *sql.Tx, id string, cambio func(*dominio.Venta) ([]dominio.TipoTarea, []dominio.Mail, error)) (dominio.Venta, error) {
	v, err := r.venta(ctx, tx, `WHERE id = ? FOR UPDATE`, id)
	if err != nil {
		return dominio.Venta{}, err
	}
	tareas, mails, err := cambio(&v)
	if err != nil {
		return dominio.Venta{}, err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE ventas SET estado = ?, pago_aprobado_en = ?, actualizada_en = ? WHERE id = ?`,
		v.Estado, v.PagoAprobadoEn, v.ActualizadaEn, v.ID); err != nil {
		return dominio.Venta{}, err
	}
	return v, guardarEfectos(ctx, tx, v.ID, tareas, mails)
}

func guardarEfectos(ctx context.Context, tx *sql.Tx, ventaID string, tareas []dominio.TipoTarea, mails []dominio.Mail) error {
	// Sin fracción de segundo: DATETIME la redondea hacia arriba y dejaría la tarea en el futuro.
	ahora := time.Now().UTC().Truncate(time.Second)
	for _, t := range tareas {
		if _, err := tx.ExecContext(ctx, `INSERT INTO tareas (id, venta_id, tipo, proximo_intento) VALUES (?, ?, ?, ?)`,
			uuid.NewString(), ventaID, t, ahora); err != nil {
			return err
		}
	}
	for _, m := range mails {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO mails (id, venta_id, tipo, destinatario, asunto, cuerpo, proximo_intento) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			uuid.NewString(), ventaID, m.Tipo, m.Destinatario, m.Asunto, m.Cuerpo, ahora); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repositorio) ListarVentas(ctx context.Context, f puertos.FiltroVentas) ([]dominio.Venta, error) {
	condicion, args := `WHERE 1 = 1`, []any{}
	if f.Estado != "" {
		condicion += ` AND estado = ?`
		args = append(args, f.Estado)
	}
	if f.CompradorID != "" {
		condicion += ` AND comprador_id = ?`
		args = append(args, f.CompradorID)
	}
	args = append(args, f.Limite, f.Desde)
	filas, err := r.db.QueryContext(ctx, `SELECT `+columnasVenta+` FROM ventas `+condicion+` ORDER BY creada_en DESC, id LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	var ventas []dominio.Venta
	for filas.Next() {
		v, err := escanearVenta(filas)
		if err != nil {
			filas.Close()
			return nil, err
		}
		ventas = append(ventas, v)
	}
	filas.Close()
	if err := filas.Err(); err != nil {
		return nil, err
	}
	for i := range ventas {
		if ventas[i].Items, err = itemsDe(ctx, r.db, ventas[i].ID); err != nil {
			return nil, err
		}
	}
	return ventas, nil
}

const columnasVenta = `id, clave_idempotencia, huella, canal, estado, medio, comprador_id, email, referencia_externa,
	reserva_id, total, url_pago, pago_aprobado_en, vence_en, creada_en, actualizada_en`

type escaneable interface{ Scan(dest ...any) error }

func escanearVenta(f escaneable) (dominio.Venta, error) {
	var v dominio.Venta
	var pago, vence sql.NullTime
	err := f.Scan(&v.ID, &v.Clave, &v.Huella, &v.Canal, &v.Estado, &v.Medio, &v.CompradorID, &v.Email, &v.ReferenciaExterna,
		&v.ReservaID, &v.Total, &v.UrlPago, &pago, &vence, &v.CreadaEn, &v.ActualizadaEn)
	if pago.Valid {
		v.PagoAprobadoEn = &pago.Time
	}
	if vence.Valid {
		v.VenceEn = &vence.Time
	}
	return v, err
}

type consultor interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func (r *Repositorio) venta(ctx context.Context, c consultor, condicion string, arg any) (dominio.Venta, error) {
	v, err := escanearVenta(c.QueryRowContext(ctx, `SELECT `+columnasVenta+` FROM ventas `+condicion, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return dominio.Venta{}, dominio.ErrVentaNoEncontrada
	}
	if err != nil {
		return dominio.Venta{}, err
	}
	v.Items, err = itemsDe(ctx, c, v.ID)
	return v, err
}

func itemsDe(ctx context.Context, c consultor, ventaID string) ([]dominio.Item, error) {
	filas, err := c.QueryContext(ctx,
		`SELECT variante_id, nombre, talle, color, cantidad, precio FROM venta_items WHERE venta_id = ? ORDER BY variante_id`, ventaID)
	if err != nil {
		return nil, err
	}
	defer filas.Close()
	var items []dominio.Item
	for filas.Next() {
		var it dominio.Item
		if err := filas.Scan(&it.VarianteID, &it.Nombre, &it.Talle, &it.Color, &it.Cantidad, &it.Precio); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, filas.Err()
}

// ---------- Tareas ----------

// TomarTareas usa SKIP LOCKED: dos réplicas no toman la misma. Quedan apartadas por el plazo; si este
// proceso se cae, otra las toma al vencer.
func (r *Repositorio) TomarTareas(ctx context.Context, ahora time.Time, plazo time.Duration, limite int) ([]dominio.Tarea, error) {
	var tomadas []dominio.Tarea
	err := r.enTransaccion(ctx, func(tx *sql.Tx) error {
		filas, err := tx.QueryContext(ctx,
			`SELECT id, venta_id, tipo, intentos FROM tareas
			 WHERE estado = 'PENDIENTE' AND proximo_intento <= ?
			 ORDER BY proximo_intento LIMIT ? FOR UPDATE SKIP LOCKED`, ahora.UTC(), limite)
		if err != nil {
			return err
		}
		for filas.Next() {
			var t dominio.Tarea
			if err := filas.Scan(&t.ID, &t.VentaID, &t.Tipo, &t.Intentos); err != nil {
				filas.Close()
				return err
			}
			tomadas = append(tomadas, t)
		}
		filas.Close()
		if err := filas.Err(); err != nil {
			return err
		}
		for _, t := range tomadas {
			if _, err := tx.ExecContext(ctx, `UPDATE tareas SET proximo_intento = ? WHERE id = ?`, ahora.Add(plazo).UTC(), t.ID); err != nil {
				return err
			}
		}
		return nil
	})
	return tomadas, err
}

func (r *Repositorio) CompletarTarea(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE tareas SET estado = 'COMPLETADA', intentos = intentos + 1, ultimo_error = '' WHERE id = ?`, id)
	return err
}

func (r *Repositorio) ReprogramarTarea(ctx context.Context, id, motivo string, proximo time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE tareas SET intentos = intentos + 1, ultimo_error = ?, proximo_intento = ? WHERE id = ? AND estado = 'PENDIENTE'`,
		truncar(motivo, 500), proximo.UTC(), id)
	return err
}

// ---------- Mails ----------

func (r *Repositorio) TomarMails(ctx context.Context, ahora time.Time, plazo time.Duration, limite int) ([]dominio.Mail, error) {
	var tomados []dominio.Mail
	err := r.enTransaccion(ctx, func(tx *sql.Tx) error {
		filas, err := tx.QueryContext(ctx,
			`SELECT id, venta_id, tipo, destinatario, asunto, cuerpo, intentos FROM mails
			 WHERE estado = 'PENDIENTE' AND proximo_intento <= ?
			 ORDER BY proximo_intento LIMIT ? FOR UPDATE SKIP LOCKED`, ahora.UTC(), limite)
		if err != nil {
			return err
		}
		for filas.Next() {
			var m dominio.Mail
			if err := filas.Scan(&m.ID, &m.VentaID, &m.Tipo, &m.Destinatario, &m.Asunto, &m.Cuerpo, &m.Intentos); err != nil {
				filas.Close()
				return err
			}
			tomados = append(tomados, m)
		}
		filas.Close()
		if err := filas.Err(); err != nil {
			return err
		}
		for _, m := range tomados {
			if _, err := tx.ExecContext(ctx, `UPDATE mails SET proximo_intento = ? WHERE id = ?`, ahora.Add(plazo).UTC(), m.ID); err != nil {
				return err
			}
		}
		return nil
	})
	return tomados, err
}

func (r *Repositorio) MarcarMailEnviado(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE mails SET estado = 'ENVIADO', intentos = intentos + 1, ultimo_error = '' WHERE id = ?`, id)
	return err
}

func (r *Repositorio) ReprogramarMail(ctx context.Context, id, motivo string, proximo time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE mails SET intentos = intentos + 1, ultimo_error = ?, proximo_intento = ? WHERE id = ?`,
		truncar(motivo, 500), proximo.UTC(), id)
	return err
}

func (r *Repositorio) MarcarMailFallido(ctx context.Context, id, motivo string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE mails SET estado = 'FALLIDO', intentos = intentos + 1, ultimo_error = ? WHERE id = ?`, truncar(motivo, 500), id)
	return err
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
