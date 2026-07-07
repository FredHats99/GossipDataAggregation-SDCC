package transport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type JSONCodec struct{}

func NewJSONCodec() *JSONCodec {
	return &JSONCodec{}
}

type jsonEnvelope struct {
	Type          string          `json:"type"`
	Seq           uint64          `json:"seq"`
	Timestamp     string          `json:"timestamp"`
	Checksum      string          `json:"checksum"`
	From          string          `json:"from"`
	TraceID       string          `json:"trace_id,omitempty"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	Version       string          `json:"version,omitempty"`
	Payload       json.RawMessage `json:"payload"`
}

func (c *JSONCodec) Encode(message Envelope) ([]byte, error) {
	if message.Version == "" {
		message.Version = "v1"
	}
	if message.Checksum == "" {
		message.Checksum = checksumForPayload(message.Payload)
	}
	if err := validateEnvelope(message); err != nil {
		return nil, err
	}

	frame := jsonEnvelope{
		Type:          message.Type,
		Seq:           message.Seq,
		Timestamp:     message.Timestamp,
		Checksum:      message.Checksum,
		From:          message.From,
		TraceID:       message.TraceID,
		CorrelationID: message.CorrelationID,
		Version:       message.Version,
		Payload:       append([]byte(nil), message.Payload...),
	}
	return json.Marshal(frame)
}

func (c *JSONCodec) Decode(frame []byte) (Envelope, error) {
	var wire jsonEnvelope
	if err := json.Unmarshal(frame, &wire); err != nil {
		return Envelope{}, err
	}

	message := Envelope{
		Type:          wire.Type,
		Seq:           wire.Seq,
		Timestamp:     wire.Timestamp,
		Checksum:      wire.Checksum,
		From:          wire.From,
		TraceID:       wire.TraceID,
		CorrelationID: wire.CorrelationID,
		Version:       wire.Version,
		Payload:       append([]byte(nil), wire.Payload...),
	}
	if message.Version == "" {
		message.Version = "v1"
	}
	if err := validateEnvelope(message); err != nil {
		return Envelope{}, err
	}
	if checksumForPayload(message.Payload) != message.Checksum {
		return Envelope{}, ErrChecksumMismatch
	}
	return message, nil
}

func checksumForPayload(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}
