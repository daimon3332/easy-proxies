package monitor

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpdateSettingsRejectsUnsupportedProbeTarget(t *testing.T) {
	s := &Server{}
	err := s.updateSettings("", "https://example.com/generate_204", false, nil, false)
	if err == nil {
		t.Fatal("expected unsupported probe target error")
	}
	if !strings.Contains(err.Error(), "探测目标只支持") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleFavicon(t *testing.T) {
	recorder := httptest.NewRecorder()
	(&Server{}).handleFavicon(recorder, httptest.NewRequest(http.MethodGet, "/favicon.svg", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "image/svg+xml" {
		t.Fatalf("unexpected content type: %q", contentType)
	}
	if cacheControl := recorder.Header().Get("Cache-Control"); cacheControl != "public, max-age=86400" {
		t.Fatalf("unexpected cache control: %q", cacheControl)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "<svg") || !strings.Contains(body, "Easy Proxies") {
		t.Fatalf("unexpected favicon body: %q", body)
	}
}
