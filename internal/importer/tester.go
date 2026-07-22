package importer

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/sagernet/sing-box"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"
)

const (
	DefaultProbeTarget  = "https://www.gstatic.com/generate_204"
	DefaultProbeTimeout = 5 * time.Second
)

type OutboundBuilder func(tag, uri string, skipCertVerify bool) (option.Outbound, error)

type NodeTester struct {
	probeTarget    string
	ipinfoURL      string
	timeout        time.Duration
	concurrency    int
	skipCertVerify bool
	buildOutbound  OutboundBuilder
}

type TesterOption func(*NodeTester)

func NewNodeTester(buildFn OutboundBuilder, opts ...TesterOption) *NodeTester {
	concurrency := runtime.NumCPU() * 2
	if concurrency > 16 {
		concurrency = 16
	}
	if concurrency < 4 {
		concurrency = 4
	}
	t := &NodeTester{
		probeTarget:   DefaultProbeTarget,
		ipinfoURL:     "https://ipinfo.io/json",
		timeout:       DefaultProbeTimeout,
		concurrency:   concurrency,
		buildOutbound: buildFn,
	}
	for _, opt := range opts {
		opt(t)
	}
	if t.concurrency < 1 {
		t.concurrency = 1
	}
	return t
}

func WithProbeTarget(target string) TesterOption {
	return func(t *NodeTester) {
		if target != "" {
			t.probeTarget = target
		}
	}
}

func WithIPInfoURL(u string) TesterOption {
	return func(t *NodeTester) {
		if u != "" {
			t.ipinfoURL = u
		}
	}
}

func WithTesterTimeout(d time.Duration) TesterOption {
	return func(t *NodeTester) {
		if d > 0 {
			t.timeout = d
		}
	}
}

func WithTesterConcurrency(n int) TesterOption {
	return func(t *NodeTester) {
		if n > 0 {
			t.concurrency = n
		}
	}
}

func WithSkipCertVerify(skip bool) TesterOption {
	return func(t *NodeTester) {
		t.skipCertVerify = skip
	}
}

func (t *NodeTester) Test(ctx context.Context, node ManagedNode) (result TestResult) {
	defer recoverTestResult(&result)

	client, closeClient, err := t.clientForNode(ctx, node)
	if err != nil {
		return TestResult{Error: err}
	}
	defer closeClient()

	start := time.Now()
	if err := t.probeWithRetry(ctx, client); err != nil {
		return TestResult{Error: err}
	}
	latency := time.Since(start).Milliseconds()

	countryCtx, cancel := context.WithTimeout(ctx, minDuration(t.timeout/3, 5*time.Second))
	defer cancel()
	countryCode, countryName, _ := t.lookupCountry(countryCtx, client)
	return TestResult{
		LatencyMs:   latency,
		CountryCode: strings.ToUpper(countryCode),
		CountryName: countryName,
	}
}

func (t *NodeTester) Probe(ctx context.Context, node ManagedNode) (result TestResult) {
	defer recoverTestResult(&result)

	client, closeClient, err := t.clientForNode(ctx, node)
	if err != nil {
		return TestResult{Error: err}
	}
	defer closeClient()

	start := time.Now()
	if err := t.probeWithRetry(ctx, client); err != nil {
		return TestResult{Error: err}
	}
	return TestResult{LatencyMs: time.Since(start).Milliseconds()}
}

func (t *NodeTester) LookupCountry(ctx context.Context, node ManagedNode) (result TestResult) {
	defer recoverTestResult(&result)

	client, closeClient, err := t.clientForNode(ctx, node)
	if err != nil {
		return TestResult{Error: err}
	}
	defer closeClient()

	countryCode, countryName, err := t.lookupCountry(ctx, client)
	if err != nil {
		return TestResult{Error: err}
	}
	return TestResult{
		CountryCode: strings.ToUpper(countryCode),
		CountryName: countryName,
	}
}

func recoverTestResult(result *TestResult) {
	if r := recover(); r != nil {
		*result = TestResult{Error: fmt.Errorf("node test panic: %v", r)}
	}
}

func (t *NodeTester) clientForNode(ctx context.Context, node ManagedNode) (*http.Client, func(), error) {
	tag := "test-" + safeTagPart(node.ID)
	outbound, err := t.buildOutbound(tag, node.URI, t.skipCertVerify)
	if err != nil {
		return nil, nil, fmt.Errorf("build outbound: %w", err)
	}

	instance, port, err := startProxyBox(ctx, tag, outbound)
	if err != nil {
		return nil, nil, err
	}

	proxyURL, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	if err != nil {
		_ = instance.Close()
		return nil, nil, err
	}
	client := &http.Client{
		Timeout: t.timeout,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: t.skipCertVerify,
			},
		},
	}
	return client, func() { _ = instance.Close() }, nil
}

func (t *NodeTester) TestBatch(ctx context.Context, nodes []ManagedNode) <-chan NodeTestEvent {
	return t.runBatch(ctx, nodes, t.Test)
}

func (t *NodeTester) ProbeBatch(ctx context.Context, nodes []ManagedNode) <-chan NodeTestEvent {
	return t.runBatch(ctx, nodes, t.Probe)
}

func (t *NodeTester) CountryBatch(ctx context.Context, nodes []ManagedNode) <-chan NodeTestEvent {
	return t.runBatch(ctx, nodes, t.LookupCountry)
}

func (t *NodeTester) runBatch(ctx context.Context, nodes []ManagedNode, fn func(context.Context, ManagedNode) TestResult) <-chan NodeTestEvent {
	events := make(chan NodeTestEvent)
	go func() {
		defer close(events)
		sem := make(chan struct{}, t.concurrency)
		var wg sync.WaitGroup
		for _, node := range nodes {
			select {
			case <-ctx.Done():
				wg.Wait()
				return
			case sem <- struct{}{}:
			}
			wg.Add(1)
			go func(node ManagedNode) {
				defer wg.Done()
				defer func() { <-sem }()
				nodeCtx, cancel := context.WithTimeout(ctx, t.timeout*2+1500*time.Millisecond)
				defer cancel()
				result := safeTestResult(func() TestResult {
					return fn(nodeCtx, node)
				})
				event := NodeTestEvent{NodeID: node.ID, Result: result}
				select {
				case events <- event:
				case <-ctx.Done():
				}
			}(node)
		}
		wg.Wait()
	}()
	return events
}

func safeTestResult(fn func() TestResult) (result TestResult) {
	defer recoverTestResult(&result)
	return fn()
}

func startProxyBox(ctx context.Context, outboundTag string, outbound option.Outbound) (*box.Box, uint16, error) {
	addr := badoption.Addr(netipAddr127())
	inboundTag := "test-in-" + safeTagPart(outboundTag)
	opts := option.Options{
		Log: &option.LogOptions{Level: "error"},
		Inbounds: []option.Inbound{
			{
				Type: C.TypeMixed,
				Tag:  inboundTag,
				Options: &option.HTTPMixedInboundOptions{
					ListenOptions: option.ListenOptions{
						Listen:     &addr,
						ListenPort: 0,
					},
				},
			},
		},
		Outbounds: []option.Outbound{outbound},
		Route:     &option.RouteOptions{Final: outboundTag},
	}
	instance, err := box.New(box.Options{Context: include.Context(ctx), Options: opts})
	if err != nil {
		return nil, 0, fmt.Errorf("create test box: %w", err)
	}
	if err := instance.Start(); err != nil {
		_ = instance.Close()
		return nil, 0, fmt.Errorf("start test box: %w", err)
	}
	port, err := boxInboundPort(instance, inboundTag)
	if err != nil {
		_ = instance.Close()
		return nil, 0, err
	}
	return instance, port, nil
}

func (t *NodeTester) probe(ctx context.Context, client *http.Client) error {
	u, err := normalizeProbeURL(t.probeTarget)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("probe request failed: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("probe got HTTP %d, expected 204", resp.StatusCode)
	}
	return nil
}

func (t *NodeTester) probeWithRetry(ctx context.Context, client *http.Client) error {
	err := t.probe(ctx, client)
	if err == nil || ctx.Err() != nil {
		return err
	}
	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return err
	case <-timer.C:
	}
	if retryErr := t.probe(ctx, client); retryErr != nil {
		return fmt.Errorf("%s; retry failed: %w", classifyProbeError(err), retryErr)
	}
	return nil
}

func classifyProbeError(err error) string {
	if err == nil {
		return ""
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "probe timeout"
	}
	if strings.Contains(strings.ToLower(err.Error()), "timeout") || strings.Contains(strings.ToLower(err.Error()), "deadline exceeded") {
		return "probe timeout"
	}
	return err.Error()
}

func normalizeProbeURL(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		target = DefaultProbeTarget
	}

	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		target = "http://" + target
	}

	u, err := url.Parse(target)
	if err != nil {
		return "", err
	}
	if u.Host == "" {
		return "", fmt.Errorf("invalid probe target %q", target)
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/generate_204"
	}
	return u.String(), nil
}

func (t *NodeTester) lookupIPInfo(ctx context.Context, client *http.Client) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.ipinfoURL, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("ipinfo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		io.Copy(io.Discard, resp.Body)
		return "", "", fmt.Errorf("ipinfo status %d", resp.StatusCode)
	}
	var data struct {
		Country     string `json:"country"`
		CountryName string `json:"country_name"`
		City        string `json:"city"`
		Region      string `json:"region"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&data); err != nil {
		return "", "", fmt.Errorf("decode ipinfo: %w", err)
	}
	name := data.CountryName
	if name == "" {
		name = data.City
	}
	if name == "" {
		name = data.Region
	}
	if name == "" {
		name = strings.ToUpper(data.Country)
	}
	return data.Country, name, nil
}

func (t *NodeTester) lookupCountry(ctx context.Context, client *http.Client) (string, string, error) {
	if code, name, err := t.lookupIPInfo(ctx, client); err == nil {
		return code, name, nil
	}
	if code, name, err := lookupCountryJSON(ctx, client, "http://ip-api.com/json/?fields=status,countryCode,country", func(data map[string]any) (string, string, error) {
		if strings.EqualFold(fmt.Sprint(data["status"]), "success") {
			return fmt.Sprint(data["countryCode"]), fmt.Sprint(data["country"]), nil
		}
		return "", "", fmt.Errorf("ip-api status %v", data["status"])
	}); err == nil {
		return code, name, nil
	}
	return lookupCountryJSON(ctx, client, "https://api.country.is", func(data map[string]any) (string, string, error) {
		code := fmt.Sprint(data["country"])
		if code == "" || code == "<nil>" {
			return "", "", fmt.Errorf("country.is missing country")
		}
		return code, strings.ToUpper(code), nil
	})
}

func lookupCountryJSON(ctx context.Context, client *http.Client, endpoint string, parse func(map[string]any) (string, string, error)) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		io.Copy(io.Discard, resp.Body)
		return "", "", fmt.Errorf("%s status %d", endpoint, resp.StatusCode)
	}
	var data map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&data); err != nil {
		return "", "", err
	}
	code, name, err := parse(data)
	return strings.ToUpper(code), name, err
}

func boxInboundPort(instance *box.Box, tag string) (uint16, error) {
	in, ok := instance.Inbound().Get(tag)
	if !ok {
		return 0, fmt.Errorf("test inbound %s not found", tag)
	}
	v := reflect.ValueOf(in)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	field := v.FieldByName("listener")
	if !field.IsValid() || !field.CanAddr() {
		return 0, fmt.Errorf("test inbound listener unavailable")
	}
	listenerValue := reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem()
	tcpGetter, ok := listenerValue.Interface().(interface{ TCPListener() net.Listener })
	if !ok {
		return 0, fmt.Errorf("test inbound listener unsupported")
	}
	tcpListener := tcpGetter.TCPListener()
	if tcpListener == nil {
		return 0, fmt.Errorf("test inbound tcp listener unavailable")
	}
	tcpAddr, ok := tcpListener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("test inbound address %s is not tcp", tcpListener.Addr())
	}
	return uint16(tcpAddr.Port), nil
}

func safeTagPart(s string) string {
	if len(s) > 24 {
		s = s[:24]
	}
	s = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, s)
	if s == "" {
		return "node"
	}
	return s
}

func netipAddr127() netip.Addr {
	return netip.AddrFrom4([4]byte{127, 0, 0, 1})
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
