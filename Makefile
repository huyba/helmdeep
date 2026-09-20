BINARY     := helmdeep-gateway
CMD        := ./cmd/helmdeep-gateway
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS    := -s -w -X main.version=$(VERSION)
IMAGE      := helmdeep-gateway:$(VERSION)
CONFIG     ?= config.yaml
FUZZTIME   ?= 10s

.PHONY: build
build:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/$(BINARY) $(CMD)

.PHONY: test
test:
	go test -race ./...

.PHONY: lint
lint:
	golangci-lint run

.PHONY: bench
bench:
	go test -run '^$$' -bench . -benchmem ./...

# go test -fuzz accepts exactly one target per invocation, so `test`
# (which runs every seed corpus entry as an ordinary test case, giving
# regression coverage on every run) and `fuzz` (which spends real time
# generating new inputs) are deliberately separate targets — CI runs
# `test` on every PR; `fuzz` is for a deliberate, bounded local or
# scheduled run. Override duration with FUZZTIME=30s make fuzz.
.PHONY: fuzz
fuzz:
	go test ./pkg/mcp/... -fuzz FuzzServeHTTP -fuzztime $(FUZZTIME)
	go test ./pkg/policy/... -fuzz FuzzLoad -fuzztime $(FUZZTIME)
	go test ./internal/gateway/... -fuzz FuzzRedactPath -fuzztime $(FUZZTIME)

.PHONY: docker
docker:
	docker build -f deploy/docker/Dockerfile -t $(IMAGE) .

.PHONY: verify-chain
verify-chain: build
	./bin/$(BINARY) verify-chain -config=$(CONFIG)

.PHONY: clean
clean:
	rm -rf bin/

.PHONY: fmt
fmt:
	gofmt -l -w .

.PHONY: vuln
vuln:
	govulncheck ./...
