package proxychain

import "testing"

func TestRouteIDIncludesChainContent(t *testing.T) {
	a := Profile{ID: "same", Hops: []Hop{{URI: "socks5://127.0.0.1:1080"}}}
	b := Profile{ID: "same", Hops: []Hop{{URI: "socks5://127.0.0.1:1081"}}}
	if RouteID("http://example.com:80", &a) == RouteID("http://example.com:80", &b) {
		t.Fatal("route ID must include canonical chain content")
	}
	if RouteID("http://example.com:80", nil) == RouteID("http://example.com:80", &a) {
		t.Fatal("direct and chained routes must have different IDs")
	}
}

func TestNormalizeProfileRejectsDuplicateHop(t *testing.T) {
	_, err := NormalizeProfile(Profile{Hops: []Hop{
		{URI: "socks5://127.0.0.1:1080"},
		{URI: "socks5://127.0.0.1:1080"},
	}})
	if err == nil {
		t.Fatal("expected duplicate hop error")
	}
}
