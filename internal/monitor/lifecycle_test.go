package monitor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestProbeAllBoundsWorkersAndRejectsDuplicates(t *testing.T) {
	mgr, err := NewManager(Config{ProbeTarget: "http://example.test"})
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop()
	s := NewServer(Config{Enabled: true}, mgr, log.New(io.Discard, "", 0))
	defer s.Shutdown(context.Background())
	started := make(chan struct{}, 100)
	var active, peak atomic.Int32
	for i := 0; i < 100; i++ {
		mgr.Register(NodeInfo{Tag: fmt.Sprint(i)}).SetProbe(func(ctx context.Context) (time.Duration, error) {
			current := active.Add(1)
			defer active.Add(-1)
			for {
				previous := peak.Load()
				if current <= previous || peak.CompareAndSwap(previous, current) {
					break
				}
			}
			started <- struct{}{}
			<-ctx.Done()
			return 0, ctx.Err()
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		s.handleProbeAll(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil).WithContext(ctx))
		close(done)
	}()
	for i := 0; i < min(32, max(10, runtime.NumCPU()*4)); i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("probe workers did not start")
		}
	}
	duplicate := httptest.NewRecorder()
	s.handleProbeAll(duplicate, httptest.NewRequest(http.MethodPost, "/", nil))
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate probe status = %d", duplicate.Code)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("probe workers did not stop after cancellation")
	}
	if peak.Load() > 32 || active.Load() != 0 {
		t.Fatalf("probe peak=%d active=%d", peak.Load(), active.Load())
	}
}

func TestManagerStopWaitsForProbes(t *testing.T) {
	mgr, err := NewManager(Config{ProbeTarget: "http://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	mgr.Register(NodeInfo{Tag: "node"}).SetProbe(func(ctx context.Context) (time.Duration, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		<-release
		return 0, ctx.Err()
	})
	probeDone := make(chan struct{})
	go func() { mgr.ProbeAllNow(time.Minute); close(probeDone) }()
	<-started
	stopped := make(chan struct{})
	go func() { mgr.Stop(); close(stopped) }()
	<-canceled
	select {
	case <-stopped:
		close(release)
		<-probeDone
		t.Fatal("Stop returned with a probe still running")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not finish")
	}
	<-probeDone
	if mgr.StartPeriodicHealthCheck(time.Second, time.Second) {
		t.Fatal("health checks restarted after Stop")
	}
}

func TestServerShutdownCancelsCleanupAndReleasesPort(t *testing.T) {
	mgr, err := NewManager(Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop()
	s := NewServer(Config{Enabled: true}, mgr, log.New(io.Discard, "", 0))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	s.srv.Handler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
	})
	defer close(release)
	serveDone := make(chan struct{})
	go func() { _ = s.srv.Serve(listener); close(serveDone) }()
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		resp, err := http.Get("http://" + listener.Addr().String())
		if err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := s.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown error=%v", err)
	}
	select {
	case <-s.cleanupDone:
	default:
		t.Fatal("session cleanup is still running")
	}
	<-serveDone
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("active HTTP connection remains open")
	}
	rebound, err := net.Listen("tcp", listener.Addr().String())
	if err != nil {
		t.Fatalf("port not released: %v", err)
	}
	_ = rebound.Close()
}

func TestServerStartFailsWhenListenAddressIsBusy(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	mgr, err := NewManager(Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Stop()
	s := NewServer(Config{Enabled: true, Listen: occupied.Addr().String()}, mgr, log.New(io.Discard, "", 0))
	defer s.Close()
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("Start succeeded on an occupied address")
	}
}
