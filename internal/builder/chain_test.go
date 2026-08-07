package builder

import (
	"testing"

	"github.com/sagernet/sing-box/option"
)

func TestBuildChainOutboundsDetourOrder(t *testing.T) {
	outbounds, err := BuildChainOutbounds("terminal", "http://127.0.0.1:8080", []string{
		"socks5://127.0.0.1:1080",
		"http://127.0.0.1:3128",
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(outbounds) != 3 {
		t.Fatalf("got %d outbounds", len(outbounds))
	}
	for index, want := range []string{"", "terminal-hop-1", "terminal-hop-2"} {
		options, ok := outbounds[index].Options.(interface {
			TakeDialerOptions() option.DialerOptions
		})
		if !ok {
			t.Fatalf("outbound %d has no dialer options", index)
		}
		if got := options.TakeDialerOptions().Detour; got != want {
			t.Fatalf("outbound %d detour = %q, want %q", index, got, want)
		}
	}
}
