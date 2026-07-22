package monitor

import (
	"strings"
	"testing"
)

func TestUpdateSettingsRejectsUnsupportedProbeTarget(t *testing.T) {
	s := &Server{}
	err := s.updateSettings("", "https://example.com/generate_204", false, nil, false)
	if err == nil {
		t.Fatal("expected unsupported probe target error")
	}
	if !strings.Contains(err.Error(), "探测目标只支持") {
		t.Fatalf("unexpected error: %v", err)
	}
}
