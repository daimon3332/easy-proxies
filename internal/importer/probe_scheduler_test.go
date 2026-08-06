package importer

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProbeSchedulerDeduplicatesQueuedTask(t *testing.T) {
	scheduler := newProbeScheduler(1, 4)
	defer scheduler.Close()
	blockStarted := make(chan struct{})
	release := make(chan struct{})
	block, err := scheduler.Submit(context.Background(), "block", probePriorityNormal, time.Second, func(context.Context) TestResult {
		close(blockStarted)
		<-release
		return TestResult{}
	})
	if err != nil {
		t.Fatal(err)
	}
	<-blockStarted
	var executions atomic.Int32
	fn := func(context.Context) TestResult {
		executions.Add(1)
		return TestResult{LatencyMs: 7}
	}
	first, err := scheduler.Submit(context.Background(), "same", probePriorityNormal, time.Second, fn)
	if err != nil {
		t.Fatal(err)
	}
	second, err := scheduler.Submit(context.Background(), "same", probePriorityHigh, time.Second, fn)
	if err != nil {
		t.Fatal(err)
	}
	close(release)
	<-block
	if (<-first).LatencyMs != 7 || (<-second).LatencyMs != 7 {
		t.Fatal("deduplicated waiters did not receive the shared result")
	}
	if executions.Load() != 1 {
		t.Fatalf("executions = %d, want 1", executions.Load())
	}
}

func TestProbeSchedulerCoalescesRunningDuplicatesIntoOneFollowUp(t *testing.T) {
	scheduler := newProbeScheduler(1, 16)
	defer scheduler.Close()
	started := make(chan struct{})
	release := make(chan struct{})
	var executions atomic.Int32
	fn := func(context.Context) TestResult {
		run := executions.Add(1)
		if run == 1 {
			close(started)
			<-release
		}
		return TestResult{LatencyMs: int64(run)}
	}
	first, err := scheduler.Submit(context.Background(), "same", probePriorityNormal, time.Second, fn)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	waiters := make([]<-chan TestResult, 8)
	for i := range waiters {
		waiters[i], err = scheduler.Submit(context.Background(), "same", probePriorityNormal, time.Second, fn)
		if err != nil {
			t.Fatal(err)
		}
	}
	close(release)
	if result := <-first; result.LatencyMs != 1 {
		t.Fatalf("first result = %d", result.LatencyMs)
	}
	for _, waiter := range waiters {
		if result := <-waiter; result.LatencyMs != 2 {
			t.Fatalf("follow-up result = %d", result.LatencyMs)
		}
	}
	if executions.Load() != 2 {
		t.Fatalf("executions = %d, want 2", executions.Load())
	}
}

func TestProbeSchedulerBoundsExecutionAndSkipsCanceledQueue(t *testing.T) {
	const workers = 3
	scheduler := newProbeScheduler(workers, 32)
	defer scheduler.Close()
	var mu sync.Mutex
	active := 0
	peak := 0
	fn := func(context.Context) TestResult {
		mu.Lock()
		active++
		if active > peak {
			peak = active
		}
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		return TestResult{}
	}
	results := make([]<-chan TestResult, 24)
	for i := range results {
		var err error
		results[i], err = scheduler.Submit(context.Background(), strconv.Itoa(i), probePriorityNormal, time.Second, fn)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, result := range results {
		<-result
	}
	if peak > workers {
		t.Fatalf("peak executions = %d, workers = %d", peak, workers)
	}

	blockStarted := make(chan struct{})
	release := make(chan struct{})
	blockers := make([]<-chan TestResult, workers)
	var startedOnce sync.Once
	for i := range blockers {
		blockers[i], _ = scheduler.Submit(context.Background(), "block-"+strconv.Itoa(i), probePriorityNormal, time.Second, func(context.Context) TestResult {
			startedOnce.Do(func() { close(blockStarted) })
			<-release
			return TestResult{}
		})
	}
	<-blockStarted
	ctx, cancel := context.WithCancel(context.Background())
	var canceledExecutions atomic.Int32
	_, err := scheduler.Submit(ctx, "canceled", probePriorityNormal, time.Second, func(context.Context) TestResult {
		canceledExecutions.Add(1)
		return TestResult{}
	})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	close(release)
	for _, blocker := range blockers {
		<-blocker
	}
	time.Sleep(25 * time.Millisecond)
	if canceledExecutions.Load() != 0 {
		t.Fatal("canceled queued task executed")
	}
}
