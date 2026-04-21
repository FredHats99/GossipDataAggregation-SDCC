# syntax=docker/dockerfile:1.7

FROM golang:1.24-alpine AS builder
WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/gossip-node ./cmd/node

FROM alpine:3.20
RUN adduser -D -u 10001 appuser
USER appuser
WORKDIR /app

COPY --from=builder /out/gossip-node /app/gossip-node

EXPOSE 8080
ENTRYPOINT ["/app/gossip-node"]
