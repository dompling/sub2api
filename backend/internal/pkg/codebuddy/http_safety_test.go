package codebuddy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodeBuddyConfigInvalidProxyDoesNotConnectDirectly(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"code":0,"data":{"models":[]}}`))
	}))
	defer upstream.Close()

	for _, proxy := range []string{"http://proxy-user:proxy-secret@bad%host", "file://proxy-user:proxy-secret@localhost"} {
		_, err := FetchEnabledModelsFromBaseURL(context.Background(), upstream.URL, "synthetic-token", "synthetic-user", proxy)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "proxy-secret")
		require.Zero(t, calls.Load(), "invalid proxy must fail before an authenticated direct request")
	}
}

func TestCodeBuddyConfigDoesNotFollowAuthenticatedRedirect(t *testing.T) {
	var calls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"code":0,"data":{"models":[]}}`))
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	_, err := FetchEnabledModelsFromBaseURL(context.Background(), source.URL, "synthetic-token", "synthetic-user", "")
	require.Error(t, err)
	require.Zero(t, calls.Load())
}

func TestCodeBuddyConfigUsesConfiguredProxy(t *testing.T) {
	var calls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, "workbuddy.invalid", r.URL.Host)
		require.Equal(t, ConfigPath, r.URL.Path)
		require.Equal(t, "Bearer synthetic-token", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"code":0,"data":{"models":[]}}`))
	}))
	defer proxy.Close()

	_, err := FetchEnabledModelsFromBaseURL(context.Background(), "http://workbuddy.invalid", "synthetic-token", "synthetic-user", proxy.URL)
	require.NoError(t, err)
	require.EqualValues(t, 1, calls.Load())
}

func TestCodeBuddyProxyValidation(t *testing.T) {
	for _, proxy := range []string{"http://user:secret@bad%host", "missing-scheme", "file://localhost"} {
		_, err := newProxyTransport(proxy)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "secret")
	}
	transport, err := newProxyTransport("socks5://localhost:1080")
	require.NoError(t, err)
	defer transport.CloseIdleConnections()
	proxy, err := transport.Proxy(&http.Request{})
	require.NoError(t, err)
	require.Equal(t, "socks5h", proxy.Scheme)
}

func TestCodeBuddyConfigRedactsCredentialErrors(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"accessToken":"synthetic-secret","refresh_token":"synthetic-refresh"}`))
	}))
	defer upstream.Close()
	_, err := FetchEnabledModelsFromBaseURL(context.Background(), upstream.URL, "synthetic-token", "synthetic-user", "")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "synthetic-secret")
	require.NotContains(t, err.Error(), "synthetic-refresh")
}

type codeBuddySharedTransport struct {
	closes atomic.Int32
}

func (t *codeBuddySharedTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"models":[],"Response":{"Data":{"Accounts":[]}}}}`))}, nil
}

func (t *codeBuddySharedTransport) CloseIdleConnections() { t.closes.Add(1) }

func TestCodeBuddyDirectQueriesKeepSharedConnections(t *testing.T) {
	transport := &codeBuddySharedTransport{}
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	_, err := FetchEnabledModelsFromBaseURL(context.Background(), "https://workbuddy.invalid", "synthetic-token", "synthetic-user", "")
	require.NoError(t, err)
	_, _, err = GetUserResource(context.Background(), "synthetic-token", "synthetic-user", "")
	require.NoError(t, err)
	require.Zero(t, transport.closes.Load(), "queries must not close connections owned by other clients")
}
