---
name: flowstep-tdd
description: |
  Enforces strict TDD Red-Green-Refactor for all flowstep Go code changes.
  Use when: (1) implementing any new function, interface, adapter, or engine behavior,
  (2) fixing a bug — write reproducing test FIRST, (3) spec-implement runs a task,
  (4) modifying existing behavior — verify existing tests cover it, add tests if not.
  Triggers on any code change that isn't documentation-only or config-only.
---

# flowstep TDD Workflow

## When to Use

Every code change in flowstep that adds or modifies behavior. This includes:
- New functions, methods, interfaces, types
- Bug fixes (reproduce first)
- Refactoring existing logic
- New adapters or store implementations
- Engine behavior changes

## When NOT to Use

- Documentation-only changes (README, comments, CHANGELOG)
- go.mod / go.sum dependency updates with no code changes
- Configuration files (CLAUDE.md, .gitignore)
- Formatting-only changes (`gofmt`)

## The Cycle

Every feature or fix follows this exact sequence. No shortcuts.

### 1. RED — Write a failing test

Write the **minimum** test that describes the desired behavior. The test MUST fail because the production code doesn't exist yet or doesn't handle the case.

**Rules:**
- One behavior per test. If you're testing two things, write two tests.
- Test behavior, not implementation. Assert on outputs and state changes.
- Name: `TestSubject_Scenario_Expected` (e.g., `TestEngine_TransitionWithGuard_RejectsWhenGuardFails`)
- Use `t.Run()` for subtests in table-driven tests.

**Which test package:**

| What you're testing | Package | File location |
|---------------------|---------|---------------|
| Public API contract (Engine, Define, Transition) | `flowstep_test` (external) | Root `*_test.go` |
| Internal engine logic, private helpers | `engine` (same package) | `internal/runtime/*_test.go` |
| Adapter behavior | Same package (e.g., `memstore`) | `adapters/memstore/*_test.go` |
| Builder validation | `flowstep_test` or `builder` | Root or `internal/builder/*_test.go` |

**Test infrastructure — use what exists:**

```go
// Full engine with all in-memory adapters + FakeClock:
te := testutil.NewTestEngine(t)

// Pre-built workflow definitions:
def := testutil.OrderWorkflow(t)     // CREATED -> PROCESSING -> DONE
def := testutil.ApprovalWorkflow(t)  // With wait state + task

// Register and use:
te.Engine.Register(ctx, def)
```

**For white-box engine tests** (`internal/runtime/`), use minimal stubs (not testutil):

```go
// Stub interfaces inline — only implement what the test needs:
type noopEventStore struct{}
func (n *noopEventStore) Append(_ context.Context, _ any, _ types.DomainEvent) error { return nil }
// ... only stub methods the test actually calls
```

### 2. VERIFY RED — Confirm the test fails correctly

```bash
go test ./path/to/package -run TestName -v
```

**Check:**
- Test fails (exit code 1)
- Failure is because the feature doesn't exist (not a syntax error, import error, or nil pointer)
- The error message is meaningful (will help debug if the test regresses later)

If the test passes → it's not testing new behavior. Rewrite it.
If it fails for the wrong reason → fix the test setup, not the production code.

### 3. GREEN — Write the minimum code to pass

**Rules:**
- Write the simplest code that makes the test pass. Nothing more.
- Hardcoding is acceptable if only one test case exists.
- Do NOT refactor during this step.
- Do NOT add error handling for cases no test requires yet.
- Do NOT add functionality beyond what the test demands.

### 4. VERIFY GREEN — All tests pass

```bash
go test ./... -count=1
```

**Critical:** Run the FULL suite, not just the file you touched. flowstep has cross-package dependencies (engine ↔ builder ↔ stores). A change in `internal/runtime/` can break `integration_test.go`.

**Check:**
- Exit code 0
- Zero failures
- No race warnings (run with `-race` for concurrent code)

### 5. REFACTOR — Improve without changing behavior

**Rules:**
- Tests must stay green after every refactoring step.
- Extract common setup into test helpers (use `t.Helper()`).
- Remove duplication in production code.
- Improve names, split functions if > 50 lines.
- Do NOT add new behavior during refactoring.

After refactoring, run full suite again:

```bash
go test ./... -count=1
```

### 6. REPEAT — Next behavior

Go back to step 1 for the next behavior. Each cycle should take 5-15 minutes. If you're spending longer, the step is too big — break it down.

## Assertion Patterns

flowstep uses stdlib `testing` (no testify). Follow existing conventions:

```go
// Fatal for setup failures (test can't continue):
if err != nil {
    t.Fatalf("setup failed: %v", err)
}

// Error for assertions (test continues, reports all failures):
if got.CurrentState != "PROCESSING" {
    t.Errorf("expected PROCESSING, got %s", got.CurrentState)
}

// Sentinel error checking:
if !errors.Is(err, flowstep.ErrInstanceNotFound) {
    t.Errorf("expected ErrInstanceNotFound, got %v", err)
}
```

## Table-Driven Tests

Use for testing multiple inputs against the same behavior:

```go
func TestEngine_Transition_Validation(t *testing.T) {
    tests := []struct {
        name      string
        from      string
        to        string
        wantErr   error
    }{
        {"valid transition", "CREATED", "PROCESSING", nil},
        {"invalid from state", "NONEXISTENT", "PROCESSING", flowstep.ErrInvalidTransition},
        {"already terminal", "DONE", "PROCESSING", flowstep.ErrAlreadyTerminal},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // ... setup and assert
        })
    }
}
```

## Test Organization Rules

| Rule | Rationale |
|------|-----------|
| No global state | Tests must be independent — run in any order |
| `context.Background()` for all ctx | Never use `context.TODO()` in tests |
| `t.Helper()` on all test helpers | Error messages point to the caller, not the helper |
| `t.Parallel()` where safe | Adapter tests with shared state: no. Pure logic: yes. |
| Clean up resources | Use `t.Cleanup()` for teardown, not `defer` in subtests |

## Bug Fix Protocol

1. **Write a test that reproduces the bug** (RED)
2. **Verify it fails** with the expected symptom
3. **Fix the bug** — minimum change (GREEN)
4. **Verify the fix** — the reproducing test passes
5. **Run full suite** — no regressions
6. **Add edge case tests** if the bug suggests gaps

## Verification Commands

```bash
# Single test:
go test ./path/to/package -run TestName -v -count=1

# Full suite:
go test ./... -count=1

# With race detector (for concurrent code):
go test ./... -race -count=1

# Coverage:
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out

# Specific adapter:
go test ./adapters/memstore/... -v -count=1

# Internal engine only:
go test ./internal/runtime/... -v -count=1
```

`-count=1` disables test caching. Always use it during TDD to ensure fresh runs.

## Completion Checklist

- [ ] Every new/changed function has a test
- [ ] Tests follow naming convention: `TestSubject_Scenario_Expected`
- [ ] White-box tests in same package, black-box in `_test` package
- [ ] `go test ./... -count=1` passes with 0 failures
- [ ] `go vet ./...` clean
- [ ] No test relies on timing (use `testutil.FakeClock`, not `time.Sleep`)
- [ ] Sentinel errors tested with `errors.Is()`

## References

- `testutil/engine.go` — TestEngine factory with all in-memory adapters
- `testutil/fixtures.go` — Pre-built workflow definitions (OrderWorkflow, ApprovalWorkflow)
- `internal/runtime/engine_test.go` — White-box engine tests with inline stubs
- `integration_test.go` — Black-box integration tests
- `adapters/memstore/*_test.go` — Adapter test patterns
