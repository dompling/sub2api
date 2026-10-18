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
	leonardo := &LeonardoTokenProvider{}

	svc := ProvideAccountUsageService(
		nil, nil, nil, nil, nil, nil, nil, codeBuddy, nil,
		NewUsageCache(), nil, nil, gateway, kiro, leonardo,
	)

	require.Equal(t, kiro, svc.kiroTokenProvider)
	require.Same(t, codeBuddy, svc.codebuddyQuotaFetcher)
	require.Same(t, leonardo, svc.sharedLeonardoTokenProvider)
	require.Equal(t, gateway, svc.agentIdentityWS)
}

func TestProvideAccountTestServicePreservesKiroCodeBuddyAndAgentIdentityDependencies(t *testing.T) {
	kiro := &KiroTokenProvider{}
	codeBuddy := &CodeBuddyTokenProvider{}
	gateway := &OpenAIGatewayService{}
	leonardo := &LeonardoTokenProvider{}

	svc := ProvideAccountTestService(
		nil, nil, nil, kiro, nil, codeBuddy, leonardo, nil, nil, nil, nil, gateway, nil, nil,
	)

	require.Equal(t, kiro, svc.kiroTokenProvider)
	require.Same(t, codeBuddy, svc.codeBuddyTokenProvider)
	require.Same(t, leonardo, svc.leonardoTokenProvider)
	require.Equal(t, gateway, svc.agentIdentityWS)
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
