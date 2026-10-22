FROM golang:1.26-alpine AS compilacion
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOWORK=off go build -trimpath -ldflags="-s -w" -o /mock .

FROM alpine:3.22
RUN adduser -D -u 10001 mock
USER mock
COPY --from=compilacion /mock /mock
EXPOSE 8080
ENTRYPOINT ["/mock"]
