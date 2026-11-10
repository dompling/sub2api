//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func codeBuddy429Settings(t *testing.T, svc *RateLimitService, enabled bool) {
	t.Helper()
	repo := newMockSettingRepo()
	data, err := json.Marshal(RateLimit429CooldownSettings{Enabled: enabled, CooldownSeconds: 12})
	require.NoError(t, err)
	repo.data[SettingKeyRateLimit429CooldownSettings] = string(data)
	svc.SetSettingService(NewSettingService(repo, &config.Config{}))
}

func TestHandle429_CodeBuddyRetryAfter(t *testing.T) {
	date := time.Now().Add(3 * time.Minute).UTC().Truncate(time.Second)
	for _, tc := range []struct {
		name    string
		header  string
		seconds time.Duration
		exact   *time.Time
	}{
		{name: "seconds", header: "120", seconds: 120 * time.Second},
		{name: "fractional_seconds", header: "1.5", seconds: 1500 * time.Millisecond},
		{name: "http_date", header: date.Format(http.TimeFormat), exact: &date},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{ID: 801, Platform: PlatformCodeBuddy, Type: AccountTypeOAuth}
			repo := newOllama429Repo(account)
			svc := NewRateLimitService(repo, nil, nil, nil, nil)
			blocker := &runtimeBlockRecorder{}
			svc.SetAccountRuntimeBlocker(blocker)
			// Disabling the local fallback must not discard an explicit upstream wait.
			codeBuddy429Settings(t, svc, false)
			before := time.Now()
			require.False(t, svc.HandleUpstreamError(context.Background(), account, http.StatusTooManyRequests,
				http.Header{"Retry-After": {tc.header}}, []byte(`{"error":{"message":"slow down"}}`)))
			after := time.Now()
			reset := repo.currentReset(account.ID)
			require.NotNil(t, reset)
			if tc.exact != nil {
				require.True(t, reset.Equal(*tc.exact))
			} else {
				require.False(t, reset.Before(before.Add(tc.seconds)))
				require.False(t, reset.After(after.Add(tc.seconds)))
			}
			require.Empty(t, repo.uncondWrites, "production repository must use atomic extension")
			require.Len(t, blocker.until, 1)
			require.True(t, blocker.until[0].Equal(*reset))
		})
	}
}

func TestHandle429_CodeBuddyInvalidRetryAfterUsesConfiguredFallback(t *testing.T) {
	for _, header := range []string{"", "invalid", "0", "-1", "NaN", "+Inf", "1e100", "Wed, 21 Oct 2015 07:28:00 GMT"} {
		t.Run(header, func(t *testing.T) {
			repo := &rateLimit429AccountRepoStub{}
			svc := NewRateLimitService(repo, nil, nil, nil, nil)
			codeBuddy429Settings(t, svc, true)
			account := &Account{ID: 802, Platform: PlatformCodeBuddy, Type: AccountTypeOAuth}
			before := time.Now()
			svc.handle429(context.Background(), account, http.Header{"Retry-After": {header}}, nil)
			after := time.Now()
			require.Equal(t, 1, repo.rateLimitCalls)
			require.False(t, repo.lastRateLimitReset.Before(before.Add(12*time.Second)))
			require.False(t, repo.lastRateLimitReset.After(after.Add(12*time.Second)))
		})
	}
}

func TestHandle429_CodeBuddyDisabledFallback(t *testing.T) {
	repo := &rateLimit429AccountRepoStub{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	codeBuddy429Settings(t, svc, false)
	account := &Account{ID: 803, Platform: PlatformCodeBuddy, Type: AccountTypeOAuth}
	svc.handle429(context.Background(), account, http.Header{"Retry-After": {"invalid"}}, nil)
	require.Zero(t, repo.rateLimitCalls)
}

func TestHandle429_CodeBuddyDoesNotShortenCooldown(t *testing.T) {
	for _, header := range []string{"30", "invalid"} {
		t.Run(header, func(t *testing.T) {
			later := time.Now().Add(10 * time.Minute)
			stored := &Account{ID: 804, Platform: PlatformCodeBuddy, Type: AccountTypeOAuth, RateLimitResetAt: &later}
			repo := newOllama429Repo(stored)
			svc := NewRateLimitService(repo, nil, nil, nil, nil)
			blocker := &runtimeBlockRecorder{}
			svc.SetAccountRuntimeBlocker(blocker)
			// Simulate another request extending the database deadline after selection.
			stale := *stored
			stale.RateLimitResetAt = nil
			svc.handle429(context.Background(), &stale, http.Header{"Retry-After": {header}}, nil)
			require.True(t, repo.currentReset(stored.ID).Equal(later))
			require.Empty(t, repo.uncondWrites)
			require.Len(t, blocker.until, 1)
			require.True(t, blocker.until[0].Equal(later), "runtime scheduling must use the authoritative longer deadline")
		})
	}
}

func TestHandle429_CodeBuddyLegacyRepositoryPreservesKnownCooldown(t *testing.T) {
	repo := &rateLimit429AccountRepoStub{}
	svc := NewRateLimitService(repo, nil, nil, nil, nil)
	later := time.Now().Add(10 * time.Minute)
	account := &Account{ID: 805, Platform: PlatformCodeBuddy, Type: AccountTypeOAuth, RateLimitResetAt: &later}
	svc.handle429(context.Background(), account, http.Header{"Retry-After": {"20"}}, nil)
	require.Equal(t, 1, repo.rateLimitCalls)
	require.True(t, repo.lastRateLimitReset.Equal(later))
}

func TestHandleUpstreamError_CodeBuddyEdge403DoesNotDisableAccount(t *testing.T) {
	for _, body := range []string{openAI403HTMLBody, "\n\t <!DOCTYPE HTML><html>Forbidden</html>", "<html>Forbidden</html>", openAI403Cloudflare1010Body} {
		t.Run(body, func(t *testing.T) {
			h := newOpenAI403TestHarness(t, 806, 1, 2, 3, 4, 5)
			h.account.Platform = PlatformCodeBuddy
			for i := 0; i < openAI403DisableThreshold+2; i++ {
				require.False(t, h.handle(body))
			}
			h.requireNoAccountPenalty(t)
		})
	}
}

func TestHandleUpstreamError_CodeBuddyCredential403StillDisablesAccount(t *testing.T) {
	h := newOpenAI403TestHarness(t, 807, 1)
	h.account.Platform = PlatformCodeBuddy
	require.True(t, h.handle(`{"error":{"code":"permission_denied","message":"account suspended"}}`))
	require.Equal(t, 1, h.repo.setErrorCalls)
	require.Contains(t, h.repo.lastErrorMsg, "account suspended")
	require.Zero(t, h.counter.increments)
}

func TestCodeBuddyEdge403DoesNotRetrySameOAuthAccount(t *testing.T) {
	h := newOpenAI403TestHarness(t, 808)
	h.account.Platform = PlatformCodeBuddy
	gateway := &OpenAIGatewayService{rateLimitService: h.svc}
	failover := gateway.failoverOpenAIUpstreamHTTPError(context.Background(), nil, h.account,
		&http.Response{StatusCode: http.StatusForbidden, Header: http.Header{}},
		[]byte(openAI403HTMLBody), "", "test-model")
	require.NotNil(t, failover)
	require.False(t, failover.RetryableOnSameAccount)
	h.requireNoAccountPenalty(t)
}
