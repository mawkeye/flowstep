# flowstep Architecture & Coding Standards

## Package Responsibilities

| Package | Purpose | Rule |
|---------|---------|------|
| `flowstep` (root) | Public API contract | No heavy logic. Delegates to `internal`. |
| `types/` | Pure domain structs | **Zero dependencies** on other packages. |
| `internal/runtime/` | State machine execution | Users cannot import. |
| `internal/builder/` | DSL builder | Users cannot import. |
| `internal/graph/` | Validation & compilation | Users cannot import. |
| `adapters/*` | Pluggable implementations | Implement root interfaces. |

## Coding Patterns

**Functional Options:** All constructors use `func NewX(opts ...Option) (*X, error)`.

**Context Propagation:** `context.Context` is the first argument of every interface method (`Store`, `Guard.Check`, `Condition.Evaluate`, `Activity.Execute`).

**Sentinel Errors:** Defined in `errors.go` (root). Internal packages return these (wrapped). Users check with `errors.Is(err, flowstep.ErrGuardFailed)`.

**Interfaces:** Live in root next to the consumer. Single-method interfaces use `-er` suffix.

## Forbidden Patterns

- **God Package:** Do not dump everything in root.
- **Global State:** No `init()` functions or global variables.
- **Panic:** Never panic. Always return `error`.
- **Utility Packages:** No `util` or `common`. Put code where it belongs.

## Testing Strategy

| Type | Package | Location | Focus |
|------|---------|----------|-------|
| Black-box (integration) | `flowstep_test` | Root `*_test.go` | Public API contract |
| White-box (unit) | Same package | `internal/runtime/*_test.go`, `internal/builder/*_test.go` | Internal logic |
| Adapter tests | Same package | `adapters/memstore/*_test.go` | Adapter behavior |

- stdlib `testing` only (no testify). `t.Fatalf` for setup, `t.Errorf` for assertions.
- Table-driven tests with `t.Run()`. Sentinel errors with `errors.Is()`.
- `testutil.NewTestEngine(t)` for integration tests. `testutil.FakeClock` for time control.
