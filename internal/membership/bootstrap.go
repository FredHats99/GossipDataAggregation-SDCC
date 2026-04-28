package membership

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"strings"
	"time"
)

const (
	pingPrefix = "PING "
	ackPrefix  = "ACK "

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
	}
}

func (b *Bootstrapper) StartJoinListener(ctx context.Context) error {
	conn, err := net.ListenPacket("udp", b.bindAddr)
	if err != nil {
		return fmt.Errorf("start UDP join listener: %w", err)
	}

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	go func() {
		buf := make([]byte, 1024)
		for {
			n, remoteAddr, err := conn.ReadFrom(buf)
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				b.logger.Warn("membership join listener read failed", "error", err.Error())
				continue
			}

			msg := strings.TrimSpace(string(buf[:n]))
			if !strings.HasPrefix(msg, pingPrefix) {
				continue
			}
			peerNodeID := strings.TrimSpace(strings.TrimPrefix(msg, pingPrefix))
			b.table.MarkAlive(peerNodeID, "")
			ack := []byte(ackPrefix + b.nodeID)
			if _, err := conn.WriteTo(ack, remoteAddr); err != nil {
				b.logger.Warn(
					"membership join ACK send failed",
					"peer", remoteAddr.String(),
					"peer_node_id", peerNodeID,
					"error", err.Error(),
				)
				continue
			}
			b.logger.Debug("membership join ACK sent", "peer", remoteAddr.String(), "peer_node_id", peerNodeID)
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
	host = strings.TrimSpace(host)
	return host == b.nodeID || host == "localhost" || host == "127.0.0.1" || host == "0.0.0.0"
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
	conn, err := net.DialTimeout("udp", seed, 2*time.Second)
	if err != nil {
		return "", fmt.Errorf("dial seed: %w", err)
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return "", fmt.Errorf("set deadline: %w", err)
	}

	ping := []byte(pingPrefix + b.nodeID)
	if _, err := conn.Write(ping); err != nil {
		return "", fmt.Errorf("send ping: %w", err)
	}

	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil {
		return "", fmt.Errorf("read ack: %w", err)
	}
	msg := strings.TrimSpace(string(buf[:n]))
	if !strings.HasPrefix(msg, ackPrefix) {
		return "", fmt.Errorf("unexpected response %q", msg)
	}
	peerNodeID := strings.TrimSpace(strings.TrimPrefix(msg, ackPrefix))
	if peerNodeID == "" {
		return "", fmt.Errorf("empty peer node_id in ACK")
	}
	return peerNodeID, nil
}

func (b *Bootstrapper) MembersSnapshot() []Member {
	return b.table.Snapshot()
}
