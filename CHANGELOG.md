# Changelog

All notable changes to this project will be documented in this file.
Format based on [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

## [v0.4.2] - 2026-03-10

### Changed
- Task 1: Decomposed `internal/engine/engine.go` (895 lines) into `internal/runtime/` sublayers — `runtime.go` (coordinator), `executor.go` (transition pipeline), `dispatcher.go` (post-commit), `scheduler.go` (trigger matching), `helpers.go` (utilities) (b9b842e)
- All files remain under 300 lines; internal test suite preserved across 4 test files
- No API or behavior changes — root `flowstep.Engine` interface unchanged

## [v0.4.1] - 2026-03-10

### Changed
- Task 0: Consolidated 9 root type alias forwarding files (`activityrunner.go`, `activitystore.go`, `childstore.go`, `eventbus.go`, `clock.go`, `txprovider.go`, `hooks.go`, `taskstore.go`, `store.go`) into a single `aliases.go` (d964b6f)
- Root package reduced from 14 to 6 production files
- No API or behavior changes — all exported symbols preserved as type aliases

## [v0.4.0] - 2026-03-10

### Changed
- README update and workflow examples

## [v0.3.0] - 2026-03-10

### Changed
- Codebase refactoring

## [v0.2.0] - 2026-03-10

### Fixed
- Project review and critical-to-medium fixes implementation

## [v0.1.0] - 2026-03-10

### Added
- flowstep first implementation
