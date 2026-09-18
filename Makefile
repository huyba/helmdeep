BINARY     := helmdeep-gateway
CMD        := ./cmd/helmdeep-gateway
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS    := -s -w -X main.version=$(VERSION)
IMAGE      := helmdeep-gateway:$(VERSION)
CONFIG     ?= config.yaml

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
