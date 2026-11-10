package observabilidad

import (
	"context"
	"strconv"
	"sync"

	"go.mongodb.org/mongo-driver/v2/event"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// MonitorMongo abre un span por comando que Catálogo manda a MongoDB (find, insert, update…), usando el monitor de
// comandos del propio driver. No se usa otelmongo porque su versión para el driver v2 todavía no tiene release.
// El span lleva el comando y la colección, nunca el filtro ni los documentos.
func MonitorMongo() *event.CommandMonitor {
	var abiertos sync.Map // clave: conexión + id del pedido al servidor → span
	clave := func(conexion string, pedido int64) string { return conexion + "/" + strconv.FormatInt(pedido, 10) }

	terminar := func(e event.CommandFinishedEvent, err error) {
		valor, ok := abiertos.LoadAndDelete(clave(e.ConnectionID, e.RequestID))
		if !ok {
			return
		}
		span := valor.(trace.Span)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}

	return &event.CommandMonitor{
		Started: func(ctx context.Context, e *event.CommandStartedEvent) {
			atributos := []attribute.KeyValue{
				semconv.DBSystemMongoDB, semconv.DBNamespace(e.DatabaseName), semconv.DBOperationName(e.CommandName),
			}
			nombre := e.CommandName
			// El primer campo del comando es su nombre y su valor la colección: {"find": "productos", …}.
			if primero, err := e.Command.IndexErr(0); err == nil {
				if coleccion, ok := primero.Value().StringValueOK(); ok {
					atributos = append(atributos, semconv.DBCollectionName(coleccion))
					nombre += " " + coleccion
				}
			}
			_, span := tracer().Start(ctx, nombre, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(atributos...))
			if span.SpanContext().IsValid() {
				abiertos.Store(clave(e.ConnectionID, e.RequestID), span)
			}
		},
		Succeeded: func(_ context.Context, e *event.CommandSucceededEvent) { terminar(e.CommandFinishedEvent, nil) },
		Failed:    func(_ context.Context, e *event.CommandFailedEvent) { terminar(e.CommandFinishedEvent, e.Failure) },
	}
}
