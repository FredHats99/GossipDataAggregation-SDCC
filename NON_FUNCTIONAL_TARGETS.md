# Step 0 Non-Functional Targets (Draft)

This document captures candidate targets for Step 0 and a recommended baseline to lock before implementation.

## 1) Cluster Size Target

### Problem to Solve

Define the scale envelope the MVP must reliably support so architecture and tests are scoped correctly.

### Options

- **Option A (progressive tiers, recommended):**
  - Dev/local: `3` nodes
  - CI integration: `5` nodes
  - Stretch/perf validation: `10` nodes
- **Option B (single fixed target):**
  - Use only `5` nodes for all environments

### Recommendation

Adopt **Option A**. It gives a clear maturity path and keeps early iteration fast without losing scale direction.

## 2) Convergence Expectation

### Problem to Solve

Set measurable convergence behavior so “eventual consistency” is testable and not ambiguous.

### Options

- **Option A (qualitative only):**
  - eventual convergence, no bounded timing target
- **Option B (eventual + soft SLO, recommended):**
  - eventual convergence required
  - under normal network and no faults:
    - at `5` nodes, `95%` of updates visible cluster-wide within `<= 5s`

### Recommendation

Adopt **Option B**. Keep the soft SLO as an engineering target (not a hard SLA) for local/CI assertions.

## 3) Fault Model

### Problem to Solve

Define which failure classes the MVP must tolerate so design and tests cover the intended resilience scope.

### Options

- **Option A (phased fault model, recommended):**
  1. node crash/stop + restart/rejoin
  2. packet loss/delay (for example 5-10% loss, bounded added latency)
  3. temporary network partition + heal
- **Option B (all faults in first implementation pass):**
  - implement and validate all fault classes immediately

### Recommendation

Adopt **Option A**. It reduces delivery risk while preserving complete roadmap coverage.

## 4) Gossip Baseline Needed to Support Targets

To make the above targets testable, freeze a minimum dissemination policy now:

- mode: `push-pull`
- fanout: `2`
- gossip interval: `500ms` to `1000ms` (start with `1000ms`)
- peer selection: random among `alive` members
- anti-entropy interval: `10s` to `30s` (start with `15s`)

These values are initial defaults, not long-term tuning limits.

## 5) Proposed Step 0 Lock-In

- Cluster size target: `3/5/10` tiered
- Convergence expectation: eventual + soft SLO (`95%` within `<= 5s` at 5 nodes under normal network)
- Fault model: phased (`crash/restart` -> `loss/delay` -> `partition/heal`)
- Gossip baseline: `push-pull`, fanout `2`, gossip `1s`, anti-entropy `15s`
