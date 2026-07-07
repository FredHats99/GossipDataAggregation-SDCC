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

	"gossipdataaggregation-sdcc/internal/aggregation/pipeline"
	"gossipdataaggregation-sdcc/internal/gossip/protocol"
	"gossipdataaggregation-sdcc/internal/gossip/transport"
)

const messageTypeStateDelta = "StateDelta"

var (
	ErrNilManager = errors.New("delta runtime: nil manager")
	ErrNilSender  = errors.New("delta runtime: nil sender")
)

type Config struct {
	NodeID       string
	SelfEndpoint string
	Peers        []string
	Fanout       int
	SendTimeout  time.Duration
	Logger       *slog.Logger
}

type Runtime struct {
	nodeID       string
	selfEndpoint string
	peers        []string
	fanout       int
	sendTimeout  time.Duration
	logger       *slog.Logger
	manager      *pipeline.Manager
	sender       transport.Sender
	guard        *transport.MessageGuard
	seq          atomic.Uint64
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
	if config.Logger == nil {
		config.Logger = slog.Default()
	}

	return &Runtime{
		nodeID:       config.NodeID,
		selfEndpoint: config.SelfEndpoint,
		peers:        normalizedPeers(config.Peers),
		fanout:       config.Fanout,
		sendTimeout:  config.SendTimeout,
		logger:       config.Logger,
		manager:      manager,
		sender:       sender,
		guard:        transport.NewMessageGuard(time.Minute),
	}, nil
}

func (r *Runtime) Start(ctx context.Context) {
	go r.runOutbound(ctx)
}

func (r *Runtime) HandleEnvelope(ctx context.Context, message transport.Envelope) {
	if message.Type != messageTypeStateDelta {
		return
	}
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
	if !advanced {
		return
	}

	r.logger.Debug("state delta advanced local aggregate state", "from", message.From, "seq", message.Seq)
	r.broadcastEnvelope(ctx, message)
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
		r.broadcastEnvelope(ctx, message)
	}
}

func (r *Runtime) newStateDeltaEnvelope(stateDelta protocol.StateDelta) (transport.Envelope, error) {
	payload, err := json.Marshal(stateDelta)
	if err != nil {
		return transport.Envelope{}, err
	}
	return transport.Envelope{
		Type:      messageTypeStateDelta,
		Seq:       r.seq.Add(1),
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		From:      r.nodeID,
		Version:   "v1",
		Payload:   payload,
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
