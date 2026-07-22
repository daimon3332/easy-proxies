package importer

import (
	"context"
	"strings"
	"testing"

	"github.com/sagernet/sing-box/option"
)

func TestNormalizeProbeURL(t *testing.T) {
	tests := []struct {
		name   string
		target string
		want   string
	}{
		{name: "empty default", target: "", want: "https://www.gstatic.com/generate_204"},
		{name: "host port", target: "www.apple.com:80", want: "http://www.apple.com:80/generate_204"},
		{name: "host only", target: "cp.cloudflare.com", want: "http://cp.cloudflare.com/generate_204"},
		{name: "http full path", target: "http://cp.cloudflare.com/generate_204", want: "http://cp.cloudflare.com/generate_204"},
		{name: "https full path", target: "https://cp.cloudflare.com/generate_204", want: "https://cp.cloudflare.com/generate_204"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeProbeURL(tt.target)
			if err != nil {
				t.Fatalf("normalizeProbeURL() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizeProbeURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNodeTesterRecoverPanic(t *testing.T) {
	tester := NewNodeTester(func(tag, uri string, skipCertVerify bool) (option.Outbound, error) {
		panic("bad outbound")
	})

	result := tester.Probe(context.Background(), ManagedNode{ID: "node-1", URI: "vless://example"})
	if result.Error == nil {
		t.Fatal("expected panic to be converted to an error")
	}
	if !strings.Contains(result.Error.Error(), "node test panic: bad outbound") {
		t.Fatalf("unexpected error: %v", result.Error)
	}
}
