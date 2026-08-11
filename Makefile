.PHONY: build build-go build-web test check dev clean

build: build-web build-go

build-web:
	pnpm build

build-go:
	go build -o build/mutiblog ./cmd/mutiblog

test:
	go test ./...
	pnpm test

check:
	go test ./...
	pnpm typecheck
	pnpm build

dev:
	go run ./cmd/mutiblog

clean:
	go clean
	pnpm -r exec -- rm -rf dist
