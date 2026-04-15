# Step 0 Aggregation Strategies (MVP Focus)

This document expands Step 0 strategy choices for the two MVP aggregation operations:

- `SUM`
- `TOP-K`

The goal is to make the MVP choices explicit before implementation in later roadmap steps.

## 1) SUM Strategy

### 1.1 Problem Definition

Compute a cluster-wide sum that converges correctly under:

- out-of-order message delivery
- duplicate messages
- retransmissions
- temporary disconnections with later healing

### 1.2 Candidate Strategies

#### Strategy A: CRDT G-Counter (Exact, Recommended for MVP)

Represent state as a map keyed by `node_id`:

- `state[node_id] = max local contribution observed for that node`

Update on local node:

- increase only local component: `state[self] += delta` where `delta >= 0`

Merge with peer:

- element-wise max by node id:
  - `state[i] = max(state[i], peer_state[i])`

Estimate:

- return `sum(state[i])`

Why this works:

- merge is associative
- merge is commutative
- merge is idempotent
- therefore gossip reordering/duplication is safe

Tradeoffs:

- exact and robust
- memory grows with number of node IDs seen
- supports only monotonic increments unless extended

#### Strategy B: Event/Event-log Summation (Not recommended for MVP core)

Send raw update events and deduplicate by event ID.

Tradeoffs:

- intuitive
- dedup + persistence complexity is higher
- replay/order concerns are easier to get wrong than G-Counter merge

### 1.3 MVP Decision for SUM

Use **Strategy A (G-Counter)** as base behavior.

### 1.4 Problem Solved by Selected SUM Strategy

The selected `SUM` strategy solves this concrete problem:

"Given a decentralized gossip network where messages may be delayed, duplicated, reordered, or temporarily dropped, compute an exact cluster-wide non-negative cumulative sum such that all healthy nodes eventually converge to the same value without double-counting updates."

Operationally, this ensures:

- exact final value for increment-only workloads
- safe convergence despite duplicate/replayed gossip traffic
- deterministic reconciliation after partitions and node restarts

### 1.5 MVP Guardrails

- restrict updates to non-negative increments (`delta >= 0`)
- reject unknown/malformed node IDs
- include state version metadata for anti-entropy optimization later

### 1.6 Post-MVP Enhancements

- delta-state gossip to reduce payload size
- tombstone/compaction strategy for permanently removed nodes
- PN-Counter variant if decrement support is required

## 2) TOP-K Strategy

### 2.1 Problem Definition

Maintain a globally convergent top `K` set (largest values) under gossip conditions with deterministic behavior across nodes.

### 2.2 Key Design Questions

- What is the identity of an item (`item_id`)?
- Can a source update an existing item?
- Do ties need deterministic order?
- Is exactness required or is approximation acceptable?

### 2.3 Candidate Strategies

#### Strategy A: Exact Mergeable Candidate Set (Recommended for MVP)

State model:

- bounded map/set of candidates keyed by `item_id`
- each candidate includes:
  - `item_id`
  - `score`
  - `origin_node_id`
  - `event_ts` (or logical version)

Merge:

- union candidates
- resolve same `item_id` via deterministic latest-or-highest rule (must be fixed in spec)
- sort by deterministic comparator
- keep first `K` entries only

Deterministic comparator example:

1. `score` descending
2. `item_id` ascending (or `origin_node_id` ascending)
3. `event_ts` ascending (or fixed logical order)

Why this works:

- every node applies the same merge + trim rule
- same input set yields same top `K` output
- duplicates/reordering are harmless when identity and conflict rules are stable

Tradeoffs:

- exact result for retained candidates
- bounded memory by design
- more careful spec/testing needed than `SUM`

#### Strategy B: Approximate Heavy-Hitter Sketch (Not recommended for MVP core)

Use sketch-based frequent-item approximations.

Tradeoffs:

- lower memory for very large cardinality
- non-exact output
- harder to explain and validate for early milestone

### 2.4 MVP Decision for TOP-K

Use **Strategy A (exact bounded candidate set)** with strict deterministic tie-breaking.

### 2.5 Problem Solved by Selected TOP-K Strategy

The selected `TOP-K` strategy solves this concrete problem:

"Given decentralized nodes that independently observe or receive scored items, maintain the same exact global top `K` ranked items on every node under asynchronous gossip, including duplicate and out-of-order message delivery, while keeping memory bounded."

Operationally, this ensures:

- deterministic global ranking with stable tie resolution
- eventual convergence to the same top `K` across nodes
- bounded resource use through fixed candidate limits

### 2.6 MVP Guardrails

- define a canonical `item_id` format
- require deterministic conflict resolution for same `item_id`
- enforce max payload and candidate count limits per message
- include property tests for merge idempotence/commutativity/associativity at the state level

### 2.7 Post-MVP Enhancements

- compact wire format for candidate deltas
- optional approximation mode for high-cardinality streams
- adaptive `K` and memory budget controls

## 3) Cross-Cutting Validation Plan (For Both)

Minimum test set to validate chosen MVP strategies:

1. **Order invariance:** same updates in different arrival orders converge to same state.
2. **Duplicate tolerance:** duplicate gossip messages do not change final estimate.
3. **Partition healing:** split cluster, apply updates, heal network, verify convergence.
4. **Node restart:** restart a node and verify rejoin convergence behavior.

## 4) Final Step 0 Recommendation

- `SUM`: adopt **G-Counter exact strategy** for MVP.
- `TOP-K`: adopt **exact deterministic bounded candidate strategy** for MVP.

This pairing keeps MVP correctness explicit and testable while leaving room for optimization in later steps.
