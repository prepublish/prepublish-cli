# Prepublish CLI. Everything here runs from prepublish-cli/; `make build` is the
# one command a contributor needs.
BINARY  := prepublish
PKG     := github.com/prepublish/prepublish-cli
VERSION_PKG := $(PKG)/internal/version

# Stamped into the binary so `prepublish version`, the User-Agent and a bug
# report all name the same build. A source checkout with no tags still gets a
# commit and a timestamp, which is enough to identify it.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X $(VERSION_PKG).Version=$(VERSION) \
	-X $(VERSION_PKG).Commit=$(COMMIT) \
	-X $(VERSION_PKG).Date=$(DATE)

# Packaging is GoReleaser's job, pinned so a local `make release` builds with the
# same tool that the release workflow runs. There is deliberately no second,
# hand-rolled cross-compile path: two packagers is how the archive names, the
# checksums file and the attached installer drift apart.
GORELEASER_VERSION := v2.18.2
GORELEASER := go run github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION)

.PHONY: all build install test vet lint release release-check clean

all: build

# The binary a contributor runs. ./bin/ is gitignored.
build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/$(BINARY)

# Installs into GOBIN or GOPATH/bin. Packagers use this instead of build.
install:
	go install -ldflags "$(LDFLAGS)" ./cmd/$(BINARY)

test:
	go test ./...

vet:
	go vet ./...

# golangci-lint is optional: a contributor without it still gets vet and test,
# and CI installs it (pinned) before running this target.
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed; install it with:"; \
		echo "  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"; \
	fi

# dist/, with the release's own asset names and a checksums.txt, built from the
# working tree: what CI would produce, without tagging or uploading anything.
release: clean
	$(GORELEASER) release --snapshot --clean

# Validates .goreleaser.yaml only. Cheap, and the thing to run after touching it.
release-check:
	$(GORELEASER) check

clean:
	rm -rf bin dist
