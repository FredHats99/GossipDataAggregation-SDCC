APP_BIN := gossip-node

.PHONY: build test lint run

build:
	go build -o bin/$(APP_BIN) ./cmd/node

test:
	go test ./...

lint:
	go vet ./...

run:
	go run ./cmd/node
