package membership

import (
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestAdvertisedEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		nodeID   string
		bindAddr string
		seeds    []string
		want     string
	}{
		{
			name:   "matching self seed is canonical",
			nodeID: "node1", bindAddr: "0.0.0.0:7000",
			seeds: []string{"node2:7000", "node1:7100"}, want: "node1:7100",
		},
		{
			name:   "wildcard bind derives node address",
			nodeID: "node1", bindAddr: "0.0.0.0:7000",
			want: "node1:7000",
		},
		{
			name:   "concrete bind is directly advertised",
			nodeID: "node1", bindAddr: "127.0.0.1:8123",
			want: "127.0.0.1:8123",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := advertisedEndpoint(tt.nodeID, tt.bindAddr, tt.seeds); got != tt.want {
				t.Fatalf("advertised endpoint=%q, want %q", got, tt.want)
			}
		})
	}
}

func TestDisseminatedMembershipIsBoundedAndIncludesSelf(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	b := NewBootstrapper("node0", "127.0.0.1:7000", nil, time.Second, 3, logger)
	for i := 1; i <= maxDisseminatedMembership+20; i++ {
		b.table.ObserveAlive(
			fmt.Sprintf("node%d", i),
			fmt.Sprintf("127.0.0.1:%d", 7000+i),
			uint64(i),
		)
	}

	entries := b.disseminatedMembership()
	if len(entries) != maxDisseminatedMembership {
		t.Fatalf("membership batch size=%d, want %d", len(entries), maxDisseminatedMembership)
	}
	foundSelf := false
	for _, entry := range entries {
		if entry.NodeID == "node0" {
			foundSelf = true
			break
		}
	}
	if !foundSelf {
		t.Fatal("bounded membership batch must always include self")
	}
}
