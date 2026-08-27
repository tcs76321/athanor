# Athanor development shortcuts.
#
# CGO is REQUIRED for all builds: internal/store uses mattn/go-sqlite3 so
# sqlite-vec can be loaded at runtime later (see docs/sqlite-setup.md and
# docs/adr/0003). Never run plain `go build` / `go test` directly; use these
# targets so CGO_ENABLED is set consistently.

CGO_ENABLED = 1
export CGO_ENABLED

.PHONY: build test test-race vet lint check tidy run clean hooks bench

build:
	go build -o bin/athanor ./cmd/athanor

run:
	go run ./cmd/athanor

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

lint:
	golangci-lint run --timeout=5m

# Engine throughput baseline. Runs the M1 full-chain benchmark 10x
# against a fake Ollama (no network). The result is the comparison
# point for M3's multi-candidate evaluation: M3 should be slower per
# job (N candidates per phase) but the per-phase cost should remain
# in the same order of magnitude. Numbers from the first run are
# recorded in docs/benchmarks/engine-m1.txt.
bench:
	go test -bench=. -benchtime=10x -run=^$$ ./internal/engine/

# Aggregate gate; run before pushing. The pre-push hook also calls this.
check: lint vet test-race

tidy:
	go mod tidy

clean:
	rm -rf state backups

# Install the pre-push hook so CI lint/vet failures surface locally before
# the push lands. Bypass with: git push --no-verify
hooks:
	bash scripts/install-hooks.sh
