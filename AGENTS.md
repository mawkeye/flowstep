# flowstep Implementation and Development Guidelines

> **Context:** This file guides the implementation of the `flowstep` Go library. It enforces Clean Architecture, strictly separate types, and idiomatic Go patterns. This document contains critical information about working with this codebase.

## 0. Core Development Rules

- NEVER ever mention a `co-authored-by` or similar aspects. In particular, never mention the tool used to create the commit message or PR.
- All timestamps stored in **UTC** (`TIMESTAMPTZ`). Projected to facility timezone at display only.

### Documentation Trail

Every step of development must be documented in dedicated files:

- **Implementation steps:** Each implementation step (feature, bugfix, refactor, migration) must be documented in a separate file under `docs/steps/impl/`. Naming convention: `YYYY-MM-DD-<sequence>-<short-description>.md`. Each file records what was implemented, which files were created or modified, decisions made during implementation, and any deviations from the plan.
- **Plan changes:** Every change to the architecture, design, or implementation plan must be documented in a separate file under `docs/steps/plan/`. Naming convention: `YYYY-MM-DD-<sequence>-<short-description>.md`. Each file records what changed, why it changed, what the previous plan was, and what the new plan is.

These files form an immutable decision log. Never edit a previous step file -- create a new one that references it instead.

## 1. Architectural Blueprint
We are strictly following a **Library Layout** with **Clean Architecture** principles.

### Package Structure
* **`flowstep` (Root)**
    * **Purpose:** The Public API Contract.
    * **Contents:**
        * Interfaces (`Store`, `EventBus`, `Clock`, `Hooks`).
        * Public Entry Points (`NewEngine(...)`, `Define(...)`).
        * Configuration (`Options`, `With...` helpers).
        * Sentinel Errors (`ErrWorkflowNotFound`, `ErrGuardFailed`).
    * **Rule:** No heavy implementation logic here. Delegates to `internal`.

* **`flowstep/types`** (New Requirement)
    * **Purpose:** Pure Domain Data Structures.
    * **Contents:** `DomainEvent`, `WorkflowInstance`, `PendingTask`, `ActivityInvocation`, `State`, `Transition`.
    * **Rule:** **Zero dependencies** on other packages. Pure structs only. Used by both Root and Internal to prevent circular imports.

* **`flowstep/internal`**
    * **Purpose:** The Business Logic (Hidden).
    * **Contents:**
        * `internal/engine`: The state machine execution logic.
        * `internal/builder`: The DSL builder implementation.
        * `internal/graph`: Validation and reachability analysis.
    * **Rule:** Users cannot import this.

* **`flowstep/adapters`** (Refined from 'Reference Implementations')
    * **Purpose:** Concrete implementations of interfaces.
    * **Contents:** `memstore`, `pgxstore`, `redisbus`, etc.

## 2. Coding Standards & Patterns

### A. Functional Options Pattern
* **Requirement:** All constructors (especially `NewEngine`) must use the Functional Options pattern.
* **Example:**
    ```go
    func NewEngine(opts ...Option) (*Engine, error)
    func WithEventStore(s Store) Option
    ```

### B. Context Propagation
* **Requirement:** `context.Context` must be the **first argument** of EVERY interface method, specifically:
    * `Store` methods (DB access)
    * `Guard.Check(ctx, ...)`
    * `Condition.Evaluate(ctx, ...)`
    * `Activity.Execute(ctx, ...)`
    * `Clock.Now()` -> Change to `Now(ctx)` if distributed time is ever needed (keep simple for now, but `ctx` ready).

### C. Error Handling
* **Requirement:** Define sentinel errors in `errors.go` (Root).
* **Usage:** Internal packages must return these errors (wrapped if necessary) so users can check `errors.Is(err, flowstep.ErrGuardFailed)`.

### D. Interface Definition
* **Location:** Interfaces live in `flowstep` (Root) next to the consumer (the API).
* **Naming:** Single-method interfaces named with `-er` suffix where possible (Go convention), but descriptive names (`EventStore`) are accepted for complex types.

## 3. Testing Strategy

### A. Black Box (Integration)
* **Location:** `flowstep_test` package (external to root).
* **Focus:** Tests `NewEngine`, `Define`, and `Transition` end-to-end.
* **Goal:** Verify the contract.

### B. White Box (Unit)
* **Location:** Inside `internal/engine`, `internal/builder`.
* **Focus:** Tests state transition logic, graph validation edge cases, and private helper functions.
* **Goal:** Verify correctness of the complex internal machinery.

## 4. Implementation Phase Adjustments
* **Phase 1 (Setup):** You must create `types/` folder immediately.
* **Phase 2 (Builder):** Move builder logic to `internal/builder` but keep `Define()` in root as a wrapper.
* **Phase 3 (Engine):** Move engine logic to `internal/engine`.

## 5. Forbidden Patterns
* ❌ **God Package:** Do not dump everything in root.
* ❌ **Global State:** No `init()` functions or global variables.
* ❌ **Panic:** Never panic. Always return `error`.
* ❌ **Utility Packages:** Avoid `util` or `common`. Put code where it belongs.

## 6. Versioning & Release Strategy
We strictly follow **Semantic Versioning (SemVer) 2.0.0**.

### A. Lifecycle
* **Initial Version:** `v0.1.0` (Alpha/Development).
* **1.0.0 Milestone:** Released only after **Phase 22 (Integration Tests)** is complete and the API is stable.

### B. Workflow
1.  **Track Changes:** Update `CHANGELOG.md` with every significant PR or Phase completion.
2.  **Tag Frequently:** Tag releases at the end of every **Phase**.
    * *Command:* `git tag -a v0.X.Y -m "feat: completed phase X"`
3.  **Versioning Rules:**
    * **Patch (`0.1.1`):** Bug fixes, internal refactors.
    * **Minor (`0.2.0`):** New features (e.g., adding a new Store or Bus).
    * **Major (`1.0.0`):** Breaking API changes (reserved for final release).