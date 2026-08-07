package importer

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"easy_proxies/internal/config"
)

func TestProbeConnectivityTargetRequiresRealHTMLPage(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, "<html><head><title>Fixture</title></head><body>"+strings.Repeat("page ", 120)+"</body></html>")
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	host, port, err := splitHostPort(parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newSharedProbeRuntime()
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	outbound, ok := runtime.manager.Outbound(probeDirectTag)
	if !ok {
		t.Fatal("probe direct outbound is missing")
	}
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	target := ConnectivityTarget{ID: "fixture", Name: "Fixture", Host: host, Port: port, URL: server.URL}
	passed := probeConnectivityTarget(context.Background(), outbound, ManagedNode{ID: "node"}, target, 1, pool)
	if !passed.Success || passed.Verdict != ConnectivityVerdictUsable || passed.HTTPStatus != http.StatusOK || passed.FinalHost != host || passed.InspectedBytes < 512 || passed.FailureStage != "" || passed.TLSVersion == "" || !passed.FirstSuccess {
		t.Fatalf("trusted result = %#v", passed)
	}
	failed := probeConnectivityTarget(context.Background(), outbound, ManagedNode{ID: "node"}, target, 1, x509.NewCertPool())
	if failed.Success || failed.FailureStage != "tls" || failed.Retryable || failed.Error == "" {
		t.Fatalf("untrusted result = %#v", failed)
	}
}

func TestProbeConnectivityTargetFollowsRedirectWithCookies(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "ok", Path: "/"})
			http.Redirect(w, r, "/page", http.StatusFound)
			return
		}
		cookie, err := r.Cookie("session")
		if err != nil || cookie.Value != "ok" {
			http.Error(w, "missing cookie", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, "<html><title>Fixture</title><body>"+strings.Repeat("fixture ", 100)+"</body></html>")
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	host, port, err := splitHostPort(parsed.Host)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newSharedProbeRuntime()
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	outbound, _ := runtime.manager.Outbound(probeDirectTag)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	target := ConnectivityTarget{ID: "fixture", Name: "Fixture", Host: host, Port: port, URL: server.URL}
	result := probeConnectivityTarget(context.Background(), outbound, ManagedNode{ID: "node"}, target, 1, roots)
	if !result.Success || result.HTTPStatus != http.StatusOK || len(result.Components) != 1 {
		t.Fatalf("redirect result = %#v", result)
	}
}

func TestProbeConnectivityTargetClassifiesHTTPAndContentFailures(t *testing.T) {
	status := http.StatusExpectationFailed
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, "<html><body>"+strings.Repeat("failure ", 100)+"</body></html>")
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	host, port, _ := splitHostPort(parsed.Host)
	runtime, err := newSharedProbeRuntime()
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	outbound, _ := runtime.manager.Outbound(probeDirectTag)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	target := ConnectivityTarget{ID: "fixture", Name: "Fixture", Host: host, Port: port, URL: server.URL}
	result := probeConnectivityTarget(context.Background(), outbound, ManagedNode{ID: "node"}, target, 1, roots)
	if result.Success || result.FailureStage != "http" || result.HTTPStatus != status || !result.Retryable {
		t.Fatalf("HTTP result = %#v", result)
	}

	status = http.StatusOK
	spec := connectivityTargetSpec{
		target:     target,
		components: []connectivityComponentSpec{{id: "page", name: "Fixture", url: server.URL, allowedHosts: []string{host}, marker: "expected-marker"}},
	}
	client, transport := newConnectivityHTTPClient(outbound, roots)
	defer transport.CloseIdleConnections()
	result = probeConnectivityTargetSpecWithClient(context.Background(), client, ManagedNode{ID: "node"}, spec, 1)
	if result.Success || result.FailureStage != "content" || result.Retryable {
		t.Fatalf("content result = %#v", result)
	}
}

func TestOutlookTargetChecksPageOnly(t *testing.T) {
	target, ok := connectivityTargetByID("outlook")
	if !ok {
		t.Fatal("Outlook target not found")
	}
	spec := connectivityTargetSpecFor(target)
	if len(spec.components) != 1 || spec.components[0].id != "page" || spec.components[0].url != "https://outlook.live.com/mail/0/" {
		t.Fatalf("Outlook components = %#v", spec.components)
	}
}

func TestProbeConnectivityTargetBoundsRedirectsAndTimeouts(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/loop" {
			http.Redirect(w, r, "/loop", http.StatusFound)
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	parsed, _ := url.Parse(server.URL)
	host, port, _ := splitHostPort(parsed.Host)
	runtime, err := newSharedProbeRuntime()
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	outbound, _ := runtime.manager.Outbound(probeDirectTag)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	target := ConnectivityTarget{ID: "fixture", Name: "Fixture", Host: host, Port: port, URL: server.URL + "/loop"}
	redirected := probeConnectivityTarget(context.Background(), outbound, ManagedNode{ID: "node"}, target, 1, roots)
	if redirected.Success || redirected.FailureStage != "redirect" || redirected.Retryable {
		t.Fatalf("redirect result = %#v", redirected)
	}

	target.URL = server.URL + "/wait"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	timedOut := probeConnectivityTarget(ctx, outbound, ManagedNode{ID: "node"}, target, 1, roots)
	if timedOut.Success || timedOut.Error != "检测超时" || !timedOut.Retryable {
		t.Fatalf("timeout result = %#v", timedOut)
	}
}

func splitHostPort(address string) (string, uint16, error) {
	host, rawPort, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.ParseUint(rawPort, 10, 16)
	return host, uint16(port), err
}

func TestConnectivityScopesIncludeEveryOwningTag(t *testing.T) {
	svc, store := newBatchServiceForTest(t, &batchNodeManagerStub{})
	nodes := []ManagedNode{
		{ID: "direct", URI: "trojan://direct", TagPrefix: "A", State: StateFailed},
		{ID: "chain", URI: "http://terminal", TagPrefix: "A", ChainProfileID: "front", State: StatePassed, SourceRefs: []NodeSourceRef{{TagPrefix: "A"}, {TagPrefix: "B"}}},
		{ID: "excluded", URI: "trojan://excluded", TagPrefix: "B", State: StateExcluded},
	}
	if err := store.UpsertNodes(nodes); err != nil {
		t.Fatal(err)
	}
	scopes := svc.ConnectivityScopes()
	if len(scopes.Targets) != 4 || len(scopes.Tags) != 2 {
		t.Fatalf("scopes = %#v", scopes)
	}
	counts := map[string]int{}
	for _, scope := range scopes.Tags {
		counts[scope.Tag] = scope.Nodes
	}
	if counts["A"] != 2 || counts["B"] != 1 {
		t.Fatalf("tag counts = %#v", counts)
	}
}

func TestConnectivityPortPreviewAndApplyUseSiteIntersection(t *testing.T) {
	mgr := &batchNodeManagerStub{nextPort: 25000}
	svc, store := newBatchServiceForTest(t, mgr)
	nodes := []ManagedNode{
		{ID: "remove", Name: "remove", URI: "trojan://remove", TagPrefix: "A", State: StateInPool, InPool: true, Port: 24000},
		{ID: "add", Name: "add", URI: "trojan://add", TagPrefix: "A", State: StateFailed, LastError: "旧错误"},
		{ID: "candidate", Name: "candidate", URI: "trojan://candidate", TagPrefix: "A", State: StatePassed},
		{ID: "failed", Name: "failed", URI: "trojan://failed", TagPrefix: "A", State: StateFailed, LastError: "旧的失败原因"},
		{ID: "shared", Name: "shared", URI: "trojan://shared", TagPrefix: "A", SourceRefs: []NodeSourceRef{{TagPrefix: "A"}, {TagPrefix: "B"}}, State: StateInPool, InPool: true, Port: 24001},
		{ID: "other", Name: "other", URI: "trojan://other", TagPrefix: "B", State: StateInPool, InPool: true, Port: 24002},
	}
	if err := store.UpsertNodes(nodes); err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes {
		if node.InPool {
			mgr.configNodes = append(mgr.configNodes, node.ToConfigNode())
		}
	}
	state := &connectivityJobState{
		job:    ConnectivityJob{ID: "job", Status: ConnectivityJobFinished, Tags: []string{"A"}, Targets: []string{"google", "github"}},
		routes: map[string]connectivityRoute{}, results: map[string]ConnectivityResult{},
	}
	for _, node := range nodes[:5] {
		tags := managedNodeTags(node)
		fingerprint := connectivityRouteFingerprint(node)
		state.routes[fingerprint] = connectivityRoute{node: node, tags: tags, fingerprint: fingerprint}
		for _, target := range []string{"google", "github"} {
			success := node.ID == "add"
			state.results[connectivityResultKey(node.ID, target)] = ConnectivityResult{
				NodeID: node.ID, NodeName: node.Name, Tags: tags, RouteFingerprint: fingerprint,
				TargetID: target, FirstSuccess: success, Success: success, Attempts: 1, Error: "检测超时",
			}
		}
	}
	svc.connectivityJobs["job"] = state
	req := ConnectivityPortRequest{JobID: "job", Tags: []string{"A"}, Targets: []string{"google", "github"}}
	preview, err := svc.PreviewConnectivityPool(req)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Qualifying != 1 || preview.NonQualifying != 4 || preview.WillFail != 3 || preview.Added != 1 || preview.Removed != 1 || preview.SharedRetained != 1 || preview.Unaffected != 1 || preview.Stale != 0 {
		t.Fatalf("preview = %#v", preview)
	}
	before, _ := store.GetNode("remove")
	if !before.InPool {
		t.Fatal("preview mutated the pool")
	}
	req.PreviewToken = preview.PreviewToken
	applied, err := svc.ApplyConnectivityPool(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if applied.PoolCount != 3 || len(store.ListPoolNodes()) != 3 {
		t.Fatalf("applied = %#v pool=%d", applied, len(store.ListPoolNodes()))
	}
	removed, _ := store.GetNode("remove")
	added, _ := store.GetNode("add")
	candidate, _ := store.GetNode("candidate")
	failed, _ := store.GetNode("failed")
	shared, _ := store.GetNode("shared")
	other, _ := store.GetNode("other")
	if removed.InPool || removed.State != StateFailed || removed.Port != 0 || !strings.Contains(removed.LastError, "未通过 ") || !strings.Contains(removed.LastError, "检测超时") {
		t.Fatalf("removed = %#v", removed)
	}
	if candidate.State != StateFailed || !strings.Contains(candidate.LastError, "未通过 ") || !strings.Contains(candidate.LastError, "检测超时") {
		t.Fatalf("candidate = %#v", candidate)
	}
	if failed.State != StateFailed || !strings.Contains(failed.LastError, "未通过 ") || failed.LastError == "旧的失败原因" {
		t.Fatalf("failed = %#v", failed)
	}
	if !added.InPool || added.Port == 0 || added.LastError != "" {
		t.Fatalf("added = %#v", added)
	}
	if !shared.InPool || shared.Port != 24001 || other.State != StateInPool || other.Port != 24002 {
		t.Fatalf("shared=%#v other=%#v", shared, other)
	}
}

func TestConnectivityPortApplyRollsBackRuntimeFailure(t *testing.T) {
	mgr := &batchNodeManagerStub{reloadErr: errConnectivityFixture}
	svc, store := newBatchServiceForTest(t, mgr)
	node := ManagedNode{ID: "node", Name: "node", URI: "trojan://node", TagPrefix: "A", State: StateInPool, InPool: true, Port: 24000}
	if err := store.UpsertNode(node); err != nil {
		t.Fatal(err)
	}
	mgr.configNodes = []config.NodeConfig{node.ToConfigNode()}
	fingerprint := connectivityRouteFingerprint(node)
	svc.connectivityJobs["job"] = &connectivityJobState{
		job:    ConnectivityJob{ID: "job", Status: ConnectivityJobFinished, Tags: []string{"A"}, Targets: []string{"google"}},
		routes: map[string]connectivityRoute{fingerprint: {node: node, tags: []string{"A"}, fingerprint: fingerprint}},
		results: map[string]ConnectivityResult{
			connectivityResultKey(node.ID, "google"): {NodeID: node.ID, Tags: []string{"A"}, RouteFingerprint: fingerprint, TargetID: "google", Success: false},
		},
	}
	req := ConnectivityPortRequest{JobID: "job", Tags: []string{"A"}, Targets: []string{"google"}, AllowEmpty: true}
	preview, previewErr := svc.PreviewConnectivityPool(req)
	if previewErr != nil {
		t.Fatal(previewErr)
	}
	req.PreviewToken = preview.PreviewToken
	_, err := svc.ApplyConnectivityPool(context.Background(), req)
	if err == nil {
		t.Fatal("ApplyConnectivityPool() unexpectedly succeeded")
	}
	restored, _ := store.GetNode(node.ID)
	if !restored.InPool || restored.Port != 24000 || len(mgr.configNodes) != 1 || mgr.configNodes[0].Port != 24000 {
		t.Fatalf("store=%#v config=%#v", restored, mgr.configNodes)
	}
}

func TestConnectivityPortPreviewExcludesPartialPageResult(t *testing.T) {
	svc, store := newBatchServiceForTest(t, &batchNodeManagerStub{})
	node := ManagedNode{ID: "partial", Name: "partial", URI: "trojan://partial", TagPrefix: "A", State: StatePassed}
	if err := store.UpsertNode(node); err != nil {
		t.Fatal(err)
	}
	fingerprint := connectivityRouteFingerprint(node)
	svc.connectivityJobs["job"] = &connectivityJobState{
		job:    ConnectivityJob{ID: "job", Status: ConnectivityJobFinished, Tags: []string{"A"}, Targets: []string{"outlook"}},
		routes: map[string]connectivityRoute{fingerprint: {node: node, tags: []string{"A"}, fingerprint: fingerprint}},
		results: map[string]ConnectivityResult{
			connectivityResultKey(node.ID, "outlook"): {
				NodeID: node.ID, Tags: []string{"A"}, RouteFingerprint: fingerprint, TargetID: "outlook",
				Verdict: ConnectivityVerdictPartial, Success: false,
			},
		},
	}
	preview, err := svc.PreviewConnectivityPool(ConnectivityPortRequest{JobID: "job", Tags: []string{"A"}, Targets: []string{"outlook"}})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Qualifying != 0 || !preview.EmptyBlocked {
		t.Fatalf("preview = %#v", preview)
	}
}

func TestConnectivityPortPreviewCountsEachStaleRouteOnce(t *testing.T) {
	svc, _ := newBatchServiceForTest(t, &batchNodeManagerStub{})
	node := ManagedNode{ID: "removed", URI: "trojan://removed", TagPrefix: "A"}
	fingerprint := connectivityRouteFingerprint(node)
	state := &connectivityJobState{
		job:     ConnectivityJob{ID: "job", Status: ConnectivityJobFinished, Tags: []string{"A"}, Targets: []string{"google"}},
		routes:  map[string]connectivityRoute{fingerprint: {node: node, tags: []string{"A"}, fingerprint: fingerprint}},
		results: map[string]ConnectivityResult{},
	}
	for _, target := range connectivityTargets {
		state.results[connectivityResultKey(node.ID, target.ID)] = ConnectivityResult{
			NodeID: node.ID, Tags: []string{"A"}, RouteFingerprint: fingerprint, TargetID: target.ID,
		}
	}
	svc.connectivityJobs["job"] = state

	preview, err := svc.PreviewConnectivityPool(ConnectivityPortRequest{JobID: "job", Tags: []string{"A"}, Targets: []string{"google"}})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Stale != 1 {
		t.Fatalf("stale = %d, want one removed route", preview.Stale)
	}
}

func TestConnectivityPortPreviewRejectsTagOutsideJob(t *testing.T) {
	svc, store := newBatchServiceForTest(t, &batchNodeManagerStub{})
	node := ManagedNode{ID: "node", URI: "trojan://node", TagPrefix: "B", State: StateInPool, InPool: true, Port: 24000}
	if err := store.UpsertNode(node); err != nil {
		t.Fatal(err)
	}
	svc.connectivityJobs["job"] = &connectivityJobState{
		job:    ConnectivityJob{ID: "job", Status: ConnectivityJobFinished, Tags: []string{"A"}, Targets: []string{"google"}},
		routes: map[string]connectivityRoute{}, results: map[string]ConnectivityResult{},
	}

	_, err := svc.PreviewConnectivityPool(ConnectivityPortRequest{JobID: "job", Tags: []string{"B"}, Targets: []string{"google"}, AllowEmpty: true})
	if err == nil || !strings.Contains(err.Error(), "Tag") {
		t.Fatalf("PreviewConnectivityPool() error = %v, want out-of-scope Tag rejection", err)
	}
}

func TestConnectivityErrorRedactionDoesNotExposeProxyCredentials(t *testing.T) {
	node := ManagedNode{URI: "vless://terminal-user:terminal-pass@example.test:443"}
	err := errors.New("build vless://hop-user:hop-secret@front.example.test:443 via " + node.URI)
	message := redactConnectivityError(err, node)
	for _, secret := range []string{"hop-user", "hop-secret", "terminal-user", "terminal-pass", "front.example.test", "example.test"} {
		if strings.Contains(message, secret) {
			t.Fatalf("redacted message %q contains %q", message, secret)
		}
	}
}

func TestConnectivityErrorRedactionClassifiesNetworkFailures(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{fmt.Errorf("dial: %w", syscall.ECONNRESET), "连接被远端重置"},
		{fmt.Errorf("dial: %w", syscall.ECONNREFUSED), "目标拒绝连接"},
		{fmt.Errorf("dial: %w", syscall.ENETUNREACH), "目标网络不可达"},
	}
	for _, test := range tests {
		if got := redactConnectivityError(test.err, ManagedNode{}); got != test.want {
			t.Fatalf("redactConnectivityError(%v) = %q, want %q", test.err, got, test.want)
		}
	}
}

func TestConnectivityJobRetriesOnlyRecoverableFailures(t *testing.T) {
	tester := NewNodeTester(nil, WithTesterConcurrency(3))
	store, err := newTestStore(t, t.TempDir()+"/nodes.json")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(store, tester, &batchNodeManagerStub{})
	node := ManagedNode{ID: "node", Name: "node", URI: "trojan://node", TagPrefix: "A", State: StateFailed}
	if err := store.UpsertNode(node); err != nil {
		t.Fatal(err)
	}
	attempts := make(chan int, 2)
	svc.connectivityProbePass = func(ctx context.Context, nodes []ManagedNode, selected map[string]map[string]struct{}, attempt int) <-chan connectivityProbeEvent {
		events := make(chan connectivityProbeEvent, 4)
		attempts <- attempt
		if attempt == 1 {
			for _, target := range connectivityTargets {
				events <- connectivityProbeEvent{nodeID: node.ID, result: ConnectivityResult{
					NodeID: node.ID, TargetID: target.ID, Attempts: 1, Success: target.ID != "github",
					FirstSuccess: target.ID != "github", Retryable: target.ID == "github",
				}}
			}
		} else {
			if len(selected[node.ID]) != 1 {
				t.Errorf("retry targets = %#v, want only github", selected[node.ID])
			}
			events <- connectivityProbeEvent{nodeID: node.ID, result: ConnectivityResult{NodeID: node.ID, TargetID: "github", Attempts: 2, Success: true}}
		}
		close(events)
		return events
	}
	jobID, err := svc.StartConnectivityJob(ConnectivityStartRequest{Tags: []string{"A"}, Targets: []string{"google", "github", "outlook", "proxyspace"}})
	if err != nil {
		t.Fatal(err)
	}
	job := waitConnectivityJobTerminal(t, svc, jobID)
	if job.Status != ConnectivityJobFinished || job.DoneChecks != 4 || job.RetryChecks != 1 || job.RetryDone != 1 || job.Recovered != 1 {
		t.Fatalf("job = %#v", job)
	}
	if first, second := <-attempts, <-attempts; first != 1 || second != 2 {
		t.Fatalf("attempts = %d, %d", first, second)
	}
	page, err := svc.ListConnectivityResults(ConnectivityResultQuery{JobID: jobID, TargetID: "github"})
	if err != nil || len(page.Items) != 1 || !page.Items[0].Success || page.Items[0].FirstSuccess || page.Items[0].Attempts != 2 {
		t.Fatalf("page=%#v err=%v", page, err)
	}
}

func TestConnectivityJobCancellationStopsProbe(t *testing.T) {
	tester := NewNodeTester(nil, WithTesterConcurrency(1))
	store, err := newTestStore(t, t.TempDir()+"/nodes.json")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(store, tester, &batchNodeManagerStub{})
	if err := store.UpsertNode(ManagedNode{ID: "node", URI: "trojan://node", TagPrefix: "A", State: StateFailed}); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	svc.connectivityProbePass = func(ctx context.Context, _ []ManagedNode, _ map[string]map[string]struct{}, _ int) <-chan connectivityProbeEvent {
		events := make(chan connectivityProbeEvent)
		go func() {
			close(started)
			<-ctx.Done()
			close(events)
		}()
		return events
	}
	jobID, err := svc.StartConnectivityJob(ConnectivityStartRequest{Tags: []string{"A"}})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	if _, err := svc.CancelConnectivityJob(jobID); err != nil {
		t.Fatal(err)
	}
	job := waitConnectivityJobTerminal(t, svc, jobID)
	if job.Status != ConnectivityJobCanceled || job.Phase != string(ConnectivityJobCanceled) {
		t.Fatalf("job = %#v", job)
	}
	if _, err := svc.PreviewConnectivityPool(ConnectivityPortRequest{JobID: jobID, Tags: []string{"A"}, Targets: []string{"google"}}); err == nil {
		t.Fatal("canceled job unexpectedly allowed port preview")
	}
}

func TestJobEventSnapshotIncludesConnectivityJob(t *testing.T) {
	svc, _ := newBatchServiceForTest(t, &batchNodeManagerStub{})
	svc.connectivityJobs["site-job"] = &connectivityJobState{
		job:    ConnectivityJob{ID: "site-job", Status: ConnectivityJobRunning, Tags: []string{"A"}},
		routes: map[string]connectivityRoute{}, results: map[string]ConnectivityResult{},
	}
	for _, event := range svc.JobEventSnapshot() {
		if event.Kind == "connectivity" && event.ID == "site-job" && event.Connectivity != nil {
			return
		}
	}
	t.Fatal("connectivity job missing from event snapshot")
}

func TestConnectivityJobDefaultsToOutlookOnly(t *testing.T) {
	tester := NewNodeTester(nil, WithTesterConcurrency(1))
	store, err := newTestStore(t, t.TempDir()+"/nodes.json")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(store, tester, &batchNodeManagerStub{})
	node := ManagedNode{ID: "node", Name: "node", URI: "trojan://node", TagPrefix: "A", State: StateFailed}
	if err := store.UpsertNode(node); err != nil {
		t.Fatal(err)
	}
	svc.connectivityProbePass = func(_ context.Context, nodes []ManagedNode, selected map[string]map[string]struct{}, attempt int) <-chan connectivityProbeEvent {
		events := make(chan connectivityProbeEvent, 1)
		if attempt != 1 || len(nodes) != 1 || len(selected[node.ID]) != 1 {
			t.Errorf("attempt=%d nodes=%d selected=%#v", attempt, len(nodes), selected)
		}
		if _, ok := selected[node.ID]["outlook"]; !ok {
			t.Errorf("default targets = %#v", selected[node.ID])
		}
		events <- connectivityProbeEvent{nodeID: node.ID, result: ConnectivityResult{NodeID: node.ID, TargetID: "outlook", Success: true, FirstSuccess: true}}
		close(events)
		return events
	}
	jobID, err := svc.StartConnectivityJob(ConnectivityStartRequest{Tags: []string{"A"}})
	if err != nil {
		t.Fatal(err)
	}
	job := waitConnectivityJobTerminal(t, svc, jobID)
	if job.Status != ConnectivityJobFinished || len(job.Targets) != 1 || job.Targets[0] != "outlook" || job.TotalChecks != 1 {
		t.Fatalf("job = %#v", job)
	}
}

func TestConnectivityHistoryComparesRouteMembership(t *testing.T) {
	result := func(route, target string, success bool) ConnectivityResult {
		verdict := ConnectivityVerdictFailed
		if success {
			verdict = ConnectivityVerdictUsable
		}
		return ConnectivityResult{RouteFingerprint: route, NodeName: route, Tags: []string{"A"}, TargetID: target, Verdict: verdict, Success: success}
	}
	previous := map[string]ConnectivityResult{}
	current := map[string]ConnectivityResult{}
	for route, success := range map[string]bool{"same": true, "recover": false, "regress": true, "failed": false, "removed": true} {
		previous[route+"\x00outlook"] = result(route, "outlook", success)
	}
	for route, success := range map[string]bool{"same": true, "recover": true, "regress": false, "failed": false, "new": true} {
		current[route+"\x00outlook"] = result(route, "outlook", success)
	}
	job := ConnectivityJob{ID: "current", Tags: []string{"A"}, Targets: []string{"outlook"}, UpdatedAt: time.Now()}
	comparison := connectivityHistoryComparison(job, current, storedConnectivityRun{ID: "previous", Results: previous}, true)
	want := ConnectivityHistoryCounts{ContinuedSuccess: 1, NewlySuccessful: 1, NewlyFailed: 1, ContinuedUnsuccessful: 1, NoHistory: 1, Removed: 1}
	if comparison.Overall != want || len(comparison.Changes) != 4 {
		t.Fatalf("comparison = %#v, want %#v with four membership or state changes", comparison, want)
	}
}

func TestConnectivityHistoryUsesExactScope(t *testing.T) {
	svc, _ := newBatchServiceForTest(t, &batchNodeManagerStub{})
	result := ConnectivityResult{RouteFingerprint: "route", NodeName: "node", Tags: []string{"A"}, TargetID: "outlook", Verdict: ConnectivityVerdictUsable, Success: true, TestedAt: time.Now()}
	first := ConnectivityJob{ID: "first", Tags: []string{"A"}, Targets: []string{"outlook"}, UpdatedAt: time.Now().Add(-time.Minute)}
	other := ConnectivityJob{ID: "other", Tags: []string{"A"}, Targets: []string{"google"}, UpdatedAt: time.Now()}
	if err := svc.store.db.saveConnectivityRun(first, map[string]ConnectivityResult{"result": result}); err != nil {
		t.Fatal(err)
	}
	if err := svc.store.db.saveConnectivityRun(other, map[string]ConnectivityResult{"result": result}); err != nil {
		t.Fatal(err)
	}
	current := ConnectivityJob{ID: "current", Tags: []string{"A"}, Targets: []string{"outlook"}, UpdatedAt: time.Now().Add(time.Minute)}
	previous, ok, err := svc.store.db.previousConnectivityRun(current)
	if err != nil || !ok || previous.ID != first.ID || len(previous.Results) != 1 {
		t.Fatalf("previous=%#v ok=%t err=%v", previous, ok, err)
	}
}

func TestConnectivityApplyRejectsStalePreviewToken(t *testing.T) {
	svc, store := newBatchServiceForTest(t, &batchNodeManagerStub{})
	node := ManagedNode{ID: "node", Name: "node", URI: "trojan://node", TagPrefix: "A", State: StateFailed}
	if err := store.UpsertNode(node); err != nil {
		t.Fatal(err)
	}
	fingerprint := connectivityRouteFingerprint(node)
	svc.connectivityJobs["job"] = &connectivityJobState{
		job:    ConnectivityJob{ID: "job", Status: ConnectivityJobFinished, Tags: []string{"A"}, Targets: []string{"outlook"}},
		routes: map[string]connectivityRoute{fingerprint: {node: node, tags: []string{"A"}, fingerprint: fingerprint}},
		results: map[string]ConnectivityResult{connectivityResultKey(node.ID, "outlook"): {
			NodeID: node.ID, NodeName: node.Name, Tags: []string{"A"}, RouteFingerprint: fingerprint, TargetID: "outlook", Success: true,
		}},
	}
	req := ConnectivityPortRequest{JobID: "job", Tags: []string{"A"}, Targets: []string{"outlook"}}
	preview, err := svc.PreviewConnectivityPool(req)
	if err != nil {
		t.Fatal(err)
	}
	node.TagPrefix = "B"
	if err := store.UpsertNode(node); err != nil {
		t.Fatal(err)
	}
	req.PreviewToken = preview.PreviewToken
	if _, err := svc.ApplyConnectivityPool(context.Background(), req); err == nil || !strings.Contains(err.Error(), "重新预览") {
		t.Fatalf("ApplyConnectivityPool() error = %v, want stale preview rejection", err)
	}
}

func waitConnectivityJobTerminal(t *testing.T, svc *Service, jobID string) ConnectivityJob {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := svc.GetConnectivityJob(jobID)
		if ok && job.Status != ConnectivityJobRunning {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	job, _ := svc.GetConnectivityJob(jobID)
	t.Fatalf("connectivity job did not finish: %#v", job)
	return ConnectivityJob{}
}

var errConnectivityFixture = errors.New("connectivity fixture failure")
