# Imagen de Ventas. Se construye desde la carpeta del servicio: docker build services/ventas
FROM golang:1.26-alpine AS compilacion
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOWORK=off go build -trimpath -ldflags="-s -w" -o /ventas ./cmd/ventas

FROM alpine:3.22
RUN apk add --no-cache ca-certificates wget && adduser -D -u 10001 ventas
USER ventas
COPY --from=compilacion /ventas /ventas
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=2s --retries=3 CMD wget -qO- http://localhost:8080/readyz || exit 1
ENTRYPOINT ["/ventas"]
