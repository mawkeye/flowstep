---
name: flowstep-spec-tasks
description: |
  Enforces task file separation and release discipline during /spec workflows.
  Use when: (1) spec-plan creates implementation tasks — split each into its own
  file in docs/plans/tasks/, (2) spec-implement completes a task — commit, update
  CHANGELOG.md, and tag with semver. Triggers automatically during /spec phases.
---

# Spec Task Separation & Release Discipline

## When to Use

- **During `spec-plan` (Step 1.5-1.6):** When creating implementation tasks for a plan
- **During `spec-implement`:** After completing each task's implementation

## Part 1: Task File Separation (spec-plan)

When a plan has implementation tasks, **always** split each task into its own file.

### Structure

```
docs/plans/YYYY-MM-DD-<plan-slug>.md          # Main plan (references only)
docs/plans/tasks/task-NN-<short-name>.md       # One file per task
```

### Main Plan — Implementation Tasks Section

Replace inline task descriptions with a reference table:

```markdown
## Implementation Tasks

Each task has its own detailed plan file in [`docs/plans/tasks/`](tasks/).
This prevents context bloat — feed only the relevant task plan when working on a specific feature.

| Task | Plan File | Phase | Complexity |
|------|-----------|-------|------------|
| Task 0: Name | [task-00-name.md](tasks/task-00-name.md) | 0 | LOW |
| Task 1: Name | [task-01-name.md](tasks/task-01-name.md) | 1 | HIGH |
```

### Individual Task File Template

```markdown
# Task N: [Name]

Parent Plan: [plan-name](../YYYY-MM-DD-plan-slug.md)
Status: PENDING
Phase: N

## Objective

[1-2 sentences]

## Dependencies

[None | Task X (reason)]

## Scope

- [Bullet points covering what this task implements]
- [Include architectural gotchas and anti-failure safeguards]
- [Reference specific files with line numbers where relevant]

## Definition of Done

- [ ] Detailed implementation plan approved
- [ ] [Verifiable criteria specific to this task]
```

### Rules

- **File naming:** `task-NN-<2-4-word-slug>.md` — zero-padded task number, lowercase hyphens
- **Self-contained:** Each task file has enough context to be understood without the main plan
- **Parent reference:** Always link back to the main plan
- **Status tracking:** `PENDING` | `IN_PROGRESS` | `COMPLETE`
- **No duplication:** Main plan has the reference table only, not inline task content

## Part 2: Release Discipline (spec-implement)

After completing each task (or significant subtask), follow this commit-document-tag cycle.

### Per-Task Completion Checklist

1. **Commit** the implementation with a conventional commit message:
   ```
   feat(task-N): <short description>

   - What was implemented
   - Key decisions made
   - Files created/modified
   ```

2. **Update CHANGELOG.md** in the project root. Create it if it doesn't exist:
   ```markdown
   # Changelog

   All notable changes to this project will be documented in this file.
   Format based on [Keep a Changelog](https://keepachangelog.com/).

   ## [Unreleased]

   ## [vX.Y.Z] - YYYY-MM-DD

   ### Added
   - Task N: <what was added> (#PR or commit ref)

   ### Changed
   - <what changed>

   ### Fixed
   - <what was fixed>
   ```

3. **Tag with semver** following the project's versioning rules:
   ```bash
   # Determine version bump:
   # - Patch (0.X.Y+1): bug fixes, internal refactors, test additions
   # - Minor (0.X+1.0): new features, new adapters, new interfaces
   # - Major: reserved for 1.0.0 (stable API release)

   git tag -a vX.Y.Z -m "feat: Task N — <description>"
   ```

4. **Update task file status** from `PENDING` to `COMPLETE`

5. **Update main plan** progress tracking: `[ ]` → `[x]`, increment completed count

### Version Bump Rules

| Change Type | Bump | Example |
|-------------|------|---------|
| Bug fix, internal refactor | Patch | `v0.3.1` → `v0.3.2` |
| New feature, new interface | Minor | `v0.3.2` → `v0.4.0` |
| Breaking API change | Major | Reserved for `v1.0.0` |
| Infrastructure/tooling only | Patch | `v0.3.1` → `v0.3.2` |

### Documentation Trail

Each implementation step must also be documented per CLAUDE.md rules:
- `docs/steps/impl/YYYY-MM-DD-<seq>-<description>.md` for implementation steps
- `docs/steps/plan/YYYY-MM-DD-<seq>-<description>.md` for plan changes

## Verification

- [ ] Main plan has reference table (no inline tasks)
- [ ] Each task has its own file in `docs/plans/tasks/`
- [ ] Task files link back to parent plan
- [ ] CHANGELOG.md exists and is updated after each task
- [ ] Git tag exists for each completed task
- [ ] Task status updated in both task file and main plan

## When NOT to Use

- Plans with only 1-2 trivial tasks (inline is fine for small plans)
- Documentation-only changes (no CHANGELOG entry needed)
- When the user explicitly asks to keep tasks inline

## Example

**Session context:** Created a 17-task feature roadmap comparing flowstep with statekit. Initially all tasks were inline in the main plan (~450 lines). Split into 17 individual files, reducing the main plan from ~960 to ~460 lines. This enabled feeding only the relevant task context during implementation instead of the entire plan.

**Result:** `docs/plans/tasks/task-00-directory-structure-refinement.md` through `task-16-deterministic-side-effects.md`, each self-contained with objective, dependencies, scope, and definition of done.
