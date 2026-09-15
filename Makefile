GO ?= go

.PHONY: test build tidy

build:
	$(GO) build -o bin/hermetarium ./supervisor/cmd/hermetarium

test:
	$(GO) test ./supervisor ./examples/... ./tests/... -count=1 -timeout 8m -v

tidy:
	$(GO) mod tidy
