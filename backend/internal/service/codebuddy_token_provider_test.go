//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func codeBuddyTokenAccountForTest() *Account {
	account := codeBuddyRawChatCompletionsTestAccount()
	account.Status = StatusActive
	account.Schedulable = true
	account.Credentials["refresh_token"] = "refresh-token-test"
	account.Credentials["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	account.Credentials["codebuddy_models_meta"] = `[{"id":"hy3","max_output_tokens":64000}]`
	return account
}

func TestCodeBuddyTokenProviderLockHeldRequiresUnexpiredToken(t *testing.T) {
	for _, state := range []string{"expired", "expired_cached", "unknown_expiry", "unknown_expiry_cached", "still_valid"} {
		t.Run(state, func(t *testing.T) {
			account := codeBuddyTokenAccountForTest()
			if state == "unknown_expiry" || state == "unknown_expiry_cached" {
				delete(account.Credentials, "expires_at")
			} else if state == "still_valid" {
				account.Credentials["expires_at"] = time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339)
			}
			repo := &tokenRefreshAccountRepo{}
			repo.accountsByID = map[int64]*Account{account.ID: account}
			cache := &grokTokenCacheForProviderTest{lockResult: false}
			if state == "expired_cached" || state == "unknown_expiry_cached" {
				cache.token = account.GetCodeBuddyAccessToken()
			}
			executor := &refreshAPIExecutorStub{needsRefresh: true}
			provider := NewCodeBuddyTokenProvider(repo, cache)
			provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), executor)

			token, err := provider.GetAccessToken(context.Background(), account)
			if state == "still_valid" {
				require.NoError(t, err)
				require.Equal(t, account.GetCodeBuddyAccessToken(), token)
				require.Equal(t, token, cache.setToken)
				require.Greater(t, cache.setTTL, time.Duration(0))
				require.LessOrEqual(t, cache.setTTL, 2*time.Minute)
			} else {
				require.Error(t, err)
				require.Empty(t, token)
				require.Empty(t, cache.setToken)
			}
			require.Zero(t, executor.refreshCalls)
			require.Zero(t, repo.setTempUnschedCalls)
			require.Zero(t, repo.setErrorCalls)
		})
	}
}

func TestCodeBuddyTokenProviderCallerCancellationDoesNotUnschedule(t *testing.T) {
	for _, deadline := range []bool{false, true} {
		name := "canceled"
		if deadline {
			name = "deadline"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if deadline {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 5*time.Millisecond)
			}
			defer cancel()
			account := codeBuddyTokenAccountForTest()
			repo := &tokenRefreshAccountRepo{}
			repo.accountsByID = map[int64]*Account{account.ID: account}
			cache := &grokTokenCacheForProviderTest{lockResult: true}
			executor := &refreshAPIExecutorStub{needsRefresh: true, onRefresh: func() {
				if !deadline {
					cancel()
				}
				<-ctx.Done()
			}}
			provider := NewCodeBuddyTokenProvider(repo, cache)
			provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), executor)
			provider.SetRefreshPolicy(ProviderRefreshPolicy{OnRefreshError: ProviderRefreshErrorUseExistingToken})

			token, err := provider.GetAccessToken(ctx, account)
			require.ErrorIs(t, err, ctx.Err())
			require.Empty(t, token)
			require.Zero(t, repo.setTempUnschedCalls)
			require.Zero(t, repo.setErrorCalls)
			require.Zero(t, repo.updateCredentialsCalls)
			require.Empty(t, cache.setToken)
		})
	}
}

func TestCodeBuddyTokenProviderRefreshFailureStillUnschedules(t *testing.T) {
	for _, refreshErr := range []error{errors.New("provider unavailable"), context.DeadlineExceeded} {
		t.Run(refreshErr.Error(), func(t *testing.T) {
			account := codeBuddyTokenAccountForTest()
			repo := &tokenRefreshAccountRepo{}
			repo.accountsByID = map[int64]*Account{account.ID: account}
			cache := &grokTokenCacheForProviderTest{lockResult: true}
			provider := NewCodeBuddyTokenProvider(repo, cache)
			provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), &refreshAPIExecutorStub{needsRefresh: true, err: refreshErr})

			token, err := provider.GetAccessToken(context.Background(), account)
			require.ErrorIs(t, err, refreshErr)
			require.Empty(t, token)
			require.Equal(t, 1, repo.setTempUnschedCalls)
			require.Zero(t, repo.setErrorCalls)
			require.Empty(t, cache.setToken)
		})
	}
}

func TestCodeBuddyTokenProviderRejectsInactiveAccountReread(t *testing.T) {
	account := codeBuddyTokenAccountForTest()
	inactive := snapshotOAuthRefreshAccount(account)
	inactive.Status = StatusError
	inactive.Credentials["expires_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	repo := &tokenRefreshAccountRepo{}
	repo.accountsByID = map[int64]*Account{account.ID: inactive}
	cache := &grokTokenCacheForProviderTest{lockResult: true}
	executor := &refreshAPIExecutorStub{needsRefresh: true}
	provider := NewCodeBuddyTokenProvider(repo, cache)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), executor)

	token, err := provider.GetAccessToken(context.Background(), account)
	require.ErrorIs(t, err, errOAuthRefreshAccountStateChanged)
	require.Empty(t, token)
	require.Zero(t, executor.refreshCalls)
	require.Zero(t, repo.setTempUnschedCalls)
	require.Zero(t, repo.setErrorCalls)
	require.Empty(t, cache.setToken)
}

func TestCodeBuddyTokenProviderRefreshesAndCachesRotatedToken(t *testing.T) {
	account := codeBuddyTokenAccountForTest()
	repo := &tokenRefreshAccountRepo{}
	repo.accountsByID = map[int64]*Account{account.ID: account}
	cache := &grokTokenCacheForProviderTest{lockResult: true}
	credentials := shallowCopyMap(account.Credentials)
	credentials["access_token"] = "rotated-token-test"
	credentials["expires_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	executor := &refreshAPIExecutorStub{needsRefresh: true, credentials: credentials}
	provider := NewCodeBuddyTokenProvider(repo, cache)
	provider.SetRefreshAPI(NewOAuthRefreshAPI(repo, cache), executor)

	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "rotated-token-test", token)
	require.Equal(t, 1, executor.refreshCalls)
	require.Equal(t, 1, repo.updateCredentialsCalls)
	require.Equal(t, token, cache.setToken)
	require.Equal(t, CodeBuddyTokenCacheKey(account), cache.setKey)
	require.Greater(t, cache.setTTL, 50*time.Minute)
	require.LessOrEqual(t, cache.setTTL, 55*time.Minute)
}

func TestCodeBuddyTokenProviderRejectsExpiredTokenWithoutRefreshAPI(t *testing.T) {
	account := codeBuddyTokenAccountForTest()
	cache := &grokTokenCacheForProviderTest{}
	provider := NewCodeBuddyTokenProvider(nil, cache)
	token, err := provider.GetAccessToken(context.Background(), account)
	require.Error(t, err)
	require.Empty(t, token)
	require.Empty(t, cache.setToken)
}

func TestCodeBuddyTokenProviderRejectsExpiredVersionedCredential(t *testing.T) {
	account := codeBuddyTokenAccountForTest()
	account.Credentials["expires_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	latest := snapshotOAuthRefreshAccount(account)
	latest.Credentials["_token_version"] = time.Now().UnixMilli()
	latest.Credentials["access_token"] = "expired-latest-token"
	latest.Credentials["expires_at"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	repo := &tokenRefreshAccountRepo{}
	repo.accountsByID = map[int64]*Account{account.ID: latest}
	cache := &grokTokenCacheForProviderTest{}
	provider := NewCodeBuddyTokenProvider(repo, cache)
	token, err := provider.GetAccessToken(context.Background(), account)
	require.Error(t, err)
	require.Empty(t, token)
	require.Empty(t, cache.setToken)
}
