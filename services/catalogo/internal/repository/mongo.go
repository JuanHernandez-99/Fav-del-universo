// Package repository es el único lugar que habla con MongoDB (sección 13): el resto del servicio
// no importa el driver.
package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

type Repositorio struct {
	cliente *mongo.Client
	db      *mongo.Database
}

// Conectar arma el cliente. No abre la conexión todavía: el driver conecta recién en la primera
// operación, por eso quien llama debe usar Ping para saber si la base responde.
func Conectar(uri, base string) (*Repositorio, error) {
	cliente, err := mongo.Connect(options.Client().
		ApplyURI(uri).
		SetServerSelectionTimeout(3 * time.Second))
	if err != nil {
		return nil, err
	}
	return &Repositorio{cliente: cliente, db: cliente.Database(base)}, nil
}

// Ping verifica que la base responda. Lo usan el arranque y la sonda /readyz.
func (r *Repositorio) Ping(ctx context.Context) error {
	return r.cliente.Ping(ctx, readpref.Primary())
}

// Cerrar libera las conexiones del pool al apagar el servicio.
func (r *Repositorio) Cerrar(ctx context.Context) error {
	return r.cliente.Disconnect(ctx)
}
