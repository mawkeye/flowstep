---
name: go-testing
description: |
  Handles Golang testing tasks: running tests, writing new tests, fixing failures.
  Use when: running go test commands, debugging test failures, or checking coverage.
  For TDD workflow (write test first), use the flowstate-tdd skill instead.
---

# Go Testing Skill

Quick reference for running and managing Go tests in flowstep.

## When to Use

- Running test suites or individual tests
- Debugging and fixing failing tests
- Checking test coverage
- Understanding test output

For the full TDD workflow (Red-Green-Refactor cycle), use the `flowstate-tdd` skill.

## Commands

```bash
# Full suite (always use -count=1 to bypass cache):
go test ./... -count=1

# Single package:
go test ./adapters/memstore/... -v -count=1

# Single test:
go test ./internal/runtime/... -run TestEngine_Transition -v -count=1

# Race detector:
go test ./... -race -count=1

# Coverage report:
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out
```

## Testing Conventions

- **stdlib testing only** — no testify/require. Use `t.Fatalf` for fatal, `t.Errorf` for assertions.
- **Table-driven tests** with `t.Run()` for multiple cases.
- **`t.Helper()`** on all helper functions.
- **Sentinel errors** checked with `errors.Is()`.
- **White-box** tests in same package. **Black-box** tests in `_test` package suffix.
- **`testutil.NewTestEngine(t)`** for full engine with in-memory adapters.
- **`testutil.FakeClock`** instead of `time.Sleep` or real time.
