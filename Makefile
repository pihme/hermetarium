GO ?= go
HVER ?= $(shell git describe --tags --match 'hermetarium/v*' --always --dirty 2>/dev/null || echo dev)
PVER ?= $(shell git describe --tags --match 'porter/v*' --always --dirty 2>/dev/null || echo dev)
HVER := $(patsubst hermetarium/v%,%,$(HVER))
PVER := $(patsubst porter/v%,%,$(PVER))

.PHONY: test test-live build tidy echo-image

ECHO_IMAGE ?= hermetarium/echo:dev

build:
	$(GO) build -ldflags='-s -w -X github.com/pihme/hermetarium/supervisor.Version=$(HVER)' -o bin/hermetarium ./supervisor/cmd/hermetarium
	$(GO) build -ldflags='-s -w -X main.Version=$(PVER)' -o bin/porter ./porter

test:
	$(GO) test ./supervisor ./firecracker-helper ./examples/... ./porter ./tests/... -count=1 -p 1 -timeout 15m -v

test-live:
	$(GO) test -tags live ./tests/agentd ./tests/inhabitants -count=1 -p 1 -timeout 15m -v

tidy:
	$(GO) mod tidy

echo-image:
	mkdir -p .cache/echo-image
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -ldflags='-s -w' -o .cache/echo-image/echo-service ./examples/echo
	docker build -t $(ECHO_IMAGE) -f examples/echo/Dockerfile .cache/echo-image
