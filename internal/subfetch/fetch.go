package subfetch

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxBodySize = 10 * 1024 * 1024

var ErrBodyTooLarge = errors.New("subscription response exceeds the maximum size")

type HTTPStatusError struct {
	Code int
}

func (e *HTTPStatusError) Error() string { return fmt.Sprintf("HTTP %d", e.Code) }

func (e *HTTPStatusError) Retryable() bool {
	return e.Code == http.StatusRequestTimeout || e.Code == http.StatusTooManyRequests || e.Code >= 500
}

type Options struct {
	Timeout       time.Duration
	SkipTLSVerify bool
	ProxyFallback func(context.Context, string, http.Header) ([]byte, error)
}

func Fetch(ctx context.Context, rawURL string, opts Options) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	if err := validateURL(rawURL); err != nil {
		return nil, err
	}

	headers := defaultHeaders()
	client := newHTTPClient(opts.Timeout, opts.SkipTLSVerify, nil)
	if transport, ok := client.Transport.(*http.Transport); ok {
		defer transport.CloseIdleConnections()
	}
	body, err := fetchDirect(ctx, rawURL, headers, client)
	if err == nil {
		return body, nil
	}
	if !isRetryable(err) || opts.ProxyFallback == nil || ctx.Err() != nil {
		return nil, sanitizeError(err, rawURL)
	}

	body, fallbackErr := opts.ProxyFallback(ctx, rawURL, headers)
	if fallbackErr == nil {
		if len(body) > maxBodySize {
			return nil, ErrBodyTooLarge
		}
		return body, nil
	}
	return nil, fmt.Errorf("subscription fetch failed: direct: %s; fallback: %s", sanitizeError(err, rawURL), sanitizeError(fallbackErr, rawURL))
}

func validateURL(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("invalid subscription URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("subscription URL must use http or https")
	}
	if parsed.Hostname() == "" {
		return fmt.Errorf("subscription URL hostname is empty")
	}
	return nil
}

func defaultHeaders() http.Header {
	h := make(http.Header)
	h.Set("User-Agent", "easy_proxies/2.1")
	h.Set("Accept", "*/*")
	return h
}

func fetchDirect(ctx context.Context, rawURL string, headers http.Header, client *http.Client) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header = headers.Clone()
	return doRequest(client, req)
}

func newHTTPClient(timeout time.Duration, skipTLSVerify bool, target *dialTarget) *http.Client {
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if target != nil {
				host, port, err := net.SplitHostPort(address)
				if err == nil && strings.EqualFold(host, target.Host) && port == target.Port {
					address = target.DialHost
				}
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
	return &http.Client{Timeout: timeout, Transport: transport}
}

type dialTarget struct {
	Host       string
	Port       string
	DialHost   string
	ServerName string
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
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, &HTTPStatusError{Code: resp.StatusCode}
	}

	limited := io.LimitReader(resp.Body, maxBodySize+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(body) > maxBodySize {
		return nil, ErrBodyTooLarge
	}
	return body, nil
}

func isRetryable(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, ErrBodyTooLarge) {
		return false
	}
	var statusErr *HTTPStatusError
	if errors.As(err, &statusErr) {
		return statusErr.Retryable()
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded)
}

func sanitizeError(err error, rawURL string) error {
	if err == nil {
		return nil
	}
	message := strings.ReplaceAll(err.Error(), rawURL, "subscription URL")
	if parsed, parseErr := url.Parse(rawURL); parseErr == nil {
		message = strings.ReplaceAll(message, parsed.Host, "subscription host")
	}
	if message == err.Error() {
		return err
	}
	return &sanitizedError{message: message, cause: err}
}

type sanitizedError struct {
	message string
	cause   error
}

func (e *sanitizedError) Error() string { return e.message }
func (e *sanitizedError) Unwrap() error { return e.cause }
