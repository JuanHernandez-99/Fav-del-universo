// Package mysql implementa puertos.RepositorioInventario sobre MySQL.
//
// Las garantías de no vender de más y no duplicar se resuelven acá, con
// transacciones y actualizaciones condicionales: es la base la que ve todas
// las operaciones concurrentes (ADR-003, ADR-004).
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	drv "github.com/go-sql-driver/mysql"

	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/dominio"
	"github.com/JuanHernandez-99/Fav-del-universo/services/inventario/internal/puertos"
)

const errClaveDuplicada = 1062 // ER_DUP_ENTRY

// Repositorio implementa puertos.RepositorioInventario.
type Repositorio struct {
	db *sql.DB
}

var (
	_ puertos.RepositorioInventario = (*Repositorio)(nil)
	_ puertos.Outbox                = (*Repositorio)(nil)
)

func NuevoRepositorio(db *sql.DB) *Repositorio { return &Repositorio{db: db} }

func (r *Repositorio) Disponible(ctx context.Context) error { return r.db.PingContext(ctx) }

// GuardarReservaSiAlcanza aparta el stock y guarda la reserva en una sola transacción.
func (r *Repositorio) GuardarReservaSiAlcanza(ctx context.Context, res dominio.Reserva, clave, huella string) (dominio.Reserva, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return dominio.Reserva{}, err
	}
	defer tx.Rollback() //nolint:errcheck // sin efecto si ya se confirmó

	// 1. Registrar la clave primero. Si otro pedido con la misma clave está en curso,
	//    la base hace esperar a este hasta que el otro termine.
	_, err = tx.ExecContext(ctx,
		`INSERT INTO claves_idempotencia (clave, huella, reserva_id, creada_en) VALUES (?, ?, ?, ?)`,
		clave, huella, res.ID, res.CreadaEn)
	if esClaveDuplicada(err) {
		tx.Rollback() //nolint:errcheck
		return r.reservaDeClave(ctx, clave, huella)
	}
	if err != nil {
		return dominio.Reserva{}, err
	}

	// 2. Apartar cada variante solo si alcanza. Los ítems vienen ordenados por variante,
	//    así dos reservas concurrentes bloquean las filas en el mismo orden.
	var faltantes []dominio.Faltante
	var inexistentes []string
	for _, it := range res.Items {
		resultado, err := tx.ExecContext(ctx,
			`UPDATE stock SET reservado = reservado + ?
			 WHERE variante_id = ? AND canal = 'ONLINE' AND cantidad - reservado >= ?`,
			it.Cantidad, it.VarianteID, it.Cantidad)
		if err != nil {
			return dominio.Reserva{}, err
		}
		if n, _ := resultado.RowsAffected(); n == 1 {
			continue
		}
		var existe int
		err = tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM stock WHERE variante_id = ? AND canal = 'ONLINE'`, it.VarianteID).Scan(&existe)
		if err != nil {
			return dominio.Reserva{}, err
		}
		if existe == 0 {
			inexistentes = append(inexistentes, it.VarianteID)
		} else {
			faltantes = append(faltantes, dominio.Faltante{VarianteID: it.VarianteID, Solicitada: it.Cantidad})
		}
	}
	// 3. Todo o nada: si algo no alcanza, la transacción se deshace entera
	//    (incluida la clave, así un reintento vuelve a intentarlo).
	if len(inexistentes) > 0 {
		return dominio.Reserva{}, &dominio.ErrVarianteInexistente{IDs: inexistentes}
	}
	if len(faltantes) > 0 {
		return dominio.Reserva{}, &dominio.ErrStockInsuficiente{Faltantes: faltantes}
	}

	// 4. Guardar la reserva y sus ítems.
	_, err = tx.ExecContext(ctx,
		`INSERT INTO reservas (id, estado, referencia, correlation_id, creada_en, vence_en, actualizada_en)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		res.ID, string(res.Estado), res.Referencia, res.CorrelacionID, res.CreadaEn, res.VenceEn, res.CreadaEn)
	if err != nil {
		return dominio.Reserva{}, err
	}
	for _, it := range res.Items {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO reserva_items (reserva_id, variante_id, cantidad) VALUES (?, ?, ?)`,
			res.ID, it.VarianteID, it.Cantidad)
		if err != nil {
			return dominio.Reserva{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return dominio.Reserva{}, err
	}
	return res, nil
}

// reservaDeClave resuelve un reintento: misma clave y mismo pedido devuelve la
// reserva original; misma clave con otro pedido es un error.
func (r *Repositorio) reservaDeClave(ctx context.Context, clave, huella string) (dominio.Reserva, error) {
	var huellaGuardada, reservaID string
	err := r.db.QueryRowContext(ctx,
		`SELECT huella, reserva_id FROM claves_idempotencia WHERE clave = ?`, clave).Scan(&huellaGuardada, &reservaID)
	if err != nil {
		return dominio.Reserva{}, err
	}
	if huellaGuardada != huella {
		return dominio.Reserva{}, dominio.ErrClaveReutilizada
	}
	return r.ObtenerReserva(ctx, reservaID)
}

func (r *Repositorio) ObtenerReserva(ctx context.Context, id string) (dominio.Reserva, error) {
	var res dominio.Reserva
	var estado string
	err := r.db.QueryRowContext(ctx,
		`SELECT id, estado, referencia, correlation_id, creada_en, vence_en FROM reservas WHERE id = ?`, id).
		Scan(&res.ID, &estado, &res.Referencia, &res.CorrelacionID, &res.CreadaEn, &res.VenceEn)
	if errors.Is(err, sql.ErrNoRows) {
		return dominio.Reserva{}, dominio.ErrReservaInexistente
	}
	if err != nil {
		return dominio.Reserva{}, err
	}
	res.Estado = dominio.EstadoReserva(estado)
	filas, err := r.db.QueryContext(ctx,
		`SELECT variante_id, cantidad FROM reserva_items WHERE reserva_id = ? ORDER BY variante_id`, id)
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

// ConfirmarSiVigente: el cambio de estado es condicional, así la carrera entre la
// confirmación y el vencimiento la gana una sola de las dos operaciones.
func (r *Repositorio) ConfirmarSiVigente(ctx context.Context, id string, ahora time.Time) error {
	return r.enTransaccion(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE reservas SET estado = 'CONFIRMADA', actualizada_en = ?
			 WHERE id = ? AND estado = 'ACTIVA' AND vence_en > ?`, ahora, id, ahora)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return puertos.ErrEstadoCambio
		}
		// Descuento definitivo: las unidades dejan de estar reservadas y salen del stock.
		_, err = tx.ExecContext(ctx,
			`UPDATE stock s JOIN reserva_items i ON i.variante_id = s.variante_id
			 SET s.cantidad = s.cantidad - i.cantidad, s.reservado = s.reservado - i.cantidad
			 WHERE i.reserva_id = ? AND s.canal = 'ONLINE'`, id)
		return err
	})
}

func (r *Repositorio) LiberarSiActiva(ctx context.Context, id string) error {
	return r.enTransaccion(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE reservas SET estado = 'LIBERADA', actualizada_en = UTC_TIMESTAMP(3)
			 WHERE id = ? AND estado = 'ACTIVA'`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return puertos.ErrEstadoCambio
		}
		return devolverUnidades(ctx, tx, id)
	})
}

func (r *Repositorio) VencerSiCorresponde(ctx context.Context, id string, ahora time.Time, evento dominio.Evento) (bool, error) {
	cuerpo, err := serializar(evento)
	if err != nil {
		return false, err
	}
	vencio := false
	err = r.enTransaccion(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`UPDATE reservas SET estado = 'VENCIDA', actualizada_en = ?
			 WHERE id = ? AND estado = 'ACTIVA' AND vence_en <= ?`, ahora, id, ahora)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return nil // otra operación la confirmó o liberó antes
		}
		if err := devolverUnidades(ctx, tx, id); err != nil {
			return err
		}
		// El evento se guarda en la misma transacción que el cambio (outbox, ADR-005).
		_, err = tx.ExecContext(ctx,
			`INSERT INTO outbox (id, tipo, correlation_id, payload, creado_en) VALUES (?, ?, ?, ?, ?)`,
			evento.ID, evento.Tipo, evento.CorrelacionID, string(cuerpo), evento.OcurridoEn)
		if err != nil {
			return err
		}
		vencio = true
		return nil
	})
	return vencio, err
}

func (r *Repositorio) Pendientes(ctx context.Context, limite int) ([]puertos.MensajeOutbox, error) {
	filas, err := r.db.QueryContext(ctx,
		`SELECT id, tipo, correlation_id, payload FROM outbox
		 WHERE publicado_en IS NULL ORDER BY creado_en, id LIMIT ?`, limite)
	if err != nil {
		return nil, err
	}
	defer filas.Close()
	var mensajes []puertos.MensajeOutbox
	for filas.Next() {
		var m puertos.MensajeOutbox
		if err := filas.Scan(&m.ID, &m.Tipo, &m.CorrelacionID, &m.Cuerpo); err != nil {
			return nil, err
		}
		mensajes = append(mensajes, m)
	}
	return mensajes, filas.Err()
}

func (r *Repositorio) MarcarPublicado(ctx context.Context, id string, ahora time.Time) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE outbox SET publicado_en = ? WHERE id = ? AND publicado_en IS NULL`, ahora, id)
	return err
}

func (r *Repositorio) ReservasParaVencer(ctx context.Context, ahora time.Time, limite int) ([]string, error) {
	filas, err := r.db.QueryContext(ctx,
		`SELECT id FROM reservas WHERE estado = 'ACTIVA' AND vence_en <= ? ORDER BY vence_en LIMIT ?`, ahora, limite)
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

func (r *Repositorio) ConsultarStock(ctx context.Context, varianteIDs []string) ([]dominio.Stock, error) {
	marcas := strings.TrimSuffix(strings.Repeat("?,", len(varianteIDs)), ",")
	args := make([]any, len(varianteIDs))
	for i, id := range varianteIDs {
		args[i] = id
	}
	filas, err := r.db.QueryContext(ctx,
		fmt.Sprintf(`SELECT variante_id, canal, cantidad, reservado FROM stock
		             WHERE variante_id IN (%s) ORDER BY variante_id, canal`, marcas), args...)
	if err != nil {
		return nil, err
	}
	defer filas.Close()
	var stock []dominio.Stock
	for filas.Next() {
		var s dominio.Stock
		var canal string
		if err := filas.Scan(&s.VarianteID, &canal, &s.Cantidad, &s.Reservado); err != nil {
			return nil, err
		}
		s.Canal = dominio.Canal(canal)
		stock = append(stock, s)
	}
	return stock, filas.Err()
}

// devolverUnidades libera lo apartado por una reserva.
func devolverUnidades(ctx context.Context, tx *sql.Tx, reservaID string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE stock s JOIN reserva_items i ON i.variante_id = s.variante_id
		 SET s.reservado = s.reservado - i.cantidad
		 WHERE i.reserva_id = ? AND s.canal = 'ONLINE'`, reservaID)
	return err
}

func (r *Repositorio) enTransaccion(ctx context.Context, f func(*sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := f(tx); err != nil {
		tx.Rollback() //nolint:errcheck
		return err
	}
	return tx.Commit()
}

func esClaveDuplicada(err error) bool {
	var me *drv.MySQLError
	return errors.As(err, &me) && me.Number == errClaveDuplicada
}

// Config son los datos de conexión a la base.
type Config struct {
	Host, Puerto, Usuario, Clave, Base string
}

// Abrir conecta a MySQL. Las fechas se guardan y se leen en UTC.
func Abrir(c Config) (*sql.DB, error) {
	cfg := drv.NewConfig()
	cfg.Net = "tcp"
	cfg.Addr = c.Host + ":" + c.Puerto
	cfg.User = c.Usuario
	cfg.Passwd = c.Clave
	cfg.DBName = c.Base
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.Params = map[string]string{"time_zone": "'+00:00'"}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)
	return db, nil
}
