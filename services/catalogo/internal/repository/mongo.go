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
)

type Repositorio struct {
	cliente    *mongo.Client
	productos  *mongo.Collection
	categorias *mongo.Collection
}

// indiceVariantes tiene que coincidir en nombre y definición con el de
// tp-mongo/scripts/01_crear_colecciones_e_indices.js; si difiere, Mongo rechaza la creación.
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
	return &Repositorio{cliente: cliente, productos: db.Collection("productos"), categorias: db.Collection("categorias")}, nil
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

// InsertarProducto no pregunta antes si el id existe: inserta y deja que la base decida. Así no
// hay carrera entre "leer" y "escribir" (sección 13). Una clave duplicada (E11000) se traduce según
// el índice que la rechazó.
func (r *Repositorio) InsertarProducto(ctx context.Context, p modelo.Producto) error {
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
