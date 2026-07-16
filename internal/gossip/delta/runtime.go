package delta

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"gossipdataaggregation-sdcc/internal/aggregation/common"
	"gossipdataaggregation-sdcc/internal/aggregation/pipeline"
	antientropy "gossipdataaggregation-sdcc/internal/gossip/anti_entropy"
	"gossipdataaggregation-sdcc/internal/gossip/protocol"
	"gossipdataaggregation-sdcc/internal/gossip/transport"
)

const (
	messageTypeStateDelta     = "StateDelta"
	messageTypeStateDigest    = "StateDigest"
	messageTypeDeltaRangeReq  = "DeltaRangeReq"
	messageTypeDeltaRangeResp = "DeltaRangeResp"
	messageTypeSnapshotReq    = "SnapshotReq"
	messageTypeSnapshotResp   = "SnapshotResp"
	defaultDeltaHistorySize   = 1024
)

var (
	ErrNilManager = errors.New("delta runtime: nil manager")
	ErrNilSender  = errors.New("delta runtime: nil sender")
)

type Config struct {
	NodeID              string
	SelfEndpoint        string
	Peers               []string
	Fanout              int
	SendTimeout         time.Duration
	AntiEntropyInterval time.Duration
	DeltaHistorySize    int
	Logger              *slog.Logger
}

type Runtime struct {
	nodeID              string
	selfEndpoint        string
	peers               []string
	fanout              int
	sendTimeout         time.Duration
	antiEntropyInterval time.Duration
	logger              *slog.Logger
	manager             *pipeline.Manager
	history             *antientropy.DeltaHistory
	sender              transport.Sender
	guard               *transport.MessageGuard
	seq                 atomic.Uint64
}

func NewRuntime(config Config, manager *pipeline.Manager, sender transport.Sender) (*Runtime, error) {
	if manager == nil {
		return nil, ErrNilManager
	}
	if sender == nil {
		return nil, ErrNilSender
	}
	if config.Fanout <= 0 {
		config.Fanout = len(config.Peers)
	}
	if config.SendTimeout <= 0 {
		config.SendTimeout = 2 * time.Second
	}
	if config.AntiEntropyInterval <= 0 {
		config.AntiEntropyInterval = 15 * time.Second
	}
	if config.DeltaHistorySize <= 0 {
		config.DeltaHistorySize = defaultDeltaHistorySize
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}

	history, err := antientropy.NewDeltaHistory(config.DeltaHistorySize)
	if err != nil {
		return nil, err
	}

	return &Runtime{
		nodeID:              config.NodeID,
		selfEndpoint:        config.SelfEndpoint,
		peers:               normalizedPeers(config.Peers),
		fanout:              config.Fanout,
		sendTimeout:         config.SendTimeout,
		antiEntropyInterval: config.AntiEntropyInterval,
		logger:              config.Logger,
		manager:             manager,
		history:             history,
		sender:              sender,
		guard:               transport.NewMessageGuard(time.Minute),
	}, nil
}

func (r *Runtime) Start(ctx context.Context) {
	go r.runOutbound(ctx)
	go r.runAntiEntropy(ctx)
}

func (r *Runtime) HandleEnvelope(ctx context.Context, message transport.Envelope) {
	switch message.Type {
	case messageTypeStateDelta:
		r.handleStateDelta(ctx, message)
	case messageTypeStateDigest:
		r.handleStateDigest(ctx, message)
	case messageTypeDeltaRangeReq:
		r.handleDeltaRangeReq(ctx, message)
	case messageTypeDeltaRangeResp:
		r.handleDeltaRangeResp(ctx, message)
	case messageTypeSnapshotReq:
		r.handleSnapshotReq(ctx, message)
	case messageTypeSnapshotResp:
		r.handleSnapshotResp(ctx, message)
	}
}

func (r *Runtime) handleStateDelta(ctx context.Context, message transport.Envelope) {
	if err := r.guard.Accept(message); err != nil {
		if errors.Is(err, transport.ErrDuplicateMessage) || errors.Is(err, transport.ErrStaleSequence) {
			r.logger.Debug("state delta skipped by message guard", "from", message.From, "seq", message.Seq, "error", err.Error())
			return
		}
		r.logger.Warn("state delta rejected by message guard", "from", message.From, "seq", message.Seq, "error", err.Error())
		return
	}

	var stateDelta protocol.StateDelta
	if err := json.Unmarshal(message.Payload, &stateDelta); err != nil {
		r.logger.Warn("state delta payload decode failed", "from", message.From, "seq", message.Seq, "error", err.Error())
		return
	}

	advanced, err := r.manager.ApplyReceivedDelta(stateDelta)
	if err != nil {
		r.logger.Warn("state delta merge failed", "from", message.From, "seq", message.Seq, "error", err.Error())
		return
	}
	r.recordDelta(stateDelta)
	if !advanced {
		return
	}

	r.logger.Debug("state delta advanced local aggregate state", "from", message.From, "seq", message.Seq)
	r.broadcastEnvelope(ctx, message)
}

func (r *Runtime) handleStateDigest(ctx context.Context, message transport.Envelope) {
	peerDigest, err := protocol.DecodeStateDigest(message.Payload)
	if err != nil {
		r.logger.Warn("state digest decode failed", "from", message.From, "seq", message.Seq, "error", err.Error())
		return
	}
	localDigest, err := r.manager.Digest()
	if err != nil {
		r.logger.Warn("local state digest build failed", "error", err.Error())
		return
	}
	localDigest.DeltaSequences = r.history.Watermarks()

	ranges := antientropy.MissingDeltaRanges(localDigest, peerDigest)
	if len(ranges) > 0 {
		req, err := protocol.NewDeltaRangeReq(ranges, localDigest)
		if err != nil {
			r.logger.Warn("delta range request build failed", "from", message.From, "error", err.Error())
			return
		}
		envelope, err := r.newEnvelope(messageTypeDeltaRangeReq, req)
		if err != nil {
			r.logger.Warn("delta range request envelope build failed", "from", message.From, "error", err.Error())
			return
		}
		r.sendEnvelopeToNode(ctx, message.From, envelope)
		return
	}

	want := antientropy.AggregatesNeedingSnapshot(localDigest, peerDigest)
	if len(want) == 0 {
		return
	}

	req, err := protocol.NewSnapshotReq(want, localDigest)
	if err != nil {
		r.logger.Warn("snapshot request build failed", "from", message.From, "error", err.Error())
		return
	}
	envelope, err := r.newEnvelope(messageTypeSnapshotReq, req)
	if err != nil {
		r.logger.Warn("snapshot request envelope build failed", "from", message.From, "error", err.Error())
		return
	}
	r.sendEnvelopeToNode(ctx, message.From, envelope)
}

func (r *Runtime) handleDeltaRangeReq(ctx context.Context, message transport.Envelope) {
	req, err := protocol.DecodeDeltaRangeReq(message.Payload)
	if err != nil {
		r.logger.Warn("delta range request decode failed", "from", message.From, "seq", message.Seq, "error", err.Error())
		return
	}

	var deltas []protocol.StateDelta
	for _, deltaRange := range req.Ranges {
		items, ok := r.history.Range(deltaRange)
		if !ok {
			r.sendSnapshotFallback(ctx, message.From, req.KnownVersions)
			return
		}
		deltas = append(deltas, items...)
	}
	resp, err := protocol.NewDeltaRangeResp(deltas)
	if err != nil {
		r.logger.Warn("delta range response build failed", "from", message.From, "error", err.Error())
		return
	}
	envelope, err := r.newEnvelope(messageTypeDeltaRangeResp, resp)
	if err != nil {
		r.logger.Warn("delta range response envelope build failed", "from", message.From, "error", err.Error())
		return
	}
	r.sendEnvelopeToNode(ctx, message.From, envelope)
}

func (r *Runtime) handleDeltaRangeResp(ctx context.Context, message transport.Envelope) {
	resp, err := protocol.DecodeDeltaRangeResp(message.Payload)
	if err != nil {
		r.logger.Warn("delta range response decode failed", "from", message.From, "seq", message.Seq, "error", err.Error())
		return
	}

	advanced := false
	for _, delta := range resp.Deltas {
		deltaAdvanced, err := r.manager.ApplyReceivedDelta(delta)
		if err != nil {
			r.logger.Warn("recovered delta merge failed", "from", message.From, "origin", delta.OriginNodeID, "delta_sequence", delta.DeltaSequence, "error", err.Error())
			return
		}
		r.recordDelta(delta)
		advanced = advanced || deltaAdvanced
	}
	r.logger.Debug("delta range applied", "from", message.From, "count", len(resp.Deltas), "advanced", advanced)
	r.broadcastDigest(ctx)
}

func (r *Runtime) handleSnapshotReq(ctx context.Context, message transport.Envelope) {
	req, err := protocol.DecodeSnapshotReq(message.Payload)
	if err != nil {
		r.logger.Warn("snapshot request decode failed", "from", message.From, "seq", message.Seq, "error", err.Error())
		return
	}
	r.sendSnapshot(ctx, message.From, req.WantAggregateTypes)
}

func (r *Runtime) handleSnapshotResp(ctx context.Context, message transport.Envelope) {
	resp, err := protocol.DecodeSnapshotResp(message.Payload)
	if err != nil {
		r.logger.Warn("snapshot response decode failed", "from", message.From, "seq", message.Seq, "error", err.Error())
		return
	}
	advanced, err := r.manager.ApplySnapshot(resp)
	if err != nil {
		r.logger.Warn("snapshot apply failed", "from", message.From, "seq", message.Seq, "error", err.Error())
		return
	}
	r.history.AdvanceWatermarks(resp.DeltaSequences)
	if !advanced {
		return
	}
	r.logger.Debug("snapshot advanced local aggregate state", "from", message.From, "seq", message.Seq)
	if digest, err := r.newDigestEnvelope(); err == nil {
		r.broadcastEnvelope(ctx, digest)
	}
}

func (r *Runtime) runOutbound(ctx context.Context) {
	for {
		stateDelta, err := r.manager.NextOutbound(ctx)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, pipeline.ErrOutboundQueueClosed) {
				return
			}
			r.logger.Warn("state delta outbound queue read failed", "error", err.Error())
			continue
		}

		message, err := r.newStateDeltaEnvelope(stateDelta)
		if err != nil {
			r.logger.Warn("state delta envelope build failed", "error", err.Error())
			continue
		}
		r.recordDelta(stateDelta)
		r.broadcastEnvelope(ctx, message)
	}
}

func (r *Runtime) runAntiEntropy(ctx context.Context) {
	r.broadcastDigest(ctx)
	ticker := time.NewTicker(r.antiEntropyInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.broadcastDigest(ctx)
		}
	}
}

func (r *Runtime) broadcastDigest(ctx context.Context) {
	message, err := r.newDigestEnvelope()
	if err != nil {
		r.logger.Warn("state digest envelope build failed", "error", err.Error())
		return
	}
	r.broadcastEnvelope(ctx, message)
}

func (r *Runtime) newStateDeltaEnvelope(stateDelta protocol.StateDelta) (transport.Envelope, error) {
	return r.newEnvelope(messageTypeStateDelta, stateDelta)
}

func (r *Runtime) newDigestEnvelope() (transport.Envelope, error) {
	digest, err := r.manager.Digest()
	if err != nil {
		return transport.Envelope{}, err
	}
	digest.DeltaSequences = r.history.Watermarks()
	return r.newEnvelope(messageTypeStateDigest, digest)
}

func (r *Runtime) sendSnapshotFallback(ctx context.Context, nodeID string, known protocol.StateDigest) {
	local, err := r.manager.Digest()
	if err != nil {
		r.logger.Warn("snapshot fallback digest build failed", "node_id", nodeID, "error", err.Error())
		return
	}
	want := antientropy.AggregatesNeedingSnapshot(known, local)
	if len(want) == 0 {
		want = []string{common.AggregateSUM, common.AggregateTOPK}
	}
	r.sendSnapshot(ctx, nodeID, want)
}

func (r *Runtime) sendSnapshot(ctx context.Context, nodeID string, want []string) {
	resp, err := r.manager.Snapshot(want)
	if err != nil {
		r.logger.Warn("snapshot build failed", "node_id", nodeID, "error", err.Error())
		return
	}
	resp.DeltaSequences = r.history.Watermarks()
	envelope, err := r.newEnvelope(messageTypeSnapshotResp, resp)
	if err != nil {
		r.logger.Warn("snapshot response envelope build failed", "node_id", nodeID, "error", err.Error())
		return
	}
	r.sendEnvelopeToNode(ctx, nodeID, envelope)
}

func (r *Runtime) recordDelta(delta protocol.StateDelta) {
	if delta.OriginNodeID == "" || delta.DeltaSequence == 0 {
		return
	}
	if err := r.history.Record(delta); err != nil {
		r.logger.Warn("delta history record failed", "origin", delta.OriginNodeID, "delta_sequence", delta.DeltaSequence, "error", err.Error())
	}
}

func (r *Runtime) newEnvelope(messageType string, payload any) (transport.Envelope, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return transport.Envelope{}, err
	}
	return transport.Envelope{
		Type:      messageType,
		Seq:       r.seq.Add(1),
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		From:      r.nodeID,
		Version:   "v1",
		Payload:   payloadBytes,
	}, nil
}

func (r *Runtime) broadcastEnvelope(ctx context.Context, message transport.Envelope) {
	for _, peer := range r.samplePeers() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		sendCtx, cancel := context.WithTimeout(ctx, r.sendTimeout)
		err := r.sender.Send(sendCtx, peer, message)
		cancel()
		if err != nil {
			r.logger.Debug("state delta send failed", "peer", peer, "from", message.From, "seq", message.Seq, "error", err.Error())
			continue
		}
		r.logger.Debug("state delta sent", "peer", peer, "from", message.From, "seq", message.Seq)
	}
}

func (r *Runtime) sendEnvelopeToNode(ctx context.Context, nodeID string, message transport.Envelope) {
	peer, ok := r.peerEndpointForNode(nodeID)
	if !ok {
		r.logger.Debug("peer endpoint not found", "node_id", nodeID, "msg_type", message.Type)
		return
	}
	sendCtx, cancel := context.WithTimeout(ctx, r.sendTimeout)
	err := r.sender.Send(sendCtx, peer, message)
	cancel()
	if err != nil {
		r.logger.Debug("direct envelope send failed", "peer", peer, "node_id", nodeID, "msg_type", message.Type, "error", err.Error())
	}
}

func (r *Runtime) peerEndpointForNode(nodeID string) (string, bool) {
	for _, peer := range r.peers {
		host, _, err := net.SplitHostPort(peer)
		if err == nil && strings.TrimSpace(host) == nodeID {
			return peer, true
		}
	}
	return "", false
}

func (r *Runtime) samplePeers() []string {
	candidates := make([]string, 0, len(r.peers))
	for _, peer := range r.peers {
		if isSelfPeer(peer, r.nodeID, r.selfEndpoint) {
			continue
		}
		candidates = append(candidates, peer)
	}
	if len(candidates) <= r.fanout {
		return candidates
	}
	shuffled := append([]string(nil), candidates...)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	return shuffled[:r.fanout]
}

func normalizedPeers(peers []string) []string {
	out := make([]string, 0, len(peers))
	seen := make(map[string]struct{}, len(peers))
	for _, peer := range peers {
		peer = strings.TrimSpace(peer)
		if peer == "" {
			continue
		}
		if _, ok := seen[peer]; ok {
			continue
		}
		seen[peer] = struct{}{}
		out = append(out, peer)
	}
	return out
}

func isSelfPeer(peer, nodeID, selfEndpoint string) bool {
	host, _, err := net.SplitHostPort(peer)
	if err == nil && strings.TrimSpace(host) == nodeID {
		return true
	}
	return sameEndpoint(peer, selfEndpoint)
}

func sameEndpoint(left, right string) bool {
	leftHost, leftPort, err := net.SplitHostPort(left)
	if err != nil {
		return false
	}
	rightHost, rightPort, err := net.SplitHostPort(right)
	if err != nil {
		return false
	}
	if leftPort != rightPort {
		return false
	}
	return normalizeLocalHost(leftHost) == normalizeLocalHost(rightHost)
}

func normalizeLocalHost(host string) string {
	switch strings.TrimSpace(host) {
	case "", "localhost", "127.0.0.1", "0.0.0.0", "::1", "[::1]":
		return "localhost"
	default:
		return host
	}
}
