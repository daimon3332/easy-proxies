package importer

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestServiceCloseCancelsAndWaitsForBackgroundJobs(t *testing.T) {
	service := &Service{
		importCancels:  make(map[string]context.CancelFunc),
		testCancels:    make(map[string]context.CancelFunc),
		refreshCancels: make(map[string]context.CancelFunc),
	}
	started := make(chan struct{})
	finished := make(chan struct{})
	var once sync.Once
	if !service.launchBackground(func(cancel context.CancelFunc) {
		service.importCancelsMu.Lock()
		service.importCancels["job"] = cancel
		service.importCancelsMu.Unlock()
	}, func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		once.Do(func() { close(finished) })
	}) {
		t.Fatal("background job did not start")
	}
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := service.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("Close returned before the job stopped")
	}
	if service.launchBackground(func(context.CancelFunc) {}, func(context.Context) {}) {
		t.Fatal("service accepted a job after Close")
	}
}
