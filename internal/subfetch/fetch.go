package subfetch

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxBodySize = 10 * 1024 * 1024

var (
	defaultNameServers = []string{
		"223.5.5.5:53",
		"119.29.29.29:53",
		"223.6.6.6:53",
	}
	directDoHServers = []string{
		"https://dns.alidns.com/dns-query",
		"https://doh.pub/dns-query",
	}
	globalDoHServers = []string{
		"https://cloudflare-dns.com/dns-query",
		"https://dns.google/dns-query",
	}
)

type Options struct {
	Timeout       time.Duration
	SkipTLSVerify bool
	ProxyFallback func(context.Context, string, http.Header) ([]byte, error)
}

func Fetch(ctx context.Context, rawURL string, opts Options) ([]byte, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	reqHeaders := defaultHeaders()
	var errs []string

	directTimeout := minDuration(opts.Timeout, 10*time.Second)
	resolvedTimeout := minDuration(opts.Timeout, 6*time.Second)

	body, err := fetchDirect(ctx, rawURL, reqHeaders, directTimeout, opts.SkipTLSVerify)
	if err == nil {
		return body, nil
	}
	errs = append(errs, "direct: "+err.Error())

	if resolvedBody, resolvedErr := fetchViaResolvedIP(ctx, rawURL, reqHeaders, resolvedTimeout, opts.SkipTLSVerify); resolvedErr == nil {
		return resolvedBody, nil
	} else if resolvedErr != nil {
		errs = append(errs, "resolved: "+resolvedErr.Error())
	}

	if opts.ProxyFallback != nil {
		body, err = opts.ProxyFallback(ctx, rawURL, reqHeaders)
		if err == nil {
			return body, nil
		}
		errs = append(errs, "pool: "+err.Error())
	}

	return nil, fmt.Errorf("fetch subscription failed: %s", strings.Join(errs, " | "))
}

func defaultHeaders() http.Header {
	h := make(http.Header)
	h.Set("User-Agent", "clash-verge/v2.2.3")
	h.Set("Accept", "*/*")
	return h
}

func fetchDirect(ctx context.Context, rawURL string, headers http.Header, timeout time.Duration, skipTLSVerify bool) ([]byte, error) {
	client := newHTTPClient(timeout, skipTLSVerify, nil)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header = headers.Clone()
	return doRequest(client, req)
}

func fetchViaResolvedIP(ctx context.Context, rawURL string, headers http.Header, timeout time.Duration, skipTLSVerify bool) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	host := parsed.Hostname()
	if host == "" {
		return nil, fmt.Errorf("missing hostname")
	}
	if net.ParseIP(host) != nil {
		return nil, fmt.Errorf("hostname is already an IP")
	}
	ips, err := resolveHost(ctx, host, timeout)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no resolved IPs for %s", host)
	}
	if len(ips) > 4 {
		ips = ips[:4]
	}

	port := parsed.Port()
	if port == "" {
		if strings.EqualFold(parsed.Scheme, "https") {
			port = "443"
		} else {
			port = "80"
		}
	}

	var errs []string
	for _, ip := range ips {
		dialHost := net.JoinHostPort(ip, port)
		client := newHTTPClient(timeout, skipTLSVerify, &dialTarget{
			Host:       host,
			Port:       port,
			DialHost:   dialHost,
			ServerName: host,
		})
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if reqErr != nil {
			return nil, reqErr
		}
		req.Header = headers.Clone()
		body, fetchErr := doRequest(client, req)
		if fetchErr == nil {
			return body, nil
		}
		errs = append(errs, ip+": "+fetchErr.Error())
	}
	return nil, fmt.Errorf("resolved fetch failed: %s", strings.Join(errs, " | "))
}

type dialTarget struct {
	Host       string
	Port       string
	DialHost   string
	ServerName string
}

func newHTTPClient(timeout time.Duration, skipTLSVerify bool, target *dialTarget) *http.Client {
	dialer := &net.Dialer{
		Timeout:   timeout,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if target == nil {
				return dialer.DialContext(ctx, network, address)
			}
			host, port, err := net.SplitHostPort(address)
			if err == nil && strings.EqualFold(host, target.Host) && port == target.Port {
				address = target.DialHost
			}
			return dialer.DialContext(ctx, network, address)
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: timeout,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: skipTLSVerify,
			ServerName:         serverName(target),
		},
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}

func serverName(target *dialTarget) string {
	if target == nil {
		return ""
	}
	return target.ServerName
}

func doRequest(client *http.Client, req *http.Request) ([]byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return nil, err
	}
	return body, nil
}

func resolveHost(ctx context.Context, host string, timeout time.Duration) ([]string, error) {
	seen := make(map[string]struct{})
	ips := make([]string, 0, 8)
	appendIP := func(ip string) {
		ip = strings.TrimSpace(ip)
		if ip == "" {
			return
		}
		if _, ok := seen[ip]; ok {
			return
		}
		seen[ip] = struct{}{}
		ips = append(ips, ip)
	}

	for _, server := range defaultNameServers {
		for _, ip := range resolveViaPlainDNS(ctx, server, host, timeout) {
			appendIP(ip)
		}
	}
	for _, server := range directDoHServers {
		for _, ip := range resolveViaDoH(ctx, server, host, timeout) {
			appendIP(ip)
		}
	}
	for _, server := range globalDoHServers {
		for _, ip := range resolveViaDoH(ctx, server, host, timeout) {
			appendIP(ip)
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("resolve %s failed via plain DNS and DoH", host)
	}
	return ips, nil
}

func resolveViaPlainDNS(ctx context.Context, serverAddr, host string, timeout time.Duration) []string {
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := &net.Dialer{Timeout: minDuration(timeout, 5*time.Second)}
			if network == "ip" {
				network = "udp"
			}
			return d.DialContext(ctx, network, serverAddr)
		},
	}
	lookupCtx, cancel := context.WithTimeout(ctx, minDuration(timeout, 5*time.Second))
	defer cancel()
	addrs, err := r.LookupIPAddr(lookupCtx, host)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		out = append(out, addr.IP.String())
	}
	return out
}

func resolveViaDoH(ctx context.Context, endpoint, host string, timeout time.Duration) []string {
	values := url.Values{}
	values.Set("name", host)
	values.Set("type", "A")
	dohURL := endpoint
	if strings.Contains(endpoint, "?") {
		dohURL += "&" + values.Encode()
	} else {
		dohURL += "?" + values.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dohURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/dns-json")

	client := newHTTPClient(minDuration(timeout, 8*time.Second), false, nil)
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	var payload struct {
		Answer []struct {
			Type int    `json:"type"`
			Data string `json:"data"`
		} `json:"Answer"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return nil
	}
	out := make([]string, 0, len(payload.Answer))
	for _, answer := range payload.Answer {
		if answer.Type != 1 && answer.Type != 28 {
			continue
		}
		ip := net.ParseIP(strings.TrimSpace(answer.Data))
		if ip == nil {
			continue
		}
		out = append(out, ip.String())
	}
	return out
}

func minDuration(a, b time.Duration) time.Duration {
	if a <= 0 {
		return b
	}
	if a < b {
		return a
	}
	return b
}
