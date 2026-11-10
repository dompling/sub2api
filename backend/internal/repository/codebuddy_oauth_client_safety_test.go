package repository

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodeBuddyOAuthClientDoesNotRedirectCredentials(t *testing.T) {
	var calls atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()

	client, err := createCodeBuddyReqClient("")
	require.NoError(t, err)
	resp, err := client.R().SetHeader("Authorization", "Bearer synthetic-refresh-token").
		SetHeader("X-Refresh-Token", "synthetic-refresh-token").Get(source.URL)
	require.NoError(t, err)
	require.Equal(t, http.StatusTemporaryRedirect, resp.StatusCode)
	require.Zero(t, calls.Load())

	// CodeBuddy's policy must not change other platforms' pooled clients.
	shared, err := getSharedReqClient(reqClientOptions{Timeout: 60 * time.Second})
	require.NoError(t, err)
	resp, err = shared.R().Get(source.URL)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.EqualValues(t, 1, calls.Load())
}

func TestCodeBuddyOAuthClientRedactsInvalidProxy(t *testing.T) {
	_, err := createCodeBuddyReqClient("http://proxy-user:proxy-secret@bad%host")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "proxy-secret")
}
