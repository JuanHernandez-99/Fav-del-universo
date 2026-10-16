# Compilación
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/inventario ./cmd/inventario

# Imagen final: solo el binario
FROM alpine:3.20
RUN adduser -D -H app
USER app
COPY --from=build /out/inventario /usr/local/bin/inventario
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --retries=5 CMD wget -qO- http://localhost:8080/health || exit 1
ENTRYPOINT ["inventario"]
