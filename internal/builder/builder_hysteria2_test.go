package builder

import (
	"net/url"
	"testing"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
)

func TestBuildNodeOutbound_Hysteria2PortRangeInRawURI(t *testing.T) {
	outbound, err := buildNodeOutbound("test-hy2", "hysteria2://secret@example.com:10000-20000?sni=hy2.example.com", false)
	if err != nil {
		t.Fatalf("build node outbound failed: %v", err)
	}

	opts, ok := outbound.Options.(*option.Hysteria2OutboundOptions)
	if !ok {
		t.Fatalf("expected *option.Hysteria2OutboundOptions, got %T", outbound.Options)
	}

	if opts.Server != "example.com" {
		t.Fatalf("expected server example.com, got %q", opts.Server)
	}
	if opts.ServerPort != 443 {
		t.Fatalf("expected default server port 443, got %d", opts.ServerPort)
	}
	if len(opts.ServerPorts) != 1 || opts.ServerPorts[0] != "10000:20000" {
		t.Fatalf("expected server ports [10000:20000], got %v", opts.ServerPorts)
	}
}

func TestBuildHysteria2Options_PortsFromQuery(t *testing.T) {
	u, err := url.Parse("hysteria2://secret@example.com:443?ports=10000-20000,30000")
	if err != nil {
		t.Fatalf("parse uri failed: %v", err)
	}

	opts, err := buildHysteria2Options(u, false)
	if err != nil {
		t.Fatalf("build hysteria2 options failed: %v", err)
	}

	if len(opts.ServerPorts) != 2 {
		t.Fatalf("expected 2 server ports, got %d (%v)", len(opts.ServerPorts), opts.ServerPorts)
	}
	if opts.ServerPorts[0] != "10000:20000" || opts.ServerPorts[1] != "30000" {
		t.Fatalf("unexpected server ports: %v", opts.ServerPorts)
	}
}

func TestNormalizeVLESSPacketEncoding(t *testing.T) {
	tests := []struct {
		name      string
		rawQuery  string
		want      string
		wantError bool
	}{
		{name: "missing", rawQuery: "", want: ""},
		{name: "none camel", rawQuery: "packetEncoding=none", want: ""},
		{name: "none snake", rawQuery: "packet_encoding=none", want: ""},
		{name: "xudp", rawQuery: "packetEncoding=xudp", want: "xudp"},
		{name: "packetaddr", rawQuery: "packetEncoding=packetaddr", want: "packetaddr"},
		{name: "bad", rawQuery: "packetEncoding=bad", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values, err := url.ParseQuery(tt.rawQuery)
			if err != nil {
				t.Fatalf("parse query failed: %v", err)
			}

			got, err := normalizeVLESSPacketEncoding(values)
			if tt.wantError {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeVLESSPacketEncoding() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizeVLESSPacketEncoding() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildNodeOutbound_VLESSPacketEncodingNone(t *testing.T) {
	outbound, err := buildNodeOutbound("test-vless", "vless://a42c8b66-d0a7-4b49-9143-b98f1c84edba@example.com:443?security=reality&type=tcp&packetEncoding=none&sni=www.microsoft.com&fp=chrome&flow=xtls-rprx-vision&sid=ce9f790791426d83&pbk=UDLhjunZjP-5A6KBMeuWe3qp_FusLAshcQIcCF7EZh8#test", false)
	if err != nil {
		t.Fatalf("build node outbound failed: %v", err)
	}
	if outbound.Type != C.TypeVLESS {
		t.Fatalf("expected vless outbound, got %q", outbound.Type)
	}
	opts, ok := outbound.Options.(*option.VLESSOutboundOptions)
	if !ok {
		t.Fatalf("expected *option.VLESSOutboundOptions, got %T", outbound.Options)
	}
	if opts.PacketEncoding != nil {
		t.Fatalf("expected packet encoding to be unset for none, got %q", *opts.PacketEncoding)
	}
}
