package importer

import "testing"

func TestJobEventsCoalesceAndUnsubscribe(t *testing.T) {
	service := &Service{jobEventSubs: make(map[uint64]chan JobEvent)}
	events, unsubscribe := service.SubscribeJobEvents()
	service.publishJobEvent(JobEvent{Kind: "test", ID: "first"})
	service.publishJobEvent(JobEvent{Kind: "test", ID: "latest"})

	if event := <-events; event.ID != "latest" {
		t.Fatalf("event ID = %q, want latest", event.ID)
	}
	stats := service.JobEventStats()
	if stats.Subscribers != 1 || stats.Coalesced != 1 {
		t.Fatalf("stats = %+v, want one subscriber and one coalesced event", stats)
	}
	unsubscribe()
	if subscribers := service.JobEventStats().Subscribers; subscribers != 0 {
		t.Fatalf("subscribers = %d, want 0", subscribers)
	}
}

func TestCloseJobEventsClosesSubscribers(t *testing.T) {
	service := &Service{jobEventSubs: make(map[uint64]chan JobEvent)}
	events, _ := service.SubscribeJobEvents()
	service.closeJobEvents()
	if _, open := <-events; open {
		t.Fatal("event channel remained open")
	}
}
