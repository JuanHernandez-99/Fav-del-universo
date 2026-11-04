# Imagen de Usuarios. Se construye desde la carpeta del servicio: docker build services/usuarios
FROM golang:1.26-alpine AS compilacion
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOWORK=off go build -trimpath -ldflags="-s -w" -o /usuarios ./cmd/usuarios

FROM alpine:3.22
RUN apk add --no-cache ca-certificates wget && adduser -D -u 10001 usuarios \
  && mkdir -p /claves/privada /claves/publica && chown -R usuarios /claves
USER usuarios
COPY --from=compilacion /usuarios /usuarios
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=2s --retries=3 CMD wget -qO- http://localhost:8080/readyz || exit 1
ENTRYPOINT ["/usuarios"]
