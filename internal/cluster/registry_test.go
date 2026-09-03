package cluster

import (
	"testing"
	"time"
)

func TestRegistryHeartbeatAndStale(t *testing.T) {
	reg := NewRegistry()
	reg.UpsertRegistration(Registration{NodeID: "n1", AdvertiseURL: "http://slave:8317", Version: "dev"})
	reg.UpsertHeartbeat(Heartbeat{NodeID: "n1", AppliedHash: "abc", AuthFileCount: 2}, "")
	nodes := reg.Snapshot(time.Hour)
	if len(nodes) != 1 {
		t.Fatalf("len(nodes)=%d", len(nodes))
	}
	if nodes[0].Status != NodeStatusOnline {
		t.Fatalf("status=%s", nodes[0].Status)
	}
	if nodes[0].AuthFileCount != 2 || nodes[0].AppliedHash != "abc" {
		t.Fatalf("heartbeat fields: %+v", nodes[0])
	}

	reg.SetError("n1", "push failed")
	nodes = reg.Snapshot(time.Hour)
	if nodes[0].Status != NodeStatusError {
		t.Fatalf("expected error status, got %s", nodes[0].Status)
	}

	reg.mu.Lock()
	reg.nodes["n1"].LastSeen = time.Now().Add(-time.Hour)
	reg.mu.Unlock()
	nodes = reg.Snapshot(time.Minute)
	if nodes[0].Status != NodeStatusStale {
		t.Fatalf("expected stale status, got %s", nodes[0].Status)
	}

	reg.Remove("n1")
	if got := reg.Snapshot(time.Hour); len(got) != 0 {
		t.Fatalf("expected empty after remove, got %d", len(got))
	}
}

func TestHeartbeatClearsErrorOnceHashMatches(t *testing.T) {
	reg := NewRegistry()
	reg.UpsertRegistration(Registration{NodeID: "n1", AdvertiseURL: "http://slave:8317"})
	reg.SetError("n1", "push failed")

	// Still behind: the earlier failure remains visible.
	reg.UpsertHeartbeat(Heartbeat{NodeID: "n1", AppliedHash: "old"}, "target")
	if got := reg.Snapshot(time.Hour); got[0].Status != NodeStatusError {
		t.Fatalf("status=%s, want error while the node is behind", got[0].Status)
	}

	// The node caught up on its own, so the stale push error must go away.
	reg.UpsertHeartbeat(Heartbeat{NodeID: "n1", AppliedHash: "target"}, "target")
	nodes := reg.Snapshot(time.Hour)
	if nodes[0].Status != NodeStatusOnline {
		t.Fatalf("status=%s, want online after the hashes match", nodes[0].Status)
	}
	if nodes[0].LastError != "" {
		t.Fatalf("last_error=%q, want cleared", nodes[0].LastError)
	}
}

func TestSnapshotUsesNodeReportedHeartbeatInterval(t *testing.T) {
	reg := NewRegistry()
	// The slave reports every 60s while the master's own interval is 15s. Judging
	// the node against the master's interval would mark it stale immediately.
	reg.UpsertHeartbeat(Heartbeat{NodeID: "slow", HeartbeatIntervalSeconds: 60}, "")
	reg.mu.Lock()
	reg.nodes["slow"].LastSeen = time.Now().UTC().Add(-40 * time.Second)
	reg.mu.Unlock()

	nodes := reg.Snapshot(45 * time.Second)
	if nodes[0].Status != NodeStatusOnline {
		t.Fatalf("status=%s, want online within three reported intervals", nodes[0].Status)
	}

	reg.mu.Lock()
	reg.nodes["slow"].LastSeen = time.Now().UTC().Add(-200 * time.Second)
	reg.mu.Unlock()
	if got := reg.Snapshot(45 * time.Second); got[0].Status != NodeStatusStale {
		t.Fatalf("status=%s, want stale past three reported intervals", got[0].Status)
	}
}
