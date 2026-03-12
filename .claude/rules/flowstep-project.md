# Project: flowstep

**Last Updated:** 2026-03-12

## Overview

Declarative workflow engine for Go. Define state machines with a fluent builder API, execute transitions with guards, signals, activities, and human-in-the-loop tasks. Core package has zero external dependencies.

## Technology Stack

- **Language:** Go 1.25.3
- **Module:** `github.com/mawkeye/flowstep`
- **Testing:** stdlib `testing` (no testify)
- **Adapters:** pgx v5, redis v9, nats, asynq, sqlite (modernc), DynamoDB
- **Current Version:** v0.12.0 (pre-1.0, SemVer)

## Directory Structure

```
flowstep/           # Root — public API contract (interfaces, entry points, options, errors)
├── types/          # Pure domain structs (zero deps on other packages)
├── internal/
│   ├── runtime/    # State machine execution (executor, scheduler, dispatcher, parallel)
│   ├── builder/    # DSL builder implementation
│   └── graph/      # Validation, reachability, compilation
├── adapters/       # Pluggable implementations
│   ├── memstore/   # In-memory (dev/test)
│   ├── pgxstore/   # PostgreSQL (production)
│   ├── sqlitestore/# SQLite
│   ├── dynamostore/# DynamoDB
│   ├── chanbus/    # Channel-based event bus
│   ├── redisbus/   # Redis pub/sub
│   ├── natsbus/    # NATS pub/sub
│   ├── memrunner/  # In-memory activity runner
│   ├── goroutinerunner/ # Goroutine-based runner
│   ├── asynqrunner/# Asynq task queue runner
│   └── slogadapter/# Structured logging observer
├── testutil/       # Test helpers: NewTestEngine, FakeClock, fixtures
├── examples/       # 01-07 example programs
└── docs/           # Plans, steps, analysis
```

## Development Commands

| Task | Command |
|------|---------|
| Full test suite | `go test ./... -count=1` |
| Single package | `go test ./adapters/memstore/... -v -count=1` |
| Single test | `go test ./internal/runtime/... -run TestName -v -count=1` |
| Race detector | `go test ./... -race -count=1` |
| Coverage | `go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out` |
| Vet | `go vet ./...` |
| Build examples | `go build ./examples/...` |

## Key Files

| File | Purpose |
|------|---------|
| `flowstep.go` | Package doc, `Define()` entry point |
| `engine.go` | `Engine` struct, `NewEngine()`, public methods |
| `options.go` | Functional options (`With*` helpers) |
| `errors.go` | Sentinel errors |
| `aliases.go` | Type aliases re-exported from `types/` |
| `testutil/engine.go` | `NewTestEngine(t)` — full engine with in-memory adapters + FakeClock |
| `testutil/fixtures.go` | `OrderWorkflow(t)`, `ApprovalWorkflow(t)` — pre-built definitions |
