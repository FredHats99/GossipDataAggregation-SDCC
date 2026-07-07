package delta

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"gossipdataaggregation-sdcc/internal/aggregation/common"
	"gossipdataaggregation-sdcc/internal/aggregation/pipeline"
	"gossipdataaggregation-sdcc/internal/gossip/protocol"
	"gossipdataaggregation-sdcc/internal/gossip/transport"
)

type recordingSender struct {
	mu   sync.Mutex
	sent []sentMessage
}

type sentMessage struct {
	peer    string
	message transport.Envelope
}

func (s *recordingSender) Send(_ context.Context, peer string, message transport.Envelope) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, sentMessage{peer: peer, message: message})
	return nil
}

func (s *recordingSender) snapshot() []sentMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sentMessage(nil), s.sent...)
}

func TestRuntimeSendsLocalOutboundDeltas(t *testing.T) {
	manager, err := pipeline.New(pipeline.Config{
		NodeID:            "node1",
		TopKMax:           3,
		OutboundQueueSize: 4,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	sender := &recordingSender{}
	runtime, err := NewRuntime(Config{
		NodeID:       "node1",
		SelfEndpoint: "node1:7000",
		Peers:        []string{"node1:7000", "node2:7000", "node3:7000"},
		Fanout:       2,
		SendTimeout:  time.Second,
		Logger:       slog.Default(),
	}, manager, sender)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}

	if _, advanced, err := manager.ApplyLocalUpdate(pipeline.LocalUpdate{
		AggregateType: common.AggregateSUM,
		Value:         uint64(5),
	}); err != nil || !advanced {
		t.Fatalf("apply local update advanced=%v err=%v", advanced, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runtime.Start(ctx)
	waitFor(t, func() bool { return len(sender.snapshot()) == 2 })

	for _, sent := range sender.snapshot() {
		if sent.peer == "node1:7000" {
			t.Fatalf("runtime sent local delta to self peer")
		}
		if sent.message.Type != messageTypeStateDelta {
			t.Fatalf("unexpected message type: %s", sent.message.Type)
		}
		var stateDelta protocol.StateDelta
		if err := json.Unmarshal(sent.message.Payload, &stateDelta); err != nil {
			t.Fatalf("decode sent state delta: %v", err)
		}
		if stateDelta.AggregateType != common.AggregateSUM {
			t.Fatalf("unexpected aggregate type: %s", stateDelta.AggregateType)
		}
	}
}

func TestRuntimeForwardsOnlyAdvancingReceivedDeltas(t *testing.T) {
	manager, err := pipeline.New(pipeline.Config{
		NodeID:            "node2",
		TopKMax:           3,
		OutboundQueueSize: 4,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	sender := &recordingSender{}
	runtime, err := NewRuntime(Config{
		NodeID:       "node2",
		SelfEndpoint: "node2:7000",
		Peers:        []string{"node1:7000", "node2:7000", "node3:7000"},
		Fanout:       2,
		SendTimeout:  time.Second,
		Logger:       slog.Default(),
	}, manager, sender)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}

	stateDelta, err := protocol.NewSUMStateDelta(1, protocol.SUMDelta{
		NodeID: "node1",
		Value:  7,
	})
	if err != nil {
		t.Fatalf("new state delta: %v", err)
	}
	message := envelopeForTest(t, "node1", 1, stateDelta)

	runtime.HandleEnvelope(context.Background(), message)
	if got := len(sender.snapshot()); got != 2 {
		t.Fatalf("expected advancing delta to be forwarded to 2 peers, got %d", got)
	}

	runtime.HandleEnvelope(context.Background(), message)
	if got := len(sender.snapshot()); got != 2 {
		t.Fatalf("expected duplicate delta not to be forwarded again, got %d sends", got)
	}
}

func envelopeForTest(t *testing.T, from string, seq uint64, stateDelta protocol.StateDelta) transport.Envelope {
	t.Helper()
	payload, err := json.Marshal(stateDelta)
	if err != nil {
		t.Fatalf("marshal state delta: %v", err)
	}
	return transport.Envelope{
		Type:      messageTypeStateDelta,
		Seq:       seq,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		From:      from,
		Version:   "v1",
		Payload:   payload,
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met before timeout")
}
