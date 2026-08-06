package importer

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

type probePriority uint8

const (
	probePriorityNormal probePriority = iota
	probePriorityHigh
)

type probeTaskFunc func(context.Context) TestResult

type probeWaiter struct {
	ctx    context.Context
	result chan TestResult
}

type probeTask struct {
	key          string
	priority     probePriority
	fn           probeTaskFunc
	timeout      time.Duration
	waiters      []probeWaiter
	nextPriority probePriority
	nextFn       probeTaskFunc
	nextTimeout  time.Duration
	nextWaiters  []probeWaiter
	running      bool
}

type ProbeSchedulerStats struct {
	Capacity     int   `json:"capacity"`
	Workers      int   `json:"workers"`
	Queued       int64 `json:"queued"`
	Running      int64 `json:"running"`
	Deduplicated int64 `json:"deduplicated"`
}

type probeScheduler struct {
	ctx      context.Context
	cancel   context.CancelFunc
	workers  int
	capacity int
	high     chan *probeTask
	normal   chan *probeTask
	slots    chan struct{}

	mu    sync.Mutex
	tasks map[string]*probeTask
	wg    sync.WaitGroup

	queued       atomic.Int64
	running      atomic.Int64
	deduplicated atomic.Int64
	closeOnce    sync.Once
}

func newProbeScheduler(workers, capacity int) *probeScheduler {
	if workers < 1 {
		workers = 1
	}
	if capacity < workers {
		capacity = workers
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &probeScheduler{
		ctx:      ctx,
		cancel:   cancel,
		workers:  workers,
		capacity: capacity,
		high:     make(chan *probeTask, capacity),
		normal:   make(chan *probeTask, capacity),
		slots:    make(chan struct{}, capacity),
		tasks:    make(map[string]*probeTask),
	}
	for i := 0; i < workers; i++ {
		s.wg.Add(1)
		go s.worker()
	}
	return s
}

func (s *probeScheduler) Submit(ctx context.Context, key string, priority probePriority, timeout time.Duration, fn probeTaskFunc) (<-chan TestResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, context.Canceled
	default:
	}
	waiter := probeWaiter{ctx: ctx, result: make(chan TestResult, 1)}
	if s.attachExisting(key, priority, timeout, fn, waiter) {
		return waiter.result, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.ctx.Done():
		return nil, context.Canceled
	case s.slots <- struct{}{}:
	}

	s.mu.Lock()
	if existing := s.tasks[key]; existing != nil {
		s.attachLocked(existing, priority, timeout, fn, waiter)
		s.mu.Unlock()
		<-s.slots
		return waiter.result, nil
	}
	task := &probeTask{
		key:      key,
		priority: priority,
		fn:       fn,
		timeout:  timeout,
		waiters:  []probeWaiter{waiter},
	}
	s.tasks[key] = task
	s.queued.Add(1)
	s.mu.Unlock()
	if !s.enqueue(task) {
		s.mu.Lock()
		delete(s.tasks, key)
		s.queued.Add(-1)
		s.mu.Unlock()
		<-s.slots
		return nil, context.Canceled
	}
	return waiter.result, nil
}

func (s *probeScheduler) attachExisting(key string, priority probePriority, timeout time.Duration, fn probeTaskFunc, waiter probeWaiter) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	task := s.tasks[key]
	if task == nil {
		return false
	}
	s.attachLocked(task, priority, timeout, fn, waiter)
	return true
}

func (s *probeScheduler) attachLocked(task *probeTask, priority probePriority, timeout time.Duration, fn probeTaskFunc, waiter probeWaiter) {
	s.deduplicated.Add(1)
	if task.running {
		task.nextWaiters = append(task.nextWaiters, waiter)
		task.nextFn = fn
		task.nextTimeout = timeout
		if priority > task.nextPriority {
			task.nextPriority = priority
		}
		return
	}
	task.waiters = append(task.waiters, waiter)
}

func (s *probeScheduler) enqueue(task *probeTask) bool {
	queue := s.normal
	if task.priority == probePriorityHigh {
		queue = s.high
	}
	select {
	case <-s.ctx.Done():
		return false
	case queue <- task:
		return true
	}
}

func (s *probeScheduler) worker() {
	defer s.wg.Done()
	for {
		task, ok := s.nextTask()
		if !ok {
			return
		}
		s.run(task)
	}
}

func (s *probeScheduler) nextTask() (*probeTask, bool) {
	select {
	case task := <-s.high:
		return task, true
	default:
	}
	select {
	case <-s.ctx.Done():
		return nil, false
	case task := <-s.high:
		return task, true
	case task := <-s.normal:
		return task, true
	}
}

func (s *probeScheduler) run(task *probeTask) {
	s.mu.Lock()
	if s.tasks[task.key] != task || task.running {
		s.mu.Unlock()
		return
	}
	waiters := activeProbeWaiters(task.waiters)
	if len(waiters) == 0 {
		delete(s.tasks, task.key)
		s.queued.Add(-1)
		s.mu.Unlock()
		<-s.slots
		return
	}
	task.running = true
	s.queued.Add(-1)
	s.running.Add(1)
	fn := task.fn
	timeout := task.timeout
	s.mu.Unlock()

	result := s.execute(waiters, timeout, fn)
	for _, waiter := range waiters {
		select {
		case <-waiter.ctx.Done():
		case waiter.result <- result:
		}
	}

	s.mu.Lock()
	next := activeProbeWaiters(task.nextWaiters)
	if len(next) == 0 || s.ctx.Err() != nil {
		delete(s.tasks, task.key)
		task.running = false
		s.running.Add(-1)
		s.mu.Unlock()
		<-s.slots
		return
	}
	task.running = false
	task.priority = task.nextPriority
	task.fn = task.nextFn
	task.timeout = task.nextTimeout
	task.waiters = next
	task.nextPriority = probePriorityNormal
	task.nextFn = nil
	task.nextTimeout = 0
	task.nextWaiters = nil
	s.running.Add(-1)
	s.queued.Add(1)
	s.mu.Unlock()
	if !s.enqueue(task) {
		s.mu.Lock()
		delete(s.tasks, task.key)
		s.queued.Add(-1)
		s.mu.Unlock()
		for _, waiter := range next {
			select {
			case waiter.result <- TestResult{Error: context.Canceled}:
			default:
			}
		}
		<-s.slots
	}
}

func (s *probeScheduler) execute(waiters []probeWaiter, timeout time.Duration, fn probeTaskFunc) TestResult {
	if timeout <= 0 {
		timeout = DefaultProbeTimeout*2 + 1500*time.Millisecond
	}
	ctx, cancel := context.WithTimeout(s.ctx, timeout)
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if len(activeProbeWaiters(waiters)) == 0 {
					cancel()
					return
				}
			}
		}
	}()
	defer func() {
		close(done)
		cancel()
	}()
	return safeTestResult(func() TestResult { return fn(ctx) })
}

func activeProbeWaiters(waiters []probeWaiter) []probeWaiter {
	active := make([]probeWaiter, 0, len(waiters))
	for _, waiter := range waiters {
		if waiter.ctx.Err() == nil {
			active = append(active, waiter)
		}
	}
	return active
}

func (s *probeScheduler) Stats() ProbeSchedulerStats {
	return ProbeSchedulerStats{
		Capacity:     s.capacity,
		Workers:      s.workers,
		Queued:       s.queued.Load(),
		Running:      s.running.Load(),
		Deduplicated: s.deduplicated.Load(),
	}
}

func (s *probeScheduler) Close() {
	s.closeOnce.Do(func() {
		s.cancel()
		s.wg.Wait()
		s.mu.Lock()
		for _, task := range s.tasks {
			for _, waiter := range append(task.waiters, task.nextWaiters...) {
				select {
				case waiter.result <- TestResult{Error: context.Canceled}:
				default:
				}
			}
		}
		s.tasks = make(map[string]*probeTask)
		s.mu.Unlock()
	})
}
