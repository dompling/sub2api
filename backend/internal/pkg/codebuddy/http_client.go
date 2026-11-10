package codebuddy

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
)

func newProxyTransport(raw string) (*http.Transport, error) {
	_, proxy, err := proxyurl.Parse(raw)
	if err != nil {
		// URL parser errors can contain proxy credentials, including malformed URLs.
		return nil, fmt.Errorf("invalid codebuddy proxy configuration")
	}
	return &http.Transport{Proxy: http.ProxyURL(proxy)}, nil
}

func newCodeBuddyHTTPClient(proxyURL string) (*http.Client, error) {
	client := &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	if strings.TrimSpace(proxyURL) != "" {
		transport, err := newProxyTransport(proxyURL)
		if err != nil {
			return nil, err
		}
		client.Transport = transport
	}
	return client, nil
}
