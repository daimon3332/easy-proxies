package pool

import (
	"testing"
	"time"
)

func TestRotateModeKeepsMemberWithinIntervalAndSwitchesAfter(t *testing.T) {
	p := &poolOutbound{
		options: Options{RotationInterval: time.Minute},
		members: []*memberState{
			{tag: "node-a"},
			{tag: "node-b"},
			{tag: "node-c"},
		},
	}
	candidates := p.members

	first := p.selectRotateMember(candidates)
	if first.tag != "node-a" {
		t.Fatalf("first = %s, want node-a", first.tag)
	}

	second := p.selectRotateMember(candidates)
	if second.tag != "node-a" {
		t.Fatalf("second = %s, want node-a within interval", second.tag)
	}

	p.rotateSince = time.Now().Add(-2 * time.Minute)
	third := p.selectRotateMember(candidates)
	if third.tag != "node-b" {
		t.Fatalf("third = %s, want node-b after interval", third.tag)
	}
}

func TestRotateModeSwitchesWhenCurrentMemberUnavailable(t *testing.T) {
	p := &poolOutbound{
		options:     Options{RotationInterval: time.Minute},
		rotateTag:   "node-a",
		rotateSince: time.Now(),
		members: []*memberState{
			{tag: "node-a"},
			{tag: "node-b"},
			{tag: "node-c"},
		},
	}

	selected := p.selectRotateMember([]*memberState{p.members[1], p.members[2]})
	if selected.tag != "node-b" {
		t.Fatalf("selected = %s, want node-b when current is unavailable", selected.tag)
	}
}
