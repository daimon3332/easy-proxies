package geoip

import (
	"fmt"
	"testing"
	"time"
)

func TestDNSCacheIsBoundedAndExpires(t *testing.T) {
	lookup := &Lookup{dnsCache: make(map[string]dnsCacheEntry)}
	now := time.Now()
	for i := 0; i < maxDNSCacheEntries+10; i++ {
		lookup.storeDNSCache(fmt.Sprintf("node-%d.example", i), RegionInfo{Code: RegionOther}, now.Add(time.Hour))
	}
	if len(lookup.dnsCache) > maxDNSCacheEntries {
		t.Fatalf("DNS cache entries = %d, max %d", len(lookup.dnsCache), maxDNSCacheEntries)
	}
	lookup.storeDNSCache("expired.example", RegionInfo{Code: RegionUS}, now.Add(-time.Second))
	if _, ok := lookup.loadDNSCache("expired.example", now); ok {
		t.Fatal("expired DNS cache entry was returned")
	}
}
