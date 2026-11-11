// Package repository es el único lugar que habla con MongoDB (sección 13): el resto del servicio
// no importa el driver.
package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"

	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/modelo"
	"github.com/JuanHernandez-99/Fav-del-universo/services/catalogo/internal/observabilidad"
)

type Repositorio struct {
	cliente     *mongo.Client
	productos   *mongo.Collection
	categorias  *mongo.Collection
	promociones *mongo.Collection
}

// indiceVariantes tiene que coincidir en nombre y definición con el de
// db/scripts/01_crear_colecciones_e_indices.js; si difiere, Mongo rechaza la creación.
const indiceVariantes = "variantes_id_unico"

// Conectar arma el cliente. No abre la conexión todavía: el driver conecta recién en la primera
// operación, por eso quien llama debe usar Ping para saber si la base responde.
func Conectar(uri, base string) (*Repositorio, error) {
	cliente, err := mongo.Connect(options.Client().
		ApplyURI(uri).
		SetServerSelectionTimeout(3 * time.Second))
	if err != nil {
		return nil, err
	}
	db := cliente.Database(base)
	return &Repositorio{
		cliente:     cliente,
		productos:   db.Collection("productos"),
		categorias:  db.Collection("categorias"),
		promociones: db.Collection("promociones"),
	}, nil
}

// AsegurarIndices crea al arrancar el único índice del que depende una regla: que un id de
// variante no se repita en todo el catálogo. Los demás son de rendimiento y los crea el script 01.
// Si el índice ya existe igual, Mongo no hace nada, así que dos réplicas pueden arrancar a la vez.
func (r *Repositorio) AsegurarIndices(ctx context.Context) error {
	_, err := r.productos.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "variantes.id", Value: 1}},
		Options: options.Index().SetName(indiceVariantes).SetUnique(true),
	})
	return err
}

// Ping verifica que la base responda. Lo usan el arranque y la sonda /readyz.
func (r *Repositorio) Ping(ctx context.Context) error {
	return r.cliente.Ping(ctx, readpref.Primary())
}

// Cerrar libera las conexiones del pool al apagar el servicio.
func (r *Repositorio) Cerrar(ctx context.Context) error {
	return r.cliente.Disconnect(ctx)
}

// ObtenerProducto busca por _id, que tiene índice propio. Devuelve modelo.ErrProductoNoEncontrado
// en lugar del error del driver, para que las otras capas no dependan de Mongo.
func (r *Repositorio) ObtenerProducto(ctx context.Context, id string) (modelo.Producto, error) {
	var p modelo.Producto
	err := r.productos.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&p)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return modelo.Producto{}, modelo.ErrProductoNoEncontrado
	}
	return p, err
}

func (r *Repositorio) ExisteCategoria(ctx context.Context, id string) (bool, error) {
	n, err := r.categorias.CountDocuments(ctx, bson.D{{Key: "_id", Value: id}}, options.Count().SetLimit(1))
	return n > 0, err
}

// AgregarVariante es la operación atómica de DISENO.md 5.9. La condición "no tiene esa
// variante" va dentro del filtro de un solo updateOne: MongoDB evalúa el filtro y aplica el cambio
// sobre el documento sin que otro pedido se meta en el medio. Si 20 pedidos llegan juntos, solo el
// primero encuentra el documento sin la variante; para los demás el filtro ya no coincide.
//
// Se compara por talle y color, y además por id: "Blanco" y "blanco" son colores distintos para
// $elemMatch pero dan el mismo id, y el índice único no evita repetidos dentro del mismo arreglo.
func (r *Repositorio) AgregarVariante(ctx context.Context, productoID string, v modelo.Variante, ahora time.Time) error {
	filtro := bson.D{
		{Key: "_id", Value: productoID},
		{Key: "variantes", Value: bson.D{{Key: "$not", Value: bson.D{{Key: "$elemMatch", Value: bson.D{
			{Key: "talle", Value: v.Talle}, {Key: "color", Value: v.Color},
		}}}}}},
		{Key: "variantes.id", Value: bson.D{{Key: "$ne", Value: v.ID}}},
	}
	cambio := bson.D{
		{Key: "$push", Value: bson.D{{Key: "variantes", Value: v}}},
		{Key: "$set", Value: bson.D{{Key: "actualizadoEn", Value: ahora}, correlacionDelCambio(ctx)}},
		{Key: "$inc", Value: bson.D{{Key: "version", Value: 1}}}, // ADR-006: todo cambio sube la versión
	}
	res, err := r.productos.UpdateOne(ctx, filtro, cambio)
	if mongo.IsDuplicateKeyError(err) { // el id ya lo usa otro producto
		return modelo.ErrVarianteDuplicada
	}
	if err != nil {
		return err
	}
	if res.MatchedCount == 1 {
		return nil
	}
	// No coincidió: o el producto no existe, o ya tenía la variante. Recién acá se distingue cuál.
	if existe, err := r.existeProducto(ctx, productoID); err != nil {
		return err
	} else if !existe {
		return modelo.ErrProductoNoEncontrado
	}
	return modelo.ErrVarianteDuplicada
}

// ListarProductos arma el filtro con los parámetros que vinieron y lo ejecuta con orden y
// paginación. Devuelve la página pedida y el total de productos que cumplen el filtro.
//
// El orden siempre se desempata por _id: sin eso, productos con el mismo precio podrían salir en
// distinto orden entre una página y la siguiente, y repetirse o saltearse.
func (r *Repositorio) ListarProductos(ctx context.Context, f modelo.FiltroProductos) ([]modelo.Producto, int64, error) {
	filtro := bson.D{}
	if f.Categoria != "" {
		filtro = append(filtro, bson.E{Key: "categoria", Value: f.Categoria})
	}
	if f.Publicado != nil {
		filtro = append(filtro, bson.E{Key: "publicado", Value: *f.Publicado})
	}
	precio := bson.D{}
	if f.PrecioMin != nil {
		precio = append(precio, bson.E{Key: "$gte", Value: *f.PrecioMin})
	}
	if f.PrecioMax != nil {
		precio = append(precio, bson.E{Key: "$lte", Value: *f.PrecioMax})
	}
	if len(precio) > 0 {
		filtro = append(filtro, bson.E{Key: "precioBase.monto", Value: precio})
	}
	// Con talle y color juntos tienen que ser la misma variante: eso lo garantiza $elemMatch. Por
	// separado, "variantes.talle" y "variantes.color" podrían coincidir en variantes distintas.
	switch {
	case f.Talle != "" && f.Color != "":
		filtro = append(filtro, bson.E{Key: "variantes", Value: bson.D{{Key: "$elemMatch", Value: bson.D{
			{Key: "talle", Value: f.Talle}, {Key: "color", Value: f.Color},
		}}}})
	case f.Talle != "":
		filtro = append(filtro, bson.E{Key: "variantes.talle", Value: f.Talle})
	case f.Color != "":
		filtro = append(filtro, bson.E{Key: "variantes.color", Value: f.Color})
	}
	if f.Texto != "" {
		filtro = append(filtro, bson.E{Key: "$text", Value: bson.D{{Key: "$search", Value: f.Texto}}})
	}

	var orden bson.D
	switch f.Orden {
	case modelo.OrdenPrecioAsc:
		orden = bson.D{{Key: "precioBase.monto", Value: 1}, {Key: "_id", Value: 1}}
	case modelo.OrdenPrecioDesc:
		orden = bson.D{{Key: "precioBase.monto", Value: -1}, {Key: "_id", Value: 1}}
	case modelo.OrdenNombre:
		orden = bson.D{{Key: "nombre", Value: 1}, {Key: "_id", Value: 1}}
	default:
		orden = bson.D{{Key: "creadoEn", Value: -1}, {Key: "_id", Value: 1}}
	}

	total, err := r.productos.CountDocuments(ctx, filtro)
	if err != nil {
		return nil, 0, err
	}
	cursor, err := r.productos.Find(ctx, filtro, options.Find().
		SetSort(orden).
		SetSkip(int64((f.Pagina-1)*f.Tamano)).
		SetLimit(int64(f.Tamano)))
	if err != nil {
		return nil, 0, err
	}
	var productos []modelo.Producto
	if err := cursor.All(ctx, &productos); err != nil {
		return nil, 0, err
	}
	return productos, total, nil
}

// ProductosPublicados trae el catálogo público completo: el contrato no pagina y son del orden de
// cientos de productos (ADR-008). Se ordena por _id para que la respuesta sea estable entre réplicas.
func (r *Repositorio) ProductosPublicados(ctx context.Context) ([]modelo.Producto, error) {
	cursor, err := r.productos.Find(ctx, bson.D{{Key: "publicado", Value: true}},
		options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var productos []modelo.Producto
	if err := cursor.All(ctx, &productos); err != nil {
		return nil, err
	}
	return productos, nil
}

// ProductosSinPublicar es la lectura del outbox de Catálogo (ADR-006): los productos cuya versión todavía no se
// avisó. Un producto sin publicadoVersion cuenta como 0. Compara dos campos del mismo documento, así que recorre
// la colección; son del orden de cientos o pocos miles, y para esto no hace falta un índice.
func (r *Repositorio) ProductosSinPublicar(ctx context.Context, limite int) ([]modelo.CambioProducto, error) {
	cursor, err := r.productos.Find(ctx,
		bson.D{{Key: "$expr", Value: bson.D{{Key: "$gt", Value: bson.A{"$version", bson.D{{Key: "$ifNull", Value: bson.A{"$publicadoVersion", 0}}}}}}}},
		options.Find().SetProjection(bson.D{{Key: "version", Value: 1}, {Key: "correlacionCambio", Value: 1}}).SetLimit(int64(limite)))
	if err != nil {
		return nil, err
	}
	var cambios []modelo.CambioProducto
	if err := cursor.All(ctx, &cambios); err != nil {
		return nil, err
	}
	return cambios, nil
}

// MarcarPublicado registra la versión avisada, sin tocar version ni actualizadoEn. $max es atómico y solo sube:
// si otra réplica ya marcó una versión mayor, no la pisa. Un cambio posterior deja el producto otra vez pendiente.
func (r *Repositorio) MarcarPublicado(ctx context.Context, id string, version int) error {
	_, err := r.productos.UpdateOne(ctx, bson.D{{Key: "_id", Value: id}},
		bson.D{{Key: "$max", Value: bson.D{{Key: "publicadoVersion", Value: version}}}})
	return err
}

// ProductosPorIDs lee los productos que encontró la búsqueda. Sin orden garantizado: lo define quien llama.
func (r *Repositorio) ProductosPorIDs(ctx context.Context, ids []string) ([]modelo.Producto, error) {
	cursor, err := r.productos.Find(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: ids}}}})
	if err != nil {
		return nil, err
	}
	var productos []modelo.Producto
	if err := cursor.All(ctx, &productos); err != nil {
		return nil, err
	}
	return productos, nil
}

// ProductosModificadosDesde incluye los despublicados: el índice también tiene que quitarlos.
func (r *Repositorio) ProductosModificadosDesde(ctx context.Context, desde time.Time) ([]modelo.Producto, error) {
	cursor, err := r.productos.Find(ctx, bson.D{{Key: "actualizadoEn", Value: bson.D{{Key: "$gte", Value: desde}}}})
	if err != nil {
		return nil, err
	}
	var productos []modelo.Producto
	if err := cursor.All(ctx, &productos); err != nil {
		return nil, err
	}
	return productos, nil
}

// ActualizarProducto es un PUT con control optimista de concurrencia (DISENO.md 5.9, caso
// secundario): el filtro incluye la versión que leyó el administrador. Si otro cambio se coló, la
// versión guardada ya es otra, el filtro no coincide y no se pisa nada. findOneAndUpdate devuelve el
// documento ya actualizado en la misma operación.
func (r *Repositorio) ActualizarProducto(ctx context.Context, id string, version int, d modelo.DatosProducto, ahora time.Time) (modelo.Producto, error) {
	set := bson.D{
		{Key: "nombre", Value: d.Nombre},
		{Key: "categoria", Value: d.Categoria},
		{Key: "precioBase", Value: d.PrecioBase},
		{Key: "actualizadoEn", Value: ahora},
		correlacionDelCambio(ctx),
	}
	// Es un reemplazo completo de los datos editables: un opcional que no vino se quita del documento.
	unset := bson.D{}
	opcional := func(campo string, presente bool, valor any) {
		if presente {
			set = append(set, bson.E{Key: campo, Value: valor})
		} else {
			unset = append(unset, bson.E{Key: campo, Value: ""})
		}
	}
	opcional("descripcion", d.Descripcion != "", d.Descripcion)
	opcional("atributos", len(d.Atributos) > 0, d.Atributos)
	opcional("fotos", len(d.Fotos) > 0, d.Fotos)

	cambio := bson.D{{Key: "$set", Value: set}, {Key: "$inc", Value: bson.D{{Key: "version", Value: 1}}}}
	if len(unset) > 0 {
		cambio = append(cambio, bson.E{Key: "$unset", Value: unset})
	}

	var p modelo.Producto
	err := r.productos.FindOneAndUpdate(ctx,
		bson.D{{Key: "_id", Value: id}, {Key: "version", Value: version}},
		cambio,
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&p)
	if errors.Is(err, mongo.ErrNoDocuments) {
		if existe, errExiste := r.existeProducto(ctx, id); errExiste != nil {
			return modelo.Producto{}, errExiste
		} else if !existe {
			return modelo.Producto{}, modelo.ErrProductoNoEncontrado
		}
		return modelo.Producto{}, modelo.ErrConflictoDeVersion
	}
	return p, err
}

// Publicar cambia publicado a true solo si estaba en false; así publicar dos veces no sube la
// versión dos veces. Devuelve el producto como quedó.
func (r *Repositorio) Publicar(ctx context.Context, id string, ahora time.Time) (modelo.Producto, error) {
	var p modelo.Producto
	err := r.productos.FindOneAndUpdate(ctx,
		bson.D{{Key: "_id", Value: id}, {Key: "publicado", Value: false}},
		cambioDePublicacion(ctx, true, ahora),
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&p)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return r.ObtenerProducto(ctx, id) // ya estaba publicado, o no existe (404)
	}
	return p, err
}

// Despublicar es la baja lógica del DELETE (D2). Idempotente: si ya estaba despublicado no cambia
// nada y no es un error.
func (r *Repositorio) Despublicar(ctx context.Context, id string, ahora time.Time) error {
	res, err := r.productos.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: id}, {Key: "publicado", Value: true}},
		cambioDePublicacion(ctx, false, ahora))
	if err != nil || res.MatchedCount == 1 {
		return err
	}
	if existe, err := r.existeProducto(ctx, id); err != nil {
		return err
	} else if !existe {
		return modelo.ErrProductoNoEncontrado
	}
	return nil
}

func cambioDePublicacion(ctx context.Context, publicado bool, ahora time.Time) bson.D {
	return bson.D{
		{Key: "$set", Value: bson.D{{Key: "publicado", Value: publicado}, {Key: "actualizadoEn", Value: ahora}, correlacionDelCambio(ctx)}},
		{Key: "$inc", Value: bson.D{{Key: "version", Value: 1}}},
	}
}

func (r *Repositorio) existeProducto(ctx context.Context, id string) (bool, error) {
	n, err := r.productos.CountDocuments(ctx, bson.D{{Key: "_id", Value: id}}, options.Count().SetLimit(1))
	return n > 0, err
}

// InsertarProducto no pregunta antes si el id existe: inserta y deja que la base decida. Así no
// hay carrera entre "leer" y "escribir" (sección 13). Una clave duplicada (E11000) se traduce según
// el índice que la rechazó.
func (r *Repositorio) InsertarProducto(ctx context.Context, p modelo.Producto) error {
	p.CorrelacionCambio = observabilidad.CorrelationID(ctx)
	_, err := r.productos.InsertOne(ctx, p)
	if mongo.IsDuplicateKeyError(err) {
		switch {
		case strings.Contains(err.Error(), "index: _id_"):
			return modelo.ErrIDProductoEnUso
		case strings.Contains(err.Error(), "index: "+indiceVariantes):
			return modelo.ErrVarianteDuplicada
		}
	}
	return err
}

// ObtenerVariantes busca por lote las variantes con esos ids y las devuelve junto con los datos de su
// producto. Usa el índice multikey variantes_id_unico en el primer $match; el segundo, después del
// $unwind, deja solo las variantes pedidas y no las demás del mismo producto. No filtra por publicado
// (D6): el que llama decide. Devuelve solo las que existen y en cualquier orden; el servicio detecta
// las que faltan.
func (r *Repositorio) ObtenerVariantes(ctx context.Context, ids []string) ([]modelo.DetalleVariante, error) {
	enLista := bson.D{{Key: "variantes.id", Value: bson.D{{Key: "$in", Value: ids}}}}
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: enLista}},
		{{Key: "$unwind", Value: "$variantes"}},
		{{Key: "$match", Value: enLista}},
		{{Key: "$project", Value: bson.D{
			{Key: "_id", Value: 0},
			{Key: "productoId", Value: "$_id"},
			{Key: "nombre", Value: 1},
			{Key: "categoria", Value: 1},
			{Key: "publicado", Value: 1},
			{Key: "precioBase", Value: 1},
			{Key: "varianteId", Value: "$variantes.id"},
			{Key: "talle", Value: "$variantes.talle"},
			{Key: "color", Value: "$variantes.color"},
		}}},
	}
	cursor, err := r.productos.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	var detalles []modelo.DetalleVariante
	if err := cursor.All(ctx, &detalles); err != nil {
		return nil, err
	}
	return detalles, nil
}

// InsertarPromocion inserta y deja que la base decida si el id está libre, igual que InsertarProducto.
func (r *Repositorio) InsertarPromocion(ctx context.Context, p modelo.Promocion) error {
	_, err := r.promociones.InsertOne(ctx, p)
	if mongo.IsDuplicateKeyError(err) {
		return modelo.ErrIDPromocionEnUso
	}
	return err
}

// ReemplazarPromocion cambia el documento completo; el _id es el mismo. Es una sola operación atómica.
func (r *Repositorio) ReemplazarPromocion(ctx context.Context, p modelo.Promocion) error {
	res, err := r.promociones.ReplaceOne(ctx, bson.D{{Key: "_id", Value: p.ID}}, p)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return modelo.ErrPromocionNoEncontrada
	}
	return nil
}

// ListarPromociones trae todas, vencidas incluidas, las más recientes primero. Son pocas: no se paginan.
func (r *Repositorio) ListarPromociones(ctx context.Context) ([]modelo.Promocion, error) {
	cursor, err := r.promociones.Find(ctx, bson.D{},
		options.Find().SetSort(bson.D{{Key: "vigencia.desde", Value: -1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var promociones []modelo.Promocion
	if err := cursor.All(ctx, &promociones); err != nil {
		return nil, err
	}
	return promociones, nil
}

// PromocionesVigentes trae las promociones cuya vigencia incluye ese momento. Usa el índice vigencia
// del script 01. Son pocas (una tienda tiene unas pocas a la vez), por eso se traen todas y el filtro
// por alcance y medio de pago lo hace el servicio.
func (r *Repositorio) PromocionesVigentes(ctx context.Context, ahora time.Time) ([]modelo.Promocion, error) {
	filtro := bson.D{
		{Key: "vigencia.desde", Value: bson.D{{Key: "$lte", Value: ahora}}},
		{Key: "vigencia.hasta", Value: bson.D{{Key: "$gte", Value: ahora}}},
	}
	cursor, err := r.promociones.Find(ctx, filtro)
	if err != nil {
		return nil, err
	}
	var promociones []modelo.Promocion
	if err := cursor.All(ctx, &promociones); err != nil {
		return nil, err
	}
	return promociones, nil
}

// correlacionDelCambio guarda en el producto el track id del pedido que lo cambió (ADR-020). El evento que avisa
// ese cambio sale después, desde el outbox, y lo lleva: así los logs de la indexación tienen el mismo track id.
func correlacionDelCambio(ctx context.Context) bson.E {
	return bson.E{Key: "correlacionCambio", Value: observabilidad.CorrelationID(ctx)}
}
