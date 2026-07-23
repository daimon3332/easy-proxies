package builder

import (
	"testing"

	"github.com/sagernet/sing-box/option"
)

func TestBuildShadowsocksObfsLocalOutbound(t *testing.T) {
	uri := "ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpzZWNyZXQ=@example.com:2377?plugin=obfs-local&plugin_opts=obfs%3Dtls%3Bobfs-host%3Dcover.example.net%3A249725&udp-over-tcp=1#obfs"
	outbound, err := BuildSingleNodeOutbound("test", uri, false)
	if err != nil {
		t.Fatalf("BuildSingleNodeOutbound() error = %v", err)
	}
	opts, ok := outbound.Options.(*option.ShadowsocksOutboundOptions)
	if !ok {
		t.Fatalf("options = %T, want *option.ShadowsocksOutboundOptions", outbound.Options)
	}
	if opts.Plugin != "obfs-local" || opts.PluginOptions != "obfs=tls;obfs-host=cover.example.net:249725" {
		t.Fatalf("plugin = %q, plugin options = %q", opts.Plugin, opts.PluginOptions)
	}
	if opts.UDPOverTCP == nil || !opts.UDPOverTCP.Enabled {
		t.Fatalf("UDPOverTCP = %#v, want enabled", opts.UDPOverTCP)
	}
}
