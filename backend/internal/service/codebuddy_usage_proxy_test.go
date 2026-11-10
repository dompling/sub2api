//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

type codeBuddyUsageTransportFunc func(*http.Request) (*http.Response, error)

func (f codeBuddyUsageTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestCodeBuddyUsageDoesNotBypassConfiguredProxy(t *testing.T) {
	var directCalls atomic.Int32
	previous := http.DefaultTransport
	http.DefaultTransport = codeBuddyUsageTransportFunc(func(r *http.Request) (*http.Response, error) {
		directCalls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"Response":{"Data":{"Accounts":[]}}}}`))}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })

	for _, missing := range []bool{false, true} {
		account := &Account{ID: 73, Platform: PlatformCodeBuddy, Type: AccountTypeOAuth,
			Credentials: map[string]any{"access_token": "synthetic-token", "uid": "synthetic-user"}}
		proxyID := int64(9)
		account.ProxyID = &proxyID
		if !missing {
			account.Proxy = &Proxy{ID: proxyID, Protocol: "file", Host: "localhost", Port: 1080, Username: "user", Password: "proxy-secret"}
		}
		svc := &AccountUsageService{codebuddyQuotaFetcher: NewCodeBuddyQuotaFetcher(), cache: NewUsageCache()}
		usage, err := svc.getCodeBuddyUsage(context.Background(), account, true)
		require.NoError(t, err)
		require.NotEmpty(t, usage.Error)
		require.NotContains(t, usage.Error, "proxy-secret")
		require.Zero(t, directCalls.Load(), "configured proxy failure must not send billing credentials directly")
	}
}

func TestCodeBuddyQuotaFetcherUsesAccountProxy(t *testing.T) {
	fetcher := NewCodeBuddyQuotaFetcher()
	account := &Account{Proxy: &Proxy{Protocol: "http", Host: "localhost", Port: 1080}}
	require.Equal(t, account.Proxy.URL(), fetcher.GetProxyURL(context.Background(), account))
	require.Empty(t, fetcher.GetProxyURL(context.Background(), &Account{}))
}
