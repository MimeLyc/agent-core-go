.PHONY: build run test lint fmt setup clean

# Load .env if present
ifneq (,$(wildcard .env))
  include .env
  export
endif

build:
	go build ./cmd/server/

run:
	go run ./cmd/server/

test:
	go test ./...

lint:
	go vet ./...

fmt:
	gofmt -w .

setup:
	go mod tidy

clean:
	rm -f server
