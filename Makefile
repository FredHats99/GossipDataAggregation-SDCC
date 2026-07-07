APP_BIN := gossip-node

.PHONY: build test test-race test-integration test-docker lint run

build:
	go build -o bin/$(APP_BIN) ./cmd/node

test:
	go test -count=1 ./...

test-race:
	go test -race -count=1 ./...

test-integration:
	go test -tags=integration -count=1 ./...

test-docker:
	docker run --rm -v "$(CURDIR):/src" -w /src golang:1.24 go test -count=1 ./...

lint:
	go vet ./...

run:
	go run ./cmd/node
