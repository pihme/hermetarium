GO ?= go

.PHONY: test test-live build tidy

build:
	$(GO) build -o bin/hermetarium ./supervisor/cmd/hermetarium

test:
	$(GO) test ./supervisor ./examples/... ./porter ./tests/... -count=1 -p 1 -timeout 15m -v

test-live:
	$(GO) test -tags live ./tests/claude-code ./tests/grok-build ./tests/deepseek-harness ./tests/inhabitants -count=1 -p 1 -timeout 15m -v

tidy:
	$(GO) mod tidy
