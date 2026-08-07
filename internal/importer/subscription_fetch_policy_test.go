package importer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestImportSubscriptionFallbackDoesNotUseSelectedChainProfile(t *testing.T) {
	mgr := &batchNodeManagerStub{}
	svc, _ := newBatchServiceForTest(t, mgr)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "retry", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := svc.fetchImportSubscription(context.Background(), server.URL, ParseRequest{
		Mode: "url", TagPrefix: "tag", ChainProfileID: "selected-front", FetchPolicy: FetchChainOnly,
	}, time.Second)
	if err == nil {
		t.Fatal("fetchImportSubscription() unexpectedly succeeded")
	}
	if !strings.Contains(err.Error(), "节点池") {
		t.Fatalf("error = %q, want existing pool fallback failure", err)
	}
}
