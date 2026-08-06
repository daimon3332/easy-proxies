package pool

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"
)

func TestBalanceSelectionChecksBoundedCandidates(t *testing.T) {
	p := &poolOutbound{
		mode: modeBalance,
		rng:  rand.New(rand.NewSource(1)),
	}
	p.members = make([]*memberState, 5000)
	for i := range p.members {
		p.members[i] = &memberState{tag: fmt.Sprintf("node-%d", i), shared: &sharedMemberState{}}
	}
	before := p.selectionChecks.Load()
	if _, err := p.pickMember(""); err != nil {
		t.Fatal(err)
	}
	if checks := p.selectionChecks.Load() - before; checks > 16 {
		t.Fatalf("healthy balance selection checked %d members, want at most 16", checks)
	}
}

func TestHalfOpenAllowsOneTrialAndRecoversOnSuccess(t *testing.T) {
	state := &sharedMemberState{blacklisted: true, blacklistedUntil: time.Now().Add(-time.Second)}
	if !state.tryAcquire(time.Now()) {
		t.Fatal("first half-open trial was rejected")
	}
	if state.tryAcquire(time.Now()) {
		t.Fatal("second concurrent half-open trial was allowed")
	}
	state.recordSuccess()
	if !state.tryAcquire(time.Now()) || state.isBlacklisted(time.Now()) {
		t.Fatal("successful half-open trial did not close the circuit")
	}
}

func TestHalfOpenFailureReopensImmediately(t *testing.T) {
	state := &sharedMemberState{blacklisted: true, blacklistedUntil: time.Now().Add(-time.Second)}
	if !state.tryAcquire(time.Now()) {
		t.Fatal("half-open trial was rejected")
	}
	_, triggered, until := state.recordFailure(errors.New("probe failed"), 3, time.Minute)
	if !triggered || until.Before(time.Now()) || state.tryAcquire(time.Now()) {
		t.Fatal("failed half-open trial did not reopen the circuit")
	}
}

func TestUnsupportedNetworkDoesNotConsumeHalfOpenTrial(t *testing.T) {
	state := &sharedMemberState{blacklisted: true, blacklistedUntil: time.Now().Add(-time.Second)}
	pool := &poolOutbound{}
	member := &memberState{shared: state}
	if pool.memberAvailable(member, time.Now(), "udp") {
		t.Fatal("member without UDP support was selected")
	}
	state.mu.Lock()
	halfOpen := state.halfOpen
	state.mu.Unlock()
	if halfOpen {
		t.Fatal("unsupported network consumed the half-open trial")
	}
}

func BenchmarkBalanceSelection(b *testing.B) {
	for _, size := range []int{100, 1000, 5000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			p := &poolOutbound{mode: modeBalance, rng: rand.New(rand.NewSource(1))}
			p.members = make([]*memberState, size)
			for i := range p.members {
				p.members[i] = &memberState{tag: strconv.Itoa(i), shared: &sharedMemberState{}}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := p.pickMember(""); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestRotateModeKeepsMemberWithinIntervalAndSwitchesAfter(t *testing.T) {
	p := &poolOutbound{
		options: Options{RotationInterval: time.Minute},
		mode:    modeRotate,
		members: []*memberState{
			{tag: "node-a", shared: &sharedMemberState{}},
			{tag: "node-b", shared: &sharedMemberState{}},
			{tag: "node-c", shared: &sharedMemberState{}},
		},
	}

	first := p.selectAvailableLocked(time.Now(), "")
	if first.tag != "node-a" {
		t.Fatalf("first = %s, want node-a", first.tag)
	}

	second := p.selectAvailableLocked(time.Now(), "")
	if second.tag != "node-a" {
		t.Fatalf("second = %s, want node-a within interval", second.tag)
	}

	p.rotateSince = time.Now().Add(-2 * time.Minute)
	third := p.selectAvailableLocked(time.Now(), "")
	if third.tag != "node-b" {
		t.Fatalf("third = %s, want node-b after interval", third.tag)
	}
}

func TestRotateModeSwitchesWhenCurrentMemberUnavailable(t *testing.T) {
	p := &poolOutbound{
		options:     Options{RotationInterval: time.Minute},
		mode:        modeRotate,
		rotateTag:   "node-a",
		rotateSince: time.Now(),
		members: []*memberState{
			{tag: "node-a", shared: &sharedMemberState{blacklisted: true, blacklistedUntil: time.Now().Add(time.Minute)}},
			{tag: "node-b", shared: &sharedMemberState{}},
			{tag: "node-c", shared: &sharedMemberState{}},
		},
	}

	selected := p.selectAvailableLocked(time.Now(), "")
	if selected.tag != "node-b" {
		t.Fatalf("selected = %s, want node-b when current is unavailable", selected.tag)
	}
}

func TestProbeHTTPUsesConfiguredURLAndRequires204(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/custom_204" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	dialer := &net.Dialer{}
	dial := func(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
		return dialer.DialContext(ctx, network, destination.String())
	}
	duration, err := probeHTTP(context.Background(), server.URL+"/custom_204", dial)
	if err != nil {
		t.Fatal(err)
	}
	if duration <= 0 {
		t.Fatalf("duration = %s", duration)
	}
}
