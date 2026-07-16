package transport

import (
	"errors"
	"testing"
	"time"
)

func TestJSONCodecRoundTrip(t *testing.T) {
	codec := NewJSONCodec()
	msg := Envelope{
		Type:          "StateDelta",
		Seq:           42,
		Timestamp:     time.Now().UTC().Format(time.RFC3339Nano),
		From:          "node1",
		TraceID:       "tr_1",
		CorrelationID: "corr_1",
		Payload:       []byte(`{"aggregate_type":"SUM","delta_version":5}`),
	}

	frame, err := codec.Encode(msg)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	got, err := codec.Decode(frame)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if got.Version != "v1" {
		t.Fatalf("expected version v1, got %q", got.Version)
	}
	if got.Type != msg.Type || got.From != msg.From || got.Seq != msg.Seq {
		t.Fatalf("unexpected decoded envelope: %+v", got)
	}
	if string(got.Payload) != string(msg.Payload) {
		t.Fatalf("payload mismatch: got %s want %s", string(got.Payload), string(msg.Payload))
	}
}

func TestJSONCodecRejectsChecksumMismatch(t *testing.T) {
	codec := NewJSONCodec()
	msg := Envelope{
		Type:      "Ping",
		Seq:       1,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		From:      "node1",
		Payload:   []byte(`{"node_id":"node1","incarnation":1}`),
		Checksum:  "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Version:   "v1",
	}

	frame, err := codec.Encode(msg)
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	_, err = codec.Decode(frame)
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
}

func TestJSONCodecRejectsInvalidFrom(t *testing.T) {
	codec := NewJSONCodec()
	msg := Envelope{
		Type:      "Ping",
		Seq:       1,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		From:      "Node_1",
		Payload:   []byte(`{"node_id":"Node_1","incarnation":1}`),
	}

	_, err := codec.Encode(msg)
	if !errors.Is(err, ErrInvalidFrom) {
		t.Fatalf("expected invalid from, got %v", err)
	}
}

func TestJSONCodecAcceptsDeltaRangeMessageTypes(t *testing.T) {
	codec := NewJSONCodec()
	for _, messageType := range []string{"DeltaRangeReq", "DeltaRangeResp"} {
		t.Run(messageType, func(t *testing.T) {
			message := Envelope{
				Type:      messageType,
				Seq:       1,
				Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
				From:      "node1",
				Payload:   []byte(`{"test":true}`),
			}
			frame, err := codec.Encode(message)
			if err != nil {
				t.Fatalf("encode %s: %v", messageType, err)
			}
			decoded, err := codec.Decode(frame)
			if err != nil {
				t.Fatalf("decode %s: %v", messageType, err)
			}
			if decoded.Type != messageType {
				t.Fatalf("unexpected decoded type: %s", decoded.Type)
			}
		})
	}
}
