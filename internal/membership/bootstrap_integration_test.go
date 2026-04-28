package membership

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"
)

func TestMembershipConvergence_NewNodeJoinBecomesClusterVisible(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	addr1 := freeUDPAddr(t)
	addr2 := freeUDPAddr(t)
	addr3 := freeUDPAddr(t)
	seeds := []string{addr1, addr2, addr3}

	node1, cancel1 := startNode(t, "node1", addr1, seeds, 100*time.Millisecond, 2, logger)
	defer cancel1()
	node2, cancel2 := startNode(t, "node2", addr2, seeds, 100*time.Millisecond, 2, logger)
	defer cancel2()

	// Node3 joins after node1/node2 are already running.
	time.Sleep(300 * time.Millisecond)
	node3, cancel3 := startNode(t, "node3", addr3, seeds, 100*time.Millisecond, 2, logger)
	defer cancel3()

	waitFor(t, 10*time.Second, func() bool { return hasMemberStatus(node1.MembersSnapshot(), "node3", StatusAlive) })
	waitFor(t, 10*time.Second, func() bool { return hasMemberStatus(node2.MembersSnapshot(), "node3", StatusAlive) })
	waitFor(t, 10*time.Second, func() bool { return hasMemberStatus(node3.MembersSnapshot(), "node1", StatusAlive) })
}

func TestMembershipConvergence_DeadNodeEventuallyMarkedDead(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	addr1 := freeUDPAddr(t)
	addr2 := freeUDPAddr(t)
	addr3 := freeUDPAddr(t)
	seeds := []string{addr1, addr2, addr3}

	node1, cancel1 := startNode(t, "node1", addr1, seeds, 100*time.Millisecond, 2, logger)
	defer cancel1()
	_, cancel2 := startNode(t, "node2", addr2, seeds, 100*time.Millisecond, 2, logger)
	node3, cancel3 := startNode(t, "node3", addr3, seeds, 100*time.Millisecond, 2, logger)
	defer cancel3()

	waitFor(t, 10*time.Second, func() bool { return hasMemberStatus(node1.MembersSnapshot(), "node2", StatusAlive) })
	waitFor(t, 10*time.Second, func() bool { return hasMemberStatus(node3.MembersSnapshot(), "node2", StatusAlive) })

	// Simulate crash/stop for node2.
	cancel2()

	waitFor(t, 10*time.Second, func() bool { return hasMemberStatus(node1.MembersSnapshot(), "node2", StatusDead) })
	waitFor(t, 10*time.Second, func() bool { return hasMemberStatus(node3.MembersSnapshot(), "node2", StatusDead) })
}

func startNode(
	t *testing.T,
	nodeID string,
	bindAddr string,
	seeds []string,
	gossipInterval time.Duration,
	fanout int,
	logger *slog.Logger,
) (*Bootstrapper, context.CancelFunc) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	b := NewBootstrapper(nodeID, bindAddr, seeds, gossipInterval, fanout, logger)
	if err := b.StartJoinListener(ctx); err != nil {
		cancel()
		t.Fatalf("start listener for %s: %v", nodeID, err)
	}
	go b.StartGossipLoop(ctx)
	go b.JoinSeeds(ctx)
	return b, cancel
}

func freeUDPAddr(t *testing.T) string {
	t.Helper()

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate udp addr: %v", err)
	}
	defer conn.Close()
	return conn.LocalAddr().String()
}

func waitFor(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("condition not reached within %s", timeout)
}

func hasMemberStatus(members []Member, nodeID string, status Status) bool {
	for _, m := range members {
		if m.NodeID == nodeID && m.Status == status {
			return true
		}
	}
	return false
}
