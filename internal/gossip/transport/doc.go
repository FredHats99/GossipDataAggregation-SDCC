// Package transport defines contracts for gossip message exchange.
//
// The package is intentionally interface-first:
// implementations (HTTP, UDP, in-memory, etc.) must satisfy these contracts
// without leaking transport-specific concerns into higher layers.
package transport
