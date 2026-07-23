package config

import (
	"net/url"
	"testing"
)

func TestParseClashYAMLShadowsocksSimpleObfs(t *testing.T) {
	content := `proxies:
  - name: obfs-node
    type: ss
    server: example.com
    port: 2377
    cipher: chacha20-ietf-poly1305
    password: secret
    udp-over-tcp: true
    plugin: obfs
    plugin-opts:
      mode: tls
      host: cover.example.net:249725
`

	nodes, err := parseClashYAML(content)
	if err != nil {
		t.Fatalf("parseClashYAML() error = %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(nodes))
	}
	parsed, err := url.Parse(nodes[0].URI)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	query := parsed.Query()
	if query.Get("plugin") != "obfs-local" {
		t.Fatalf("plugin = %q, want obfs-local", query.Get("plugin"))
	}
	if query.Get("plugin_opts") != "obfs=tls;obfs-host=cover.example.net:249725" {
		t.Fatalf("plugin_opts = %q", query.Get("plugin_opts"))
	}
	if query.Get("udp-over-tcp") != "1" {
		t.Fatalf("udp-over-tcp = %q, want 1", query.Get("udp-over-tcp"))
	}
}
