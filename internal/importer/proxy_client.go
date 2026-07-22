package importer

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

func NewHTTPClientForURI(ctx context.Context, buildFn OutboundBuilder, nodeID, uri string, timeout time.Duration, skipCertVerify bool) (*http.Client, func(), error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	tag := "fetch-" + safeTagPart(nodeID)
	outbound, err := buildFn(tag, uri, skipCertVerify)
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
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: skipCertVerify,
			},
		},
	}
	return client, func() { _ = instance.Close() }, nil
}
