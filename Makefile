GO ?= go
BACKEND := backend
VERSION ?= dev

.PHONY: build dev test lint api-types check-api-types frontend-build

build:
	cd $(BACKEND) && CGO_ENABLED=0 $(GO) build -trimpath -ldflags='-s -w -X main.version=$(VERSION)' -o ../blog-server ./cmd/blog

dev:
	cd $(BACKEND) && $(GO) run ./cmd/blog --dev

test: check-api-types
	cd $(BACKEND) && $(GO) test ./...

lint:
	cd $(BACKEND) && $(GO) vet ./...

api-types:
	cd frontend/admin && ./node_modules/.bin/openapi-typescript ../../backend/api/openapi.yaml -o src/api/schema.d.ts

check-api-types:
	@tmp=$$(mktemp /private/tmp/mutiblog-api-types.XXXXXX); cp frontend/admin/src/api/schema.d.ts $$tmp; $(MAKE) api-types; cmp -s frontend/admin/src/api/schema.d.ts $$tmp || (rm -f $$tmp; echo 'generated API types are out of date'; exit 1); rm -f $$tmp

frontend-build: api-types
	cd frontend/admin && ./node_modules/.bin/vite build
