# nfsense — developer entry points.
#
# The per-commit gate is `make test`. It covers every package the binary is
# built from, not just the package a test happens to live in: the sampling rule
# in 01-VALIDATION.md is widened here from ./internal/store to the whole first
# party tree (W8).
GO ?= go

# Every package that can end up in the binary.
PKGS := ./internal/... ./cmd/...

.PHONY: build test test-race lint gate

build:
	$(GO) build ./...

# Per-commit feedback. Keep this fast: it is the command run before each commit.
test:
	$(GO) test $(PKGS) -count=1

# Full suite with the race detector; run before a wave merge and before
# /gsd-verify-work.
test-race:
	$(GO) test ./... -count=1 -race

# `gofmt -l` prints the files it would reformat and still exits 0, so the
# listing has to be turned into a failure — otherwise lint can never fail.
lint:
	@files="$$(gofmt -l .)"; \
	if [ -n "$$files" ]; then echo "gofmt needed:"; echo "$$files"; exit 1; fi
	$(GO) vet ./...

# Everything that must be true before code is trusted: it builds, it is
# formatted and vetted, it passes the full suite under the race detector, and
# no firewall capability is reachable from the module (T-1-05).
gate: build lint test-race
	$(GO) test ./internal/store -run 'TestNoBannedImports' -count=1
