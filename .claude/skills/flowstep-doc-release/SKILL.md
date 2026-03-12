---
name: flowstep-doc-release
description: |
  Ensures project documentation is updated after milestones. Use when:
  (1) a task implementation is complete and ready to commit,
  (2) a git version tag is about to be created,
  (3) a plan status changes to COMPLETE or VERIFIED,
  (4) new public API surfaces are added (interfaces, types, functions).
  Triggers at completion boundaries — not during active development.
---

# Documentation & Release Checklist

## When to Use

This skill activates at **completion boundaries** — the moment between "code is done" and "commit/tag/push":

- After implementing a task (before committing)
- After all tasks in a plan are done (before marking COMPLETE)
- When creating a semver tag
- When new public API is added to the root package

## Documentation Artifacts

flowstep maintains these documentation files. Each has specific update triggers.

### 1. CHANGELOG.md (root)

**Update when:** Any commit that changes behavior (features, fixes, refactors with user impact).
**Skip when:** Documentation-only, test-only, formatting-only changes.

**Format:** [Keep a Changelog](https://keepachangelog.com/)

```markdown
# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

### Added
- <new feature or capability>

### Changed
- <behavior change to existing feature>

### Fixed
- <bug fix>

### Removed
- <removed feature>

## [v0.X.Y] - YYYY-MM-DD

### Added
- <description> (Task N)
```

**Rules:**
- New entries go under `## [Unreleased]` during development
- When tagging a release, move `[Unreleased]` entries to `## [vX.Y.Z] - YYYY-MM-DD`
- One bullet per logical change (not per file)
- Reference task number if from a plan: `(Task 3: Hierarchical States)`
- Keep descriptions user-facing: what changed, not how

### 2. README.md (root)

**Update when:**
- New public feature added (new section or update existing section)
- New adapter added (add to Adapters table)
- API signature changes (update code examples)
- New example program added (add to Examples section)

**Skip when:** Internal refactoring, test changes, infrastructure changes.

**Existing sections to maintain:**

| Section | Update Trigger |
|---------|---------------|
| Features | New capability bullet point |
| Core Concepts | New concept (hierarchy, parallel, history) |
| Engine Configuration | New `With*` option |
| Adapters > Storage/Bus/Runner | New adapter package |
| Mermaid Diagrams | Diagram format changes |
| Testing | New test utilities or fixtures |
| Error Handling > Sentinel Errors | New `Err*` sentinel |
| Examples | New `examples/NN-*` directory |

### 3. docs/steps/impl/ (implementation steps)

**Update when:** Every implementation step per CLAUDE.md rules.
**File:** `docs/steps/impl/YYYY-MM-DD-<seq>-<description>.md`

```markdown
# Implementation Step: <description>

Date: YYYY-MM-DD
Task: <Task N from plan, or "standalone">
Files Modified: <list>

## What Changed
<description>

## Decisions Made
<any non-obvious choices>

## Deviations from Plan
<if any, reference plan file>
```

### 4. docs/steps/plan/ (plan changes)

**Update when:** Plan scope, dependencies, or approach changes during implementation.
**File:** `docs/steps/plan/YYYY-MM-DD-<seq>-<description>.md`

### 5. Task Plan Files (docs/plans/tasks/)

**Update when:** Task status changes.
**Fields:** `Status: PENDING` → `IN_PROGRESS` → `COMPLETE`

### 6. Main Plan File (docs/plans/)

**Update when:** Task completes — check off in Progress Tracking, update counts.

## Semver Tagging Protocol

**When to tag:** After committing a task implementation (or batch of related tasks).

```bash
# 1. Check current version
git tag --list 'v*' | sort -V | tail -1

# 2. Determine bump (per CLAUDE.md Section 6):
#    Patch: bug fix, internal refactor, test addition
#    Minor: new feature, new adapter, new interface
#    Major: reserved for v1.0.0

# 3. Update CHANGELOG — move [Unreleased] to [vX.Y.Z]

# 4. Commit the CHANGELOG update
git add CHANGELOG.md
git commit -m "chore: release vX.Y.Z"

# 5. Tag
git tag -a vX.Y.Z -m "feat: <summary of what's in this release>"
```

**Tag message convention:**
- Single task: `"feat: Task N — <description>"`
- Multiple tasks: `"feat: Tasks N-M — <summary>"`
- Bug fix: `"fix: <what was fixed>"`

## Completion Checklists

### After Implementing a Task

- [ ] `CHANGELOG.md` updated under `[Unreleased]`
- [ ] `docs/steps/impl/` step file created
- [ ] Task file status → `COMPLETE`
- [ ] Main plan progress tracking updated (`[ ]` → `[x]`, counts)
- [ ] README.md updated (if new public API or feature)

### After Tagging a Release

- [ ] `CHANGELOG.md` `[Unreleased]` entries moved to `[vX.Y.Z] - date`
- [ ] Tag message follows convention
- [ ] No uncommitted changes at tag point

### After Completing a Plan

- [ ] All task files marked `COMPLETE`
- [ ] Main plan status → `COMPLETE`
- [ ] README.md reflects all new features from the plan
- [ ] CHANGELOG.md has entries for all implemented tasks
- [ ] Final release tag created

## Verification

```bash
# CHANGELOG has content (not empty):
test -s CHANGELOG.md && echo "OK" || echo "CHANGELOG.md is empty"

# README mentions new features (spot check):
grep -c "feature_keyword" README.md

# All tasks marked complete in plan:
grep -c '\[x\]' docs/plans/YYYY-MM-DD-*.md

# Latest tag matches CHANGELOG:
git tag --list 'v*' | sort -V | tail -1
grep -m1 '## \[v' CHANGELOG.md
```

## When NOT to Use

- During active development (mid-task, between TDD cycles)
- For documentation-only commits (no CHANGELOG entry needed)
- For test-only changes with no behavior impact
- When the user explicitly says "skip docs" or "I'll update docs later"
