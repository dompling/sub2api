//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProvideAccountUsageServicePreservesKiroCodeBuddyAndAgentIdentityDependencies(t *testing.T) {
	kiro := &KiroTokenProvider{}
	codeBuddy := &CodeBuddyQuotaFetcher{}
	gateway := &OpenAIGatewayService{}
	adobeProvider := NewAdobeTokenProvider(nil, NewOAuthRefreshAPI(nil, nil))

	svc := ProvideAccountUsageService(
		nil, nil, nil, nil, nil, nil, nil, codeBuddy, nil,
		NewUsageCache(), nil, nil, gateway, kiro, adobeProvider,
	)

	require.Equal(t, kiro, svc.kiroTokenProvider)
	require.Same(t, codeBuddy, svc.codebuddyQuotaFetcher)
	require.Equal(t, gateway, svc.agentIdentityWS)
	require.Same(t, adobeProvider, svc.adobeTokenProvider)
}

func TestProvideAccountTestServicePreservesKiroCodeBuddyAndAgentIdentityDependencies(t *testing.T) {
	kiro := &KiroTokenProvider{}
	codeBuddy := &CodeBuddyTokenProvider{}
	gateway := &OpenAIGatewayService{}

	adobeProvider := &AdobeTokenProvider{}

	svc := ProvideAccountTestService(
		nil, nil, nil, kiro, nil, codeBuddy, nil, nil, nil, nil, gateway, nil, nil, adobeProvider,
	)

	require.Equal(t, kiro, svc.kiroTokenProvider)
	require.Same(t, codeBuddy, svc.codeBuddyTokenProvider)
	require.Equal(t, gateway, svc.agentIdentityWS)
	require.Same(t, adobeProvider, svc.adobeTokenProvider)
}

func TestProvideOpenAIGatewayServicePreservesCodeBuddyTokenProvider(t *testing.T) {
	codeBuddy := &CodeBuddyTokenProvider{}

	svc := ProvideOpenAIGatewayService(
		nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, codeBuddy,
		nil, nil, nil, nil, nil,
	)
	t.Cleanup(svc.StopOpenAICodexTicketHarvester)

	require.Same(t, codeBuddy, svc.codeBuddyTokenProvider)
}

func TestProvideAdminServicePreservesCodeBuddyTokenProvider(t *testing.T) {
	codeBuddy := &CodeBuddyTokenProvider{}

	svc := ProvideAdminService(
		nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, codeBuddy,
	)
	impl, ok := svc.(*adminServiceImpl)
	require.True(t, ok)
	require.Same(t, codeBuddy, impl.codeBuddyTokenProvider)
}
