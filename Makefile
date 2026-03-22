.PHONY: build test vet lint clean preview up destroy tidy

STACK ?= dev

build:
	go build ./...

test:
	go test ./...

vet:
	go vet ./...

lint: vet

clean:
	go clean ./...

tidy:
	go mod tidy

preview:
	FORGE_STACK=$(STACK) go run ./cmd/forge preview

up:
	FORGE_STACK=$(STACK) go run ./cmd/forge up

destroy:
	FORGE_STACK=$(STACK) go run ./cmd/forge destroy
