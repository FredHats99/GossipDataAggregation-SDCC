# Step 0 Coding Standards (Draft)

This document defines candidate coding standards for Step 0 and a recommended default set.

## 1) Go Version

### Problem to Solve

Pin a stable toolchain so local development, CI, and Docker builds behave consistently.

### Options

- Option A: pin a specific minor/patch version (recommended)
- Option B: float to latest available Go release

### Recommendation

- Use **Option A** and pin to a single version across local/CI/container.
- Suggested baseline: `go 1.24.x` (or latest stable available in your pipeline at lock-in time).

## 2) Lint and Test Tooling

### Problem to Solve

Enforce code quality and regression safety with deterministic automated checks.

### Options

- Option A: minimal checks only (`go test`, `go vet`)
- Option B: full baseline quality gate (recommended)

### Recommendation (Option B)

Use this baseline in CI:

1. `go test ./...`
2. `go test -race ./...` (where supported)
3. `go vet ./...`
4. `golangci-lint run`

Enforcement rollout:

- Phase 1: lint in warning mode locally, failing in CI for critical issues.
- Phase 2: fail CI on all configured lint rules after cleanup.

## 3) Logging and Metrics Conventions

### Problem to Solve

Standardize observability so gossip behavior, failures, and convergence can be diagnosed quickly.

### Logging Recommendation

- Structured JSON logs by default.
- Required fields:
  - `ts`
  - `level`
  - `msg`
  - `node_id`
  - `peer` (when applicable)
  - `msg_type` (when applicable)
  - `seq` (when applicable)
  - `trace_id` or `correlation_id` (when available)
- Log level defaults:
  - local/dev: `debug` or `info`
  - CI/prod-like tests: `info`

### Metrics Recommendation

Expose low-cardinality counters/gauges/histograms:

- gossip send/receive counts
- merge success/failure counts
- convergence lag estimate
- queue depth and drop counts
- anti-entropy round counts and durations

## 4) Proposed Step 0 Lock-In

- Go version policy: pinned single version (`1.24.x` target)
- Quality gate: `go test`, `race`, `vet`, `golangci-lint`
- Logging: structured JSON with required gossip context fields
- Metrics: baseline convergence + transport + queue telemetry
