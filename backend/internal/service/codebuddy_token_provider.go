package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
)

const (
	codeBuddyTokenCacheSkew             = 5 * time.Minute
	codeBuddyRequestRefreshTimeout      = 8 * time.Second
	codeBuddyTokenProviderLogComponent  = "codebuddy_token_provider"
	codeBuddyTempUnschedulableErrorCode = "token_refresh_failed"
)

// CodeBuddyTokenCache 复用 Gemini 的 token 缓存实现。
type CodeBuddyTokenCache = GeminiTokenCache

// CodeBuddyTokenProvider 为 CodeBuddy OAuth 账号提供 access_token 获取与按需刷新。
type CodeBuddyTokenProvider struct {
	accountRepo      AccountRepository
	tokenCache       CodeBuddyTokenCache
	refreshAPI       *OAuthRefreshAPI
	executor         OAuthRefreshExecutor
	refreshPolicy    ProviderRefreshPolicy
	tempUnschedCache TempUnschedCache
}

func NewCodeBuddyTokenProvider(
	accountRepo AccountRepository,
	tokenCache CodeBuddyTokenCache,
) *CodeBuddyTokenProvider {
	return &CodeBuddyTokenProvider{
		accountRepo:   accountRepo,
		tokenCache:    tokenCache,
		refreshPolicy: AntigravityProviderRefreshPolicy(),
	}
}

func (p *CodeBuddyTokenProvider) SetRefreshAPI(api *OAuthRefreshAPI, executor OAuthRefreshExecutor) {
	p.refreshAPI = api
	p.executor = executor
}

func (p *CodeBuddyTokenProvider) SetRefreshPolicy(policy ProviderRefreshPolicy) {
	p.refreshPolicy = policy
}

func (p *CodeBuddyTokenProvider) SetTempUnschedCache(cache TempUnschedCache) {
	p.tempUnschedCache = cache
}

func (p *CodeBuddyTokenProvider) GetAccessToken(ctx context.Context, account *Account) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if account == nil {
		return "", errors.New("account is nil")
	}
	if account.Platform != PlatformCodeBuddy || account.Type != AccountTypeOAuth {
		return "", errors.New("not a codebuddy oauth account")
	}

	expiresAt := account.GetCredentialAsTime("expires_at")
	cacheKey := CodeBuddyTokenCacheKey(account)
	if p.tokenCache != nil {
		if token, err := p.tokenCache.GetAccessToken(ctx, cacheKey); err == nil && strings.TrimSpace(token) != "" {
			// A different token can be a newer rotation than this account snapshot.
			// The same expired credential must not become valid just by caching it.
			if token != account.GetCodeBuddyAccessToken() || (expiresAt != nil && time.Now().Before(*expiresAt)) {
				return token, nil
			}
		}
	}

	needsRefresh := expiresAt == nil || time.Until(*expiresAt) <= codeBuddyTokenCacheSkew
	if needsRefresh && strings.TrimSpace(account.GetCodeBuddyRefreshToken()) == "" {
		if expiresAt == nil || !time.Now().Before(*expiresAt) {
			return "", errors.New("codebuddy access_token expired and refresh_token is missing")
		}
		needsRefresh = false
	}
	if needsRefresh && p.refreshAPI != nil && p.executor != nil {
		refreshCtx, cancel := context.WithTimeout(ctx, codeBuddyRequestRefreshTimeout)
		defer cancel()
		result, err := p.refreshAPI.RefreshIfNeeded(withOAuthRefreshRequestPath(refreshCtx), account, p.executor, codeBuddyTokenCacheSkew)
		if err != nil {
			// A canceled request or an account changed by another actor is not
			// evidence that this credential is unhealthy. An independent refresh
			// timeout still follows the normal cooldown policy.
			if ctxErr := ctx.Err(); ctxErr != nil {
				return "", ctxErr
			}
			if errors.Is(err, errOAuthRefreshAccountStateChanged) {
				return "", err
			}
			p.markTempUnschedulable(account, err)
			if p.refreshPolicy.OnRefreshError == ProviderRefreshErrorReturn {
				return "", err
			}
		} else if result != nil && result.LockHeld {
			if expiresAt == nil || !time.Now().Before(*expiresAt) {
				return "", errors.New("codebuddy token refresh is in progress and existing access_token is not valid")
			}
		} else if result != nil && result.Account != nil {
			account = result.Account
			expiresAt = account.GetCredentialAsTime("expires_at")
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	accessToken := account.GetCodeBuddyAccessToken()
	if strings.TrimSpace(accessToken) == "" {
		return "", errors.New("access_token not found in credentials")
	}
	if expiresAt == nil || !time.Now().Before(*expiresAt) {
		return "", errors.New("codebuddy access_token is expired or expiry is unknown")
	}

	if p.tokenCache != nil {
		latestAccount, isStale := CheckTokenVersion(ctx, account, p.accountRepo)
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if isStale && latestAccount != nil {
			if !latestAccount.IsCodeBuddyOAuth() || !latestAccount.IsActive() {
				return "", errOAuthRefreshAccountStateChanged
			}
			accessToken = latestAccount.GetCodeBuddyAccessToken()
			if strings.TrimSpace(accessToken) == "" {
				return "", errors.New("access_token not found after version check")
			}
			latestExpiry := latestAccount.GetCredentialAsTime("expires_at")
			if latestExpiry == nil || !time.Now().Before(*latestExpiry) {
				return "", errors.New("codebuddy access_token is expired or expiry is unknown after version check")
			}
		} else {
			ttl := time.Until(*expiresAt)
			if ttl <= 0 {
				return "", errors.New("codebuddy access_token is expired")
			}
			if ttl > codeBuddyTokenCacheSkew {
				ttl -= codeBuddyTokenCacheSkew
			}
			_ = p.tokenCache.SetAccessToken(ctx, cacheKey, accessToken, ttl)
		}
	}

	return accessToken, nil
}

func (p *CodeBuddyTokenProvider) markTempUnschedulable(account *Account, refreshErr error) {
	if p == nil || p.accountRepo == nil || account == nil {
		return
	}
	now := time.Now()
	until := now.Add(tokenRefreshTempUnschedDuration)
	redactedErr := "unknown error"
	if refreshErr != nil {
		redactedErr = logredact.RedactText(refreshErr.Error())
	}
	if isNonRetryableRefreshError(refreshErr) {
		if err := p.accountRepo.SetError(context.Background(), account.ID, "codebuddy token refresh failed (non-retryable): "+redactedErr); err != nil {
			slog.Warn(codeBuddyTokenProviderLogComponent+".set_error_status_failed", "account_id", account.ID, "error", err)
		}
		return
	}
	reason := "codebuddy token refresh failed on request path: " + redactedErr
	bgCtx := context.Background()
	if err := p.accountRepo.SetTempUnschedulable(bgCtx, account.ID, until, reason); err != nil {
		slog.Warn(codeBuddyTokenProviderLogComponent+".set_temp_unschedulable_failed", "account_id", account.ID, "error", err)
	}
	if p.tempUnschedCache != nil {
		state := &TempUnschedState{
			UntilUnix:       until.Unix(),
			TriggeredAtUnix: now.Unix(),
			ErrorMessage:    codeBuddyTempUnschedulableErrorCode + ": " + reason,
		}
		if err := p.tempUnschedCache.SetTempUnsched(bgCtx, account.ID, state); err != nil {
			slog.Warn(codeBuddyTokenProviderLogComponent+".temp_unsched_cache_set_failed", "account_id", account.ID, "error", err)
		}
	}
}
