package main

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/community-outpost/gatehouse/internal/config"
	"github.com/community-outpost/gatehouse/internal/externallogin"
	"github.com/community-outpost/gatehouse/internal/httpapi"
	"github.com/community-outpost/gatehouse/internal/mysqlstore"
)

func configureLogin(api *httpapi.Server, cfg config.Config, resolver *mysqlstore.PrincipalResolver) error {
	providers := make(map[string]httpapi.LoginProvider)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxConnsPerHost = 32
	transport.MaxIdleConnsPerHost = 8
	transport.IdleConnTimeout = 90 * time.Second
	httpClient := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	origin := strings.TrimSuffix(cfg.Authentication.PublicOrigin, "/")
	branding := httpapi.LoginBranding{
		ServiceName:     cfg.Authentication.UI.ServiceName,
		OperatorName:    cfg.Authentication.UI.OperatorName,
		ApplicationName: cfg.Authentication.UI.ApplicationName,
		AccentColor:     cfg.Authentication.UI.AccentColor,
		BackgroundColor: cfg.Authentication.UI.BackgroundColor,
	}
	var err error
	if cfg.Authentication.UI.LogoFile != "" {
		branding.Logo, err = httpapi.LoadLoginAsset(cfg.Authentication.UI.LogoFile)
		if err != nil {
			return fmt.Errorf("load authentication.ui.logo_file: %w", err)
		}
	}
	if cfg.Authentication.UI.FaviconFile != "" {
		branding.Favicon, err = httpapi.LoadLoginAsset(cfg.Authentication.UI.FaviconFile)
		if err != nil {
			return fmt.Errorf("load authentication.ui.favicon_file: %w", err)
		}
	}

	for name, providerConfig := range cfg.Authentication.Providers {
		if !providerConfig.Enabled {
			continue
		}
		loginProvider := httpapi.LoginProvider{
			Label:       providerConfig.Label,
			Description: providerConfig.Description,
			Icon:        providerConfig.Icon,
		}
		if providerConfig.IconFile != "" {
			loginProvider.IconAsset, err = httpapi.LoadLoginAsset(providerConfig.IconFile)
			if err != nil {
				return fmt.Errorf("load authentication.providers.%s.icon_file: %w", name, err)
			}
		}
		switch providerConfig.Protocol {
		case config.ProviderProtocolOAuth2:
			provider, err := externallogin.NewOAuth(externallogin.OAuthConfig{
				AuthorizationURL: providerConfig.AuthorizationURL,
				TokenURL:         providerConfig.TokenURL,
				UserInfoURL:      providerConfig.UserInfoURL,
				ClientID:         providerConfig.ClientID,
				ClientSecret:     providerConfig.ClientSecret,
				RedirectURL:      origin + "/login/" + name + "/return",
				Scopes:           providerConfig.Scopes,
				SubjectField:     providerConfig.SubjectField,
				UserObjectField:  providerConfig.UserObjectField,
				TokenAuthMethod:  providerConfig.TokenAuthMethod,
			}, httpClient)
			if err != nil {
				return fmt.Errorf("configure provider %s OAuth: %w", name, err)
			}
			loginProvider.Authenticator = provider
		case config.ProviderProtocolSteamOpenID:
			provider, err := externallogin.NewSteam(
				providerConfig.OpenIDEndpoint,
				origin+"/login/"+name+"/return",
				origin+"/",
				httpClient,
			)
			if err != nil {
				return fmt.Errorf("configure provider %s Steam OpenID: %w", name, err)
			}
			loginProvider.Authenticator = provider
		case config.ProviderProtocolGORedirect:
			loginProvider.RedirectURL = providerConfig.LoginURL
		default:
			return fmt.Errorf("configure provider %s: unsupported protocol %q", name, providerConfig.Protocol)
		}
		providers[name] = loginProvider
	}

	return api.ConfigureLogin(resolver, httpapi.LoginOptions{
		GORedirectIssuer: cfg.Authentication.GORedirectIssuer(),
		Branding:         branding,
		Providers:        providers,
		StateSecret:      cfg.InboundAPIKey,
	})
}
