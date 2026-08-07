package importer

import (
	"strings"
	"testing"
)

func TestParseHostPortList(t *testing.T) {
	nodes, err := parseHostPortList("example.com:80\nuser:pass@[2001:db8::1]:1080", "http")
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || nodes[0].URI != "http://example.com:80" || !strings.HasPrefix(nodes[1].URI, "http://user:pass@[") {
		t.Fatalf("unexpected nodes: %#v", nodes)
	}
}

func TestParseHostPortListDoesNotEchoCredentials(t *testing.T) {
	_, err := parseHostPortList("secret-user@invalid", "socks5")
	if err == nil {
		t.Fatal("expected parse error")
	}
	if strings.Contains(err.Error(), "secret-user") {
		t.Fatal("parse error leaked credentials")
	}
}
