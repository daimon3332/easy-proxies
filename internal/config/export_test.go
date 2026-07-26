package config_test

import (
	"strings"
	"testing"

	"easy_proxies/internal/builder"
	"easy_proxies/internal/config"
)

func TestExportClashYAMLRoundTrip(t *testing.T) {
	nodes := []config.NodeConfig{
		{Name: "vmess", URI: "vmess://11111111-1111-1111-1111-111111111111@example.com:443?security=tls&type=ws&host=cdn.example.com&path=%2Fws&sni=example.com#vmess"},
		{Name: "vless", URI: "vless://22222222-2222-2222-2222-222222222222@example.com:443?encryption=none&security=reality&type=tcp&sni=example.com&fp=chrome&pbk=UDLhjunZjP-5A6KBMeuWe3qp_FusLAshcQIcCF7EZh8&sid=01234567#vless"},
		{Name: "trojan", URI: "trojan://password@example.com:443?type=ws&host=cdn.example.com&path=%2Ftrojan&sni=example.com#trojan"},
		{Name: "ss", URI: "ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ=@example.com:8388?plugin=obfs-local&plugin_opts=obfs%3Dtls%3Bobfs-host%3Dcdn.example.com&udp-over-tcp=1#ss"},
		{Name: "hy2", URI: "hysteria2://password@example.com:443?sni=example.com&obfs=salamander&obfs-password=secret&ports=10000%3A20000#hy2"},
		{Name: "tuic", URI: "tuic://33333333-3333-3333-3333-333333333333:password@example.com:443?sni=example.com&congestion_control=bbr&udp_relay_mode=native&alpn=h3#tuic"},
		{Name: "anytls", URI: "anytls://password@example.com:443?sni=example.com&fp=chrome#anytls"},
	}
	data, err := config.ExportClashYAML(nodes)
	if err != nil {
		t.Fatalf("ExportClashYAML() error = %v", err)
	}
	restored, err := config.ParseSubscriptionContent(string(data))
	if err != nil {
		t.Fatalf("ParseSubscriptionContent() error = %v\n%s", err, data)
	}
	if len(restored) != len(nodes) {
		t.Fatalf("restored node count = %d, want %d\n%s", len(restored), len(nodes), data)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != len(nodes)+1 || lines[0] != "proxies:" {
		t.Fatalf("YAML should contain one flow-style proxy per line:\n%s", data)
	}
	for _, line := range lines[1:] {
		if !strings.HasPrefix(strings.TrimSpace(line), "- {") {
			t.Fatalf("proxy is not a single flow-style line: %q", line)
		}
	}
	for _, node := range restored {
		if _, err := builder.BuildSingleNodeOutbound(node.Name, node.URI, false); err != nil {
			t.Errorf("round-trip node %q is invalid: %v\nURI: %s", node.Name, err, node.URI)
		}
	}
}

func TestExportClashYAMLRejectsUnsupportedProtocol(t *testing.T) {
	_, err := config.ExportClashYAML([]config.NodeConfig{{Name: "unsupported", URI: "ssh://example.com:22"}})
	if err == nil {
		t.Fatal("ExportClashYAML() expected error")
	}
}
