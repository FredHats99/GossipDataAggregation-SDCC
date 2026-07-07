package membership

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"gossipdataaggregation-sdcc/internal/gossip/transport"
)

const (
	joinAttempts      = 4
	suspectAfterMiss  = 3
	deadAfterMiss     = 6
	suspectAfterTicks = 3
	deadAfterTicks    = 6
)

type Bootstrapper struct {
	nodeID         string
	bindAddr       string
	seeds          []string
	fanout         int
	gossipInterval time.Duration
	logger         *slog.Logger
	table          *Table
	codec          *transport.JSONCodec
	seq            atomic.Uint64
}

type pingPayload struct {
	NodeID      string `json:"node_id"`
	Incarnation uint64 `json:"incarnation"`
}

type ackPayload struct {
	AckedSeq uint64 `json:"acked_seq"`
	Status   string `json:"status"`
	Reason   string `json:"reason,omitempty"`
}

func NewBootstrapper(
	nodeID string,
	bindAddr string,
	seeds []string,
	gossipInterval time.Duration,
	fanout int,
	logger *slog.Logger,
) *Bootstrapper {
	return &Bootstrapper{
		nodeID:         nodeID,
		bindAddr:       bindAddr,
		seeds:          seeds,
		fanout:         fanout,
		gossipInterval: gossipInterval,
		logger:         logger,
		table:          NewTable(nodeID, bindAddr),
		codec:          transport.NewJSONCodec(),
	}
}

func (b *Bootstrapper) StartJoinListener(ctx context.Context) error {
	udp, err := transport.NewUDPFrameTransport(b.bindAddr, transport.DefaultMaxFrameSize)
	if err != nil {
		return fmt.Errorf("start UDP join listener: %w", err)
	}

	go func() {
		<-ctx.Done()
		_ = udp.Close()
	}()

	go func() {
		for {
			remotePeer, frame, err := udp.NextFrame(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				b.logger.Warn("membership join listener read failed", "error", err.Error())
				continue
			}

			message, err := b.codec.Decode(frame)
			if err != nil {
				b.logger.Warn("membership join listener rejected frame", "peer", remotePeer, "error", err.Error())
				continue
			}
			if message.Type != "Ping" {
				b.logger.Debug("membership join listener ignored non-ping message", "peer", remotePeer, "msg_type", message.Type)
				continue
			}
			peerNodeID, err := decodePingNodeID(message)
			if err != nil {
				b.logger.Warn("membership ping payload rejected", "peer", remotePeer, "error", err.Error())
				continue
			}

			b.table.MarkAlive(peerNodeID, "")
			ack, err := b.newEnvelope("Ack", ackPayload{AckedSeq: message.Seq, Status: "accepted"})
			if err != nil {
				b.logger.Warn("membership ACK encode failed", "peer", remotePeer, "peer_node_id", peerNodeID, "error", err.Error())
				continue
			}
			ackFrame, err := b.codec.Encode(ack)
			if err != nil {
				b.logger.Warn("membership ACK encode failed", "peer", remotePeer, "peer_node_id", peerNodeID, "error", err.Error())
				continue
			}
			sendCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			err = udp.SendFrame(sendCtx, remotePeer, ackFrame)
			cancel()
			if err != nil {
				b.logger.Warn(
					"membership join ACK send failed",
					"peer", remotePeer,
					"peer_node_id", peerNodeID,
					"error", err.Error(),
				)
				continue
			}
			b.logger.Debug("membership join ACK sent", "peer", remotePeer, "peer_node_id", peerNodeID)
		}
	}()

	b.logger.Info("membership join listener started", "bind_addr", b.bindAddr)
	return nil
}

func (b *Bootstrapper) JoinSeeds(ctx context.Context) {
	if len(b.seeds) == 0 {
		b.logger.Info("membership bootstrap skipped: no seed nodes configured")
		return
	}

	for _, seed := range b.seeds {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if b.isSelfSeed(seed) {
			b.logger.Debug("membership bootstrap skipping self seed", "seed", seed, "node_id", b.nodeID)
			continue
		}

		peerNodeID, err := b.joinSeedWithRetry(ctx, seed)
		if err != nil {
			b.logger.Warn("membership join failed after retries", "seed", seed, "error", err.Error())
			continue
		}
		b.table.MarkAlive(peerNodeID, seed)
		b.logger.Info("membership join succeeded", "seed", seed)
	}
}

func (b *Bootstrapper) isSelfSeed(seed string) bool {
	host, _, err := net.SplitHostPort(seed)
	if err != nil {
		return false
	}
	if strings.TrimSpace(host) == b.nodeID {
		return true
	}
	if sameEndpoint(seed, b.bindAddr) {
		return true
	}
	return false
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
	leftHost = normalizeLocalHost(leftHost)
	rightHost = normalizeLocalHost(rightHost)
	return leftHost == rightHost
}

func normalizeLocalHost(host string) string {
	host = strings.TrimSpace(host)
	switch host {
	case "", "localhost", "127.0.0.1", "0.0.0.0", "::1", "[::1]":
		return "localhost"
	default:
		return host
	}
}

func (b *Bootstrapper) StartGossipLoop(ctx context.Context) {
	ticker := time.NewTicker(b.gossipInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.gossipOnce(ctx)
		}
	}
}

func (b *Bootstrapper) gossipOnce(ctx context.Context) {
	peers := b.samplePeers()
	if len(peers) == 0 {
		return
	}

	for _, peer := range peers {
		select {
		case <-ctx.Done():
			return
		default:
		}

		peerNodeID, err := b.joinSeed(peer)
		if err != nil {
			b.logger.Debug("membership gossip ping failed", "peer", peer, "error", err.Error())
			b.table.MarkMissedByEndpoint(peer, suspectAfterMiss, deadAfterMiss)
			continue
		}
		b.table.MarkAlive(peerNodeID, peer)
	}
	now := time.Now().UTC()
	b.table.ApplyTimeouts(
		now,
		time.Duration(suspectAfterTicks)*b.gossipInterval,
		time.Duration(deadAfterTicks)*b.gossipInterval,
	)

	b.logger.Debug("membership gossip round completed", "sampled_peers", len(peers), "known_members", len(b.table.Snapshot()))
}

func (b *Bootstrapper) samplePeers() []string {
	candidates := make([]string, 0, len(b.seeds))
	for _, seed := range b.seeds {
		if b.isSelfSeed(seed) {
			continue
		}
		candidates = append(candidates, seed)
	}

	if len(candidates) <= b.fanout {
		return candidates
	}

	shuffled := append([]string(nil), candidates...)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	return shuffled[:b.fanout]
}

func (b *Bootstrapper) joinSeedWithRetry(ctx context.Context, seed string) (string, error) {
	var lastErr error
	var peerNodeID string
	for attempt := 1; attempt <= joinAttempts; attempt++ {
		peerNodeID, lastErr = b.joinSeed(seed)
		if lastErr == nil {
			return peerNodeID, nil
		}

		backoff := time.Duration(attempt) * time.Second
		b.logger.Warn(
			"membership join attempt failed",
			"seed", seed,
			"attempt", attempt,
			"max_attempts", joinAttempts,
			"retry_in", backoff.String(),
			"error", lastErr.Error(),
		)

		if attempt == joinAttempts {
			break
		}

		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}
	}
	return "", lastErr
}

func (b *Bootstrapper) joinSeed(seed string) (string, error) {
	udp, err := transport.NewUDPFrameTransport(":0", transport.DefaultMaxFrameSize)
	if err != nil {
		return "", fmt.Errorf("start UDP join client: %w", err)
	}
	defer udp.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ping, err := b.newEnvelope("Ping", pingPayload{NodeID: b.nodeID, Incarnation: 1})
	if err != nil {
		return "", fmt.Errorf("build ping: %w", err)
	}
	frame, err := b.codec.Encode(ping)
	if err != nil {
		return "", fmt.Errorf("encode ping: %w", err)
	}
	if err := udp.SendFrame(ctx, seed, frame); err != nil {
		return "", fmt.Errorf("send ping: %w", err)
	}

	for {
		_, ackFrame, err := udp.NextFrame(ctx)
		if err != nil {
			return "", fmt.Errorf("read ack: %w", err)
		}
		ack, err := b.codec.Decode(ackFrame)
		if err != nil {
			return "", fmt.Errorf("decode ack: %w", err)
		}
		if ack.Type != "Ack" {
			continue
		}
		if err := validateAcceptedAck(ack, ping.Seq); err != nil {
			return "", err
		}
		return ack.From, nil
	}
}

func (b *Bootstrapper) MembersSnapshot() []Member {
	return b.table.Snapshot()
}

func (b *Bootstrapper) newEnvelope(messageType string, payload any) (transport.Envelope, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return transport.Envelope{}, err
	}
	return transport.Envelope{
		Type:      messageType,
		Seq:       b.seq.Add(1),
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		From:      b.nodeID,
		Version:   "v1",
		Payload:   payloadBytes,
	}, nil
}

func decodePingNodeID(message transport.Envelope) (string, error) {
	var payload pingPayload
	if err := json.Unmarshal(message.Payload, &payload); err != nil {
		return "", fmt.Errorf("decode ping payload: %w", err)
	}
	if strings.TrimSpace(payload.NodeID) == "" {
		return "", fmt.Errorf("empty ping node_id")
	}
	if payload.NodeID != message.From {
		return "", fmt.Errorf("ping node_id %q does not match envelope from %q", payload.NodeID, message.From)
	}
	return payload.NodeID, nil
}

func validateAcceptedAck(message transport.Envelope, expectedSeq uint64) error {
	var payload ackPayload
	if err := json.Unmarshal(message.Payload, &payload); err != nil {
		return fmt.Errorf("decode ack payload: %w", err)
	}
	if payload.AckedSeq != expectedSeq {
		return fmt.Errorf("unexpected ACK sequence %d for ping sequence %d", payload.AckedSeq, expectedSeq)
	}
	if payload.Status != "accepted" {
		if payload.Reason != "" {
			return fmt.Errorf("ACK rejected: %s", payload.Reason)
		}
		return fmt.Errorf("ACK rejected")
	}
	if strings.TrimSpace(message.From) == "" {
		return fmt.Errorf("empty peer node_id in ACK")
	}
	return nil
}
