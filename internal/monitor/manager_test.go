package monitor

import (
	"testing"
	"time"
)

func TestManagerPreservesProbeTarget(t *testing.T) {
	manager, err := NewManager(Config{ProbeTarget: "https://www.gstatic.com/generate_204"})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Stop()
	target, ok := manager.ProbeTarget()
	if !ok || target != "https://www.gstatic.com/generate_204" {
		t.Fatalf("ProbeTarget() = %q, %v", target, ok)
	}
	destination, ok := manager.DestinationForProbe()
	if !ok || destination.Port != 443 {
		t.Fatalf("DestinationForProbe() = %v, %v", destination, ok)
	}
}

func TestManagerMarkAllAvailable(t *testing.T) {
	manager, err := NewManager(Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Stop()
	manager.Register(NodeInfo{Tag: "a"})
	manager.Register(NodeInfo{Tag: "b"})
	manager.MarkAllAvailable()
	for _, snapshot := range manager.Snapshot() {
		if !snapshot.InitialCheckDone || !snapshot.Available {
			t.Fatalf("node %q was not marked available", snapshot.Tag)
		}
	}
}

func TestPeriodicHealthCheckLifecycle(t *testing.T) {
	manager, err := NewManager(Config{ProbeTarget: "https://www.gstatic.com/generate_204"})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Stop()
	if !manager.StartPeriodicHealthCheck(time.Hour, time.Second) {
		t.Fatal("first start did not create scheduler")
	}
	if manager.StartPeriodicHealthCheck(time.Hour, time.Second) {
		t.Fatal("second start created a duplicate scheduler")
	}
	manager.StopPeriodicHealthCheck()
	if !manager.StartPeriodicHealthCheck(time.Hour, time.Second) {
		t.Fatal("scheduler did not restart after stop")
	}
}
