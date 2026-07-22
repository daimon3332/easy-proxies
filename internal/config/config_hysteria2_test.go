package config

import (
	"net/url"
	"testing"
)

func TestParseClashYAML_Hysteria2PortHoppingAndObfs(t *testing.T) {
	content := `proxies:
  - name: "test-hy2"
    type: "hysteria2"
    server: example.com
    ports: 10000-20000
    password: "secret"
    obfs: "salamander"
    obfs-password: "obfs-secret"
    sni: "hy2.example.com"
    skip-cert-verify: true
`

	nodes, err := parseClashYAML(content)
	if err != nil {
		t.Fatalf("parse clash yaml failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}

	u, err := url.Parse(nodes[0].URI)
	if err != nil {
		t.Fatalf("parse generated uri failed: %v", err)
	}
	if u.Scheme != "hysteria2" {
		t.Fatalf("expected scheme hysteria2, got %q", u.Scheme)
	}
	if u.Host != "example.com:443" {
		t.Fatalf("expected host example.com:443, got %q", u.Host)
	}

	query := u.Query()
	if query.Get("ports") != "10000:20000" {
		t.Fatalf("expected ports=10000:20000, got %q", query.Get("ports"))
	}
	if query.Get("obfs") != "salamander" {
		t.Fatalf("expected obfs=salamander, got %q", query.Get("obfs"))
	}
	if query.Get("obfs-password") != "obfs-secret" {
		t.Fatalf("expected obfs-password=obfs-secret, got %q", query.Get("obfs-password"))
	}
	if query.Get("sni") != "hy2.example.com" {
		t.Fatalf("expected sni=hy2.example.com, got %q", query.Get("sni"))
	}
	if query.Get("insecure") != "1" {
		t.Fatalf("expected insecure=1, got %q", query.Get("insecure"))
	}
}

func TestParseClashYAML_FlexibleScalarFields(t *testing.T) {
	content := `proxies:
  - name: "vmess-quoted-scalars"
    type: "vmess"
    server: vmess.example.com
    port: "443"
    uuid: "418048af-a293-4b99-9b0c-98ca3580dd24"
    alterId: "64"
    network: "ws"
    tls: "true"
    udp: "1"
  - name: "trojan-quoted-bool"
    type: "trojan"
    server: trojan.example.com
    port: 8443
    password: "secret"
    skip-cert-verify: "1"
`

	nodes, err := parseClashYAML(content)
	if err != nil {
		t.Fatalf("parse clash yaml failed: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}

	vmessURL, err := url.Parse(nodes[0].URI)
	if err != nil {
		t.Fatalf("parse generated vmess uri failed: %v", err)
	}
	if vmessURL.Query().Get("security") != "tls" {
		t.Fatalf("expected vmess security=tls, got %q", vmessURL.Query().Get("security"))
	}

	trojanURL, err := url.Parse(nodes[1].URI)
	if err != nil {
		t.Fatalf("parse generated trojan uri failed: %v", err)
	}
	if trojanURL.Query().Get("allowInsecure") != "1" {
		t.Fatalf("expected trojan allowInsecure=1, got %q", trojanURL.Query().Get("allowInsecure"))
	}
}
