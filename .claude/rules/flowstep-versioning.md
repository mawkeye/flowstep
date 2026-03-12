# flowstep Versioning & Documentation

## Core Rules

- NEVER mention `co-authored-by` or the tool used to create commits/PRs.
- All timestamps stored in **UTC** (`TIMESTAMPTZ`). Projected to facility timezone at display only.

## Semantic Versioning

Pre-1.0 development. Current: v0.12.0.

| Bump | When | Example |
|------|------|---------|
| Patch | Bug fixes, internal refactors, test additions | `v0.12.0` -> `v0.12.1` |
| Minor | New features, adapters, interfaces | `v0.12.0` -> `v0.13.0` |
| Major | Breaking API changes (reserved for v1.0.0) | — |

Tag at the end of every feature/task: `git tag -a vX.Y.Z -m "feat: <description>"`

## CHANGELOG.md

Update under `[Unreleased]` with every behavior-changing commit. When tagging, move entries to `[vX.Y.Z] - YYYY-MM-DD`. Format: [Keep a Changelog](https://keepachangelog.com/).

## Documentation Trail

Every step of development must be documented in dedicated files:

- **Implementation steps:** `docs/steps/impl/YYYY-MM-DD-<sequence>-<description>.md` — what was implemented, files modified, decisions, deviations.
- **Plan changes:** `docs/steps/plan/YYYY-MM-DD-<sequence>-<description>.md` — what changed, why, previous vs new plan.

These files form an immutable decision log. Never edit a previous step file — create a new one that references it.
