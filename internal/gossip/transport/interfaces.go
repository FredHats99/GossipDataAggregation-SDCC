package transport

import "context"

// Envelope is the protocol-level wrapper exchanged between nodes.
//
// It carries routing and integrity metadata plus the opaque payload bytes.
// Concrete field-level validation (for example checksum format or timestamp
// sanity) belongs to codec/validation implementations that process this type.
type Envelope struct {
	Type      string
	Seq       uint64
	Timestamp string
	From      string
	Checksum  string
	TraceID   string
	CorrelationID string
	Version   string
	Payload   []byte
}

// Sender emits envelopes towards a peer endpoint.
//
// Expected behavior:
// - Respect context cancellation/deadlines.
// - Return an error only when delivery was not accepted by the transport.
// - Keep retries/backoff policy outside the interface so callers can compose
//   reliability behavior explicitly.
type Sender interface {
	Send(ctx context.Context, peer string, message Envelope) error
}

// Receiver exposes a stream of validated incoming envelopes.
//
// Next blocks until one of the following happens:
// - a message is available;
// - the receiver is closed;
// - the context is canceled.
//
// Implementations should avoid returning malformed envelopes; invalid data
// should be filtered before entering this contract when possible.
type Receiver interface {
	Next(ctx context.Context) (Envelope, error)
	Close() error
}

// Codec serializes and deserializes envelopes for wire exchange.
//
// Encode must produce deterministic bytes for the same input envelope.
// Decode must reject malformed or oversized frames with an error.
// Validation hooks (checksum, max size, required fields) can be implemented
// inside Decode or by a dedicated decorator.
type Codec interface {
	Encode(message Envelope) ([]byte, error)
	Decode(frame []byte) (Envelope, error)
}

// FrameSender emits raw wire frames to a peer endpoint.
//
// It is intentionally byte-oriented so higher layers can plug their own codec
// without coupling network delivery to a specific serialization format.
type FrameSender interface {
	SendFrame(ctx context.Context, peer string, frame []byte) error
}

// FrameReceiver exposes a stream of raw frames received from peers.
//
// The peer string represents the network endpoint the frame came from.
// Implementations should return promptly when the context is canceled.
type FrameReceiver interface {
	NextFrame(ctx context.Context) (peer string, frame []byte, err error)
	Close() error
}
