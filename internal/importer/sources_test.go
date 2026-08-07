package importer

import "testing"

func TestURLSourceIdentityIgnoresLegacyFetchPolicy(t *testing.T) {
	direct := NodeSourceRef{TagPrefix: "tag", Mode: "url", Source: "https://example.test/sub", ChainProfileID: "front", FetchPolicy: FetchDirect}
	chainOnly := direct
	chainOnly.FetchPolicy = FetchChainOnly

	if got, want := sourceRefIdentity(direct), sourceRefIdentity(chainOnly); got != want {
		t.Fatalf("source identities differ: %q != %q", got, want)
	}
	if got := normalizeSourceRef(chainOnly).FetchPolicy; got != FetchAuto {
		t.Fatalf("normalized fetch policy = %q, want %q", got, FetchAuto)
	}
}

func TestNormalizeFetchPolicyAlwaysUsesAutomaticPoolFallback(t *testing.T) {
	for _, policy := range []string{"", FetchDirect, FetchAuto, FetchChainOnly, "unknown"} {
		if got := normalizeFetchPolicy(policy); got != FetchAuto {
			t.Fatalf("normalizeFetchPolicy(%q) = %q, want %q", policy, got, FetchAuto)
		}
	}
}
