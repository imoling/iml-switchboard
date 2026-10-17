export PATH := /opt/homebrew/bin:$(PATH)

.PHONY: build test check smoke ui-check all

build:
	go build -o bin/imls ./cmd/imls

test:
	go vet ./... && go test ./...

smoke: build
	./scripts/smoke.sh

ui-check:
	cd ui && npm run check

check: test smoke

all: check ui-check
