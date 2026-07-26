package monitor

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"io"
	"reflect"
	"strings"
	"testing"

	"easy_proxies/internal/config"
	"easy_proxies/internal/importer"
)

func TestJoinBase64NodeURIsEncodesEachNodeOnItsOwnLine(t *testing.T) {
	nodes := []config.NodeConfig{{URI: "trojan://one"}, {URI: "ss://two"}}
	lines := strings.Split(strings.TrimSpace(joinBase64NodeURIs(nodes)), "\n")
	if len(lines) != len(nodes) {
		t.Fatalf("line count = %d, want %d", len(lines), len(nodes))
	}
	for i, line := range lines {
		decoded, err := base64.StdEncoding.DecodeString(line)
		if err != nil || string(decoded) != nodes[i].URI {
			t.Fatalf("line %d decoded to %q, err=%v", i, decoded, err)
		}
	}
}

func TestSubscriptionURLsByTags(t *testing.T) {
	nodes := []importer.ManagedNode{
		{TagPrefix: "work", ImportMode: "url", ImportSource: "https://example.com/a"},
		{TagPrefix: "work", ImportMode: "url", ImportSource: "https://example.com/a\nhttps://example.com/b"},
		{TagPrefix: "home", ImportMode: "url", ImportSource: "https://example.com/c"},
		{TagPrefix: "work", ImportMode: "content", ImportSource: "content"},
	}
	got := subscriptionURLsByTags(nodes, normalizedSet([]string{"work"}))
	want := []string{"https://example.com/a", "https://example.com/b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("subscriptionURLsByTags() = %#v, want %#v", got, want)
	}
}

func TestMatchesNodeExportSelectionByTag(t *testing.T) {
	node := importer.ManagedNode{ID: "node-1", TagPrefix: "work"}
	if !matchesNodeExportSelection(node, false, nil, normalizedSet([]string{"work"})) {
		t.Fatal("matching Tag should select the node")
	}
	if matchesNodeExportSelection(node, false, nil, normalizedSet([]string{"home"})) {
		t.Fatal("different Tag should not select the node")
	}
	if !matchesNodeExportSelection(node, false, normalizedSet([]string{"node-1"}), normalizedSet([]string{"home"})) {
		t.Fatal("explicit node ID should take precedence for node-page export")
	}
}

func TestBuildTagExportFilesUsesOriginalSourceType(t *testing.T) {
	nodes := []importer.ManagedNode{
		{ID: "sub-1", TagPrefix: "sub", ImportMode: "url", ImportSource: "https://example.com/a", URI: "trojan://one"},
		{ID: "sub-2", TagPrefix: "sub", ImportMode: "url", ImportSource: "https://example.com/b", URI: "trojan://two"},
		{ID: "uri-1", TagPrefix: "uri", ImportMode: "content", ImportFormat: "uri_list", URI: "trojan://uri-one"},
		{ID: "base64-1", TagPrefix: "base64", ImportMode: "content", ImportFormat: "base64", URI: "trojan://base64-one"},
		{ID: "base64-2", TagPrefix: "base64", ImportMode: "content", ImportFormat: "base64", URI: "trojan://base64-two"},
		{ID: "yaml-1", TagPrefix: "yaml", ImportMode: "content", ImportFormat: "clash_yaml", Name: "yaml-one", URI: "trojan://password@example.com:443?sni=example.com#yaml-one"},
	}
	files, err := buildTagExportFiles(nodes, normalizedSet([]string{"sub", "uri", "base64", "yaml"}))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 4 {
		t.Fatalf("file count = %d, want 4", len(files))
	}
	byName := make(map[string]tagExportFile, len(files))
	for _, file := range files {
		byName[file.Name] = file
	}
	if got := string(byName["sub_subscription_urls.txt"].Data); got != "https://example.com/a\nhttps://example.com/b\n" {
		t.Fatalf("subscription export = %q", got)
	}
	if got := string(byName["uri_uris.txt"].Data); got != "trojan://uri-one\n" {
		t.Fatalf("URI export = %q", got)
	}
	encoded := strings.TrimSpace(string(byName["base64_base64.txt"].Data))
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || string(decoded) != "trojan://base64-one\ntrojan://base64-two\n" {
		t.Fatalf("Base64 export decoded to %q, err=%v", decoded, err)
	}
	yamlNodes, err := config.ParseSubscriptionContent(string(byName["yaml_clash.yaml"].Data))
	if err != nil || len(yamlNodes) != 1 {
		t.Fatalf("YAML export nodes=%d, err=%v", len(yamlNodes), err)
	}
}

func TestBuildTagExportZIPIncludesEveryFile(t *testing.T) {
	files := []tagExportFile{
		{Name: "one.txt", Data: []byte("one\n")},
		{Name: "two.txt", Data: []byte("two\n")},
	}
	data, err := buildTagExportZIP(files)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if len(zr.File) != len(files) {
		t.Fatalf("ZIP entries = %d, want %d", len(zr.File), len(files))
	}
	for i, entry := range zr.File {
		reader, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil || entry.Name != files[i].Name || !bytes.Equal(content, files[i].Data) {
			t.Fatalf("ZIP entry %d name=%q content=%q err=%v", i, entry.Name, content, err)
		}
	}
}

func TestBuildTagExportFilesRejectsEmptyTagData(t *testing.T) {
	nodes := []importer.ManagedNode{{ID: "empty", TagPrefix: "empty", ImportMode: "content", ImportFormat: "uri_list"}}
	if _, err := buildTagExportFiles(nodes, normalizedSet([]string{"empty"})); err == nil {
		t.Fatal("buildTagExportFiles() should reject a Tag without exportable data")
	}
}
