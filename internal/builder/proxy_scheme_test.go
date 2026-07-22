package builder

import (
	"testing"

	C "github.com/sagernet/sing-box/constant"
)

func TestBuildSingleNodeOutboundProxySchemes(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		want string
	}{
		{name: "http", uri: "http://127.0.0.1:8080", want: C.TypeHTTP},
		{name: "https", uri: "https://127.0.0.1:8443", want: C.TypeHTTP},
		{name: "socks5", uri: "socks5://127.0.0.1:1080", want: C.TypeSOCKS},
		{name: "socks4", uri: "socks4://127.0.0.1:1080", want: C.TypeSOCKS},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := BuildSingleNodeOutbound("test", tt.uri, false)
			if err != nil {
				t.Fatalf("BuildSingleNodeOutbound() error = %v", err)
			}
			if out.Type != tt.want {
				t.Fatalf("BuildSingleNodeOutbound() type = %q, want %q", out.Type, tt.want)
			}
		})
	}
}
