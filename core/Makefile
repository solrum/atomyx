# Atomyx core runtime Makefile
#
# Builds the CLI, MCP stdio server, sidecar runtime, and schema
# generators standalone, with no dependency on the root Makefile. The
# root Makefile's build-core / release-core targets delegate here.

REPO_ROOT := $(abspath .)

# GO_VERSION_PKG is the package whose Version var `build` stamps via
# ldflags, so every binary's version surface reports exactly what this
# target built. GO_BUILD_VERSION prefers a tag reachable from the
# current commit, falls back to the short commit itself when the repo
# carries no tags yet, and falls back again to the package's own
# unstamped default when git is unavailable at all (for example a
# tarball build with no .git directory). Both may be overridden by the
# caller (the root Makefile passes its own computed values so the two
# entry points stamp identical versions).
GO_VERSION_PKG    ?= dev.atomyx/internal/version
GO_BUILD_VERSION  ?= $(shell git -C $(REPO_ROOT) describe --tags --always --dirty 2>/dev/null || echo dev)

# GO_DIST_DIR holds the binaries `release` produces plus the
# SHA256SUMS file recording their checksums, kept separate from
# `build`'s bin/ so a developer's everyday build never mixes with a
# release artifact. GO_RELEASE_BINARIES lists the entry points a
# release ships to operators, excluding the schema generators, which
# are developer tooling and not a distributed artifact.
GO_DIST_DIR          := $(REPO_ROOT)/dist
GO_RELEASE_BINARIES  ?= atomyx atomyx-mcp atomyx-runtime

.PHONY: build test release gen-schema check-schema check

.DEFAULT_GOAL := build

# Build every command under cmd/ (the atomyx CLI, the atomyx-mcp
# stdio server, the atomyx-runtime sidecar, and the schema generators)
# into bin/.
build:
	@echo "Building core binaries into bin/ (version $(GO_BUILD_VERSION)) ..."
	@go build -ldflags "-X $(GO_VERSION_PKG).Version=$(GO_BUILD_VERSION)" -o bin/ ./cmd/...
	@echo "Built: atomyx atomyx-mcp atomyx-runtime gen-config-schema gen-script-schema"

test:
	go test ./...

# Build the binaries a release ships, the way a release ships them:
# with the local checkout's absolute path trimmed from the binary
# (-trimpath) and the C toolchain disabled (CGO_ENABLED=0, safe
# because no Go code in this module uses cgo), so a build of the same
# commit is byte-identical no matter which machine or directory
# produced it. Emits a SHA256SUMS file next to the binaries so an
# operator can verify a distributed artifact against the checksum
# recorded at release time.
release:
	@echo "Building reproducible core binaries into $(GO_DIST_DIR) (version $(GO_BUILD_VERSION)) ..."
	@rm -rf $(GO_DIST_DIR)
	@mkdir -p $(GO_DIST_DIR)
	@CGO_ENABLED=0 go build -trimpath \
		-ldflags "-X $(GO_VERSION_PKG).Version=$(GO_BUILD_VERSION)" \
		-o $(GO_DIST_DIR)/ $(addprefix ./cmd/,$(GO_RELEASE_BINARIES))
	@cd $(GO_DIST_DIR) && ( command -v sha256sum >/dev/null 2>&1 \
		&& sha256sum $(GO_RELEASE_BINARIES) \
		|| shasum -a 256 $(GO_RELEASE_BINARIES) ) > SHA256SUMS
	@echo "Built: $(GO_RELEASE_BINARIES)"
	@echo "Checksums: $(GO_DIST_DIR)/SHA256SUMS"
	@cat $(GO_DIST_DIR)/SHA256SUMS

# Regenerate the checked-in JSON Schema files from the Go types that
# define them. Run after any change to internal/script's or
# internal/config's field or schema-generation types.
gen-schema:
	go run ./cmd/gen-script-schema -out internal/script/atomyx-script.schema.json
	go run ./cmd/gen-config-schema -out internal/config/atomyx-config.schema.json

# Fail when the committed JSON Schema files no longer match the Go
# structs they are generated from. Run this before trusting a schema
# change is complete; gen-schema alone does not verify anything.
check-schema:
	bash scripts/check-schema-fresh.sh

# The full local gate for the core module: format, build, vet, the
# layer-import gate, the lint gate, the comment gate, the raw-open
# gate, the schema freshness gate, and the race-enabled test suite, in
# that order, each one failing the target the moment it fails.
# go-arch-lint and golangci-lint must already be on PATH (see CONTRIBUTING.md for the
# install commands); golangci-lint must be v2.13.2, the version this
# module's .golangci.yml is authored and verified against, because a
# different version can load the config differently or reject it.
check:
	@echo "Format gate ..."
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then echo "$$unformatted"; exit 1; fi
	@echo "Build ..."
	go build ./...
	@echo "Vet ..."
	go vet ./...
	@echo "Layer-import gate ..."
	go-arch-lint check --project-path .
	@echo "Lint gate ..."
	golangci-lint run ./...
	@echo "Comment gate ..."
	bash scripts/check-comments.sh
	@echo "Raw syscall.Open gate ..."
	bash scripts/check-no-raw-syscall-open.sh
	@echo "Session test-helper import gate ..."
	bash scripts/check-sessiontest-imports.sh
	@echo "Schema freshness gate ..."
	bash scripts/check-schema-fresh.sh
	@echo "Test ..."
	go test -race ./...
	@echo "core check passed."
