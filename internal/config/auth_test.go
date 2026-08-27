package config

import (
	"strings"
	"testing"
)

func TestGORedirectIssuerUsesEnabledProviderName(t *testing.T) {
	t.Parallel()

	cfg := defaultAuthenticationConfig()
	cfg.Providers = map[string]ProviderConfig{
		"old_redirect": {Protocol: ProviderProtocolGORedirect},
		"generalsonline": {
			Enabled:  true,
			Protocol: ProviderProtocolGORedirect,
		},
	}

	if issuer := cfg.GORedirectIssuer(); issuer != "generalsonline" {
		t.Fatalf("GORedirectIssuer() = %q", issuer)
	}
}

func TestValidateRejectsMultipleEnabledGORedirectProviders(t *testing.T) {
	t.Parallel()

	cfg := validConfig()
	cfg.Authentication = defaultAuthenticationConfig()
	cfg.Authentication.Providers = map[string]ProviderConfig{
		"first": {
			Enabled:  true,
			Label:    "First",
			Protocol: ProviderProtocolGORedirect,
			LoginURL: "https://first.example/login",
		},
		"second": {
			Enabled:  true,
			Label:    "Second",
			Protocol: ProviderProtocolGORedirect,
			LoginURL: "https://second.example/login",
		},
	}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "only one enabled go_redirect provider") {
		t.Fatalf("Validate() error = %v", err)
	}
}
