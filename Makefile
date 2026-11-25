GO ?= go

.PHONY: tidy fmt-check build vet fix-check test check

tidy:
	@$(GO) mod tidy

fmt-check:
	@out="$$(gofmt -l .)" && test -z "$$out"

build:
	@CGO_ENABLED=0 $(GO) build ./...

vet:
	@$(GO) vet ./...

fix-check:
	@out="$$($(GO) fix -diff ./...)" && test -z "$$out"

test:
	@CGO_ENABLED=0 $(GO) test -count=1 ./...
	@CGO_ENABLED=1 $(GO) test -race -count=1 ./...

check: fmt-check build vet fix-check test
