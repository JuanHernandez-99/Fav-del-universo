# Simulador de Mercado Pago (ADR-014). Sin dependencias externas: solo la biblioteca estándar.
FROM golang:1.26-alpine AS compilacion
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOWORK=off go build -trimpath -ldflags="-s -w" -o /simulador .

FROM alpine:3.22
RUN adduser -D -u 10001 simulador
USER simulador
COPY --from=compilacion /simulador /simulador
EXPOSE 8080
ENTRYPOINT ["/simulador"]
