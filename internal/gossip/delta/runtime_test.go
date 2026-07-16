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
	waitFor(t, func() bool { return countSentType(sender.snapshot(), messageTypeStateDelta) == 2 })

	for _, sent := range sender.snapshot() {
		if sent.message.Type != messageTypeStateDelta {
			continue
		}
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

func TestRuntimeRequestsSnapshotWhenDigestDiffers(t *testing.T) {
	local := newRuntimeManager(t, "node2")
	peer := newRuntimeManager(t, "node1")
	if _, advanced, err := peer.ApplyLocalUpdate(pipeline.LocalUpdate{
		AggregateType: common.AggregateSUM,
		Value:         uint64(9),
	}); err != nil || !advanced {
		t.Fatalf("peer update advanced=%v err=%v", advanced, err)
	}
	peerDigest, err := peer.Digest()
	if err != nil {
		t.Fatalf("peer digest: %v", err)
	}

	sender := &recordingSender{}
	runtime, err := NewRuntime(Config{
		NodeID:       "node2",
		SelfEndpoint: "node2:7000",
		Peers:        []string{"node1:7000", "node2:7000"},
		Fanout:       1,
		SendTimeout:  time.Second,
		Logger:       slog.Default(),
	}, local, sender)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}

	runtime.HandleEnvelope(context.Background(), genericEnvelopeForTest(t, "StateDigest", "node1", 1, peerDigest))

	sent := sender.snapshot()
	if len(sent) != 1 {
		t.Fatalf("expected one snapshot request, got %d", len(sent))
	}
	if sent[0].peer != "node1:7000" || sent[0].message.Type != "SnapshotReq" {
		t.Fatalf("unexpected snapshot request send: %+v", sent[0])
	}
	req, err := protocol.DecodeSnapshotReq(sent[0].message.Payload)
	if err != nil {
		t.Fatalf("decode snapshot request: %v", err)
	}
	if len(req.WantAggregateTypes) != 1 || req.WantAggregateTypes[0] != common.AggregateSUM {
		t.Fatalf("unexpected requested aggregates: %v", req.WantAggregateTypes)
	}
}

func TestRuntimeAppliesSnapshotResponse(t *testing.T) {
	source := newRuntimeManager(t, "node1")
	target := newRuntimeManager(t, "node2")
	if _, advanced, err := source.ApplyLocalUpdate(pipeline.LocalUpdate{
		AggregateType: common.AggregateSUM,
		Value:         uint64(13),
	}); err != nil || !advanced {
		t.Fatalf("source update advanced=%v err=%v", advanced, err)
	}
	snapshot, err := source.Snapshot([]string{common.AggregateSUM})
	if err != nil {
		t.Fatalf("source snapshot: %v", err)
	}

	sender := &recordingSender{}
	runtime, err := NewRuntime(Config{
		NodeID:       "node2",
		SelfEndpoint: "node2:7000",
		Peers:        []string{"node1:7000", "node2:7000", "node3:7000"},
		Fanout:       2,
		SendTimeout:  time.Second,
		Logger:       slog.Default(),
	}, target, sender)
	if err != nil {
		t.Fatalf("new runtime: %v", err)
	}

	runtime.HandleEnvelope(context.Background(), genericEnvelopeForTest(t, "SnapshotResp", "node1", 1, snapshot))

	estimates, err := target.Estimates(3)
	if err != nil {
		t.Fatalf("target estimates: %v", err)
	}
	if estimates.SUM != 13 {
		t.Fatalf("expected healed SUM 13, got %d", estimates.SUM)
	}
}

func TestRuntimeAntiEntropyHealsDroppedDelta(t *testing.T) {
	node1 := newRuntimeManager(t, "node1")
	node2 := newRuntimeManager(t, "node2")
	missedDelta, advanced, err := node1.ApplyLocalUpdate(pipeline.LocalUpdate{
		AggregateType: common.AggregateSUM,
		Value:         uint64(21),
	})
	if err != nil || !advanced {
		t.Fatalf("node1 update advanced=%v err=%v", advanced, err)
	}

	node1Sender := &recordingSender{}
	node1Runtime, err := NewRuntime(Config{
		NodeID:       "node1",
		SelfEndpoint: "node1:7000",
		Peers:        []string{"node1:7000", "node2:7000"},
		Fanout:       1,
		SendTimeout:  time.Second,
		Logger:       slog.Default(),
	}, node1, node1Sender)
	if err != nil {
		t.Fatalf("new node1 runtime: %v", err)
	}
	node1Runtime.recordDelta(missedDelta)

	node2Sender := &recordingSender{}
	node2Runtime, err := NewRuntime(Config{
		NodeID:       "node2",
		SelfEndpoint: "node2:7000",
		Peers:        []string{"node1:7000", "node2:7000"},
		Fanout:       1,
		SendTimeout:  time.Second,
		Logger:       slog.Default(),
	}, node2, node2Sender)
	if err != nil {
		t.Fatalf("new node2 runtime: %v", err)
	}

	digest, err := node1Runtime.newDigestEnvelope()
	if err != nil {
		t.Fatalf("node1 digest: %v", err)
	}
	node2Runtime.HandleEnvelope(context.Background(), digest)
	req := onlySentOfType(t, node2Sender.snapshot(), messageTypeDeltaRangeReq)

	node1Runtime.HandleEnvelope(context.Background(), req.message)
	resp := onlySentOfType(t, node1Sender.snapshot(), messageTypeDeltaRangeResp)

	node2Runtime.HandleEnvelope(context.Background(), resp.message)
	estimates, err := node2.Estimates(3)
	if err != nil {
		t.Fatalf("node2 estimates: %v", err)
	}
	if estimates.SUM != 21 {
		t.Fatalf("expected healed SUM 21, got %d", estimates.SUM)
	}
}

func TestRuntimeFallsBackToSnapshotWhenDeltaRangeWasEvicted(t *testing.T) {
	source := newRuntimeManager(t, "node1")
	target := newRuntimeManager(t, "node2")
	sourceSender := &recordingSender{}
	sourceRuntime, err := NewRuntime(Config{
		NodeID:           "node1",
		SelfEndpoint:     "node1:7000",
		Peers:            []string{"node1:7000", "node2:7000"},
		Fanout:           1,
		SendTimeout:      time.Second,
		DeltaHistorySize: 1,
	}, source, sourceSender)
	if err != nil {
		t.Fatalf("new source runtime: %v", err)
	}
	for i := 0; i < 2; i++ {
		delta, advanced, err := source.ApplyLocalUpdate(pipeline.LocalUpdate{
			AggregateType: common.AggregateSUM,
			Value:         uint64(1),
		})
		if err != nil || !advanced {
			t.Fatalf("source update %d advanced=%v err=%v", i, advanced, err)
		}
		sourceRuntime.recordDelta(delta)
	}

	targetSender := &recordingSender{}
	targetRuntime, err := NewRuntime(Config{
		NodeID:       "node2",
		SelfEndpoint: "node2:7000",
		Peers:        []string{"node1:7000", "node2:7000"},
		Fanout:       1,
		SendTimeout:  time.Second,
	}, target, targetSender)
	if err != nil {
		t.Fatalf("new target runtime: %v", err)
	}

	digest, err := sourceRuntime.newDigestEnvelope()
	if err != nil {
		t.Fatalf("source digest: %v", err)
	}
	targetRuntime.HandleEnvelope(context.Background(), digest)
	req := onlySentOfType(t, targetSender.snapshot(), messageTypeDeltaRangeReq)
	sourceRuntime.HandleEnvelope(context.Background(), req.message)
	resp := onlySentOfType(t, sourceSender.snapshot(), messageTypeSnapshotResp)
	targetRuntime.HandleEnvelope(context.Background(), resp.message)

	estimates, err := target.Estimates(3)
	if err != nil {
		t.Fatalf("target estimates: %v", err)
	}
	if estimates.SUM != 2 {
		t.Fatalf("expected snapshot fallback SUM 2, got %d", estimates.SUM)
	}
}

func TestRuntimeAntiEntropyHealsTemporaryNetworkPartition(t *testing.T) {
	network := newSimulatedNetwork(true)
	node1 := newRuntimeManager(t, "node1")
	node2 := newRuntimeManager(t, "node2")

	node1Runtime, err := NewRuntime(Config{
		NodeID:              "node1",
		SelfEndpoint:        "node1:7000",
		Peers:               []string{"node1:7000", "node2:7000"},
		Fanout:              1,
		SendTimeout:         time.Second,
		AntiEntropyInterval: 20 * time.Millisecond,
		DeltaHistorySize:    16,
	}, node1, network)
	if err != nil {
		t.Fatalf("new node1 runtime: %v", err)
	}
	node2Runtime, err := NewRuntime(Config{
		NodeID:              "node2",
		SelfEndpoint:        "node2:7000",
		Peers:               []string{"node1:7000", "node2:7000"},
		Fanout:              1,
		SendTimeout:         time.Second,
		AntiEntropyInterval: 20 * time.Millisecond,
		DeltaHistorySize:    16,
	}, node2, network)
	if err != nil {
		t.Fatalf("new node2 runtime: %v", err)
	}
	network.register("node1:7000", node1Runtime)
	network.register("node2:7000", node2Runtime)

	for _, increment := range []uint64{5, 7} {
		if _, advanced, err := node1.ApplyLocalUpdate(pipeline.LocalUpdate{
			AggregateType: common.AggregateSUM,
			Value:         increment,
		}); err != nil || !advanced {
			t.Fatalf("node1 update advanced=%v err=%v", advanced, err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	node1Runtime.Start(ctx)
	node2Runtime.Start(ctx)
	waitFor(t, func() bool { return network.countType(messageTypeStateDelta) >= 2 })

	if estimates, err := node2.Estimates(3); err != nil || estimates.SUM != 0 {
		t.Fatalf("partitioned node unexpectedly converged: estimates=%+v err=%v", estimates, err)
	}
	network.setPartitioned(false)
	waitFor(t, func() bool {
		estimates, err := node2.Estimates(3)
		return err == nil && estimates.SUM == 12
	})

	if network.countType(messageTypeDeltaRangeReq) == 0 || network.countType(messageTypeDeltaRangeResp) == 0 {
		t.Fatalf("expected range repair after partition, sent types=%v", network.messageTypes())
	}
}

type simulatedNetwork struct {
	mu          sync.RWMutex
	partitioned bool
	runtimes    map[string]*Runtime
	sentTypes   []string
}

func newSimulatedNetwork(partitioned bool) *simulatedNetwork {
	return &simulatedNetwork{partitioned: partitioned, runtimes: make(map[string]*Runtime)}
}

func (n *simulatedNetwork) register(endpoint string, runtime *Runtime) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.runtimes[endpoint] = runtime
}

func (n *simulatedNetwork) setPartitioned(partitioned bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.partitioned = partitioned
}

func (n *simulatedNetwork) Send(ctx context.Context, peer string, message transport.Envelope) error {
	n.mu.Lock()
	n.sentTypes = append(n.sentTypes, message.Type)
	partitioned := n.partitioned
	target := n.runtimes[peer]
	n.mu.Unlock()
	if partitioned || target == nil {
		return nil
	}
	target.HandleEnvelope(ctx, message)
	return nil
}

func (n *simulatedNetwork) countType(messageType string) int {
	n.mu.RLock()
	defer n.mu.RUnlock()
	count := 0
	for _, sentType := range n.sentTypes {
		if sentType == messageType {
			count++
		}
	}
	return count
}

func (n *simulatedNetwork) messageTypes() []string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return append([]string(nil), n.sentTypes...)
}

func envelopeForTest(t *testing.T, from string, seq uint64, stateDelta protocol.StateDelta) transport.Envelope {
	return genericEnvelopeForTest(t, messageTypeStateDelta, from, seq, stateDelta)
}

func genericEnvelopeForTest(t *testing.T, messageType string, from string, seq uint64, payload any) transport.Envelope {
	t.Helper()
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return transport.Envelope{
		Type:      messageType,
		Seq:       seq,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		From:      from,
		Version:   "v1",
		Payload:   payloadBytes,
	}
}

func newRuntimeManager(t *testing.T, nodeID string) *pipeline.Manager {
	t.Helper()
	manager, err := pipeline.New(pipeline.Config{
		NodeID:            nodeID,
		TopKMax:           3,
		OutboundQueueSize: 4,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	return manager
}

func countSentType(sent []sentMessage, messageType string) int {
	count := 0
	for _, item := range sent {
		if item.message.Type == messageType {
			count++
		}
	}
	return count
}

func onlySentOfType(t *testing.T, sent []sentMessage, messageType string) sentMessage {
	t.Helper()
	var matches []sentMessage
	for _, item := range sent {
		if item.message.Type == messageType {
			matches = append(matches, item)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("expected exactly one %s message, got %d", messageType, len(matches))
	}
	return matches[0]
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
