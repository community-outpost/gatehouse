package config

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const (
	ProviderProtocolOAuth2      = "oauth2"
	ProviderProtocolSteamOpenID = "steam_openid"
	ProviderProtocolGORedirect  = "go_redirect"
)

type AuthenticationConfig struct {
	PublicOrigin string                    `yaml:"public_origin"`
	UI           LoginUIConfig             `yaml:"ui"`
	Providers    map[string]ProviderConfig `yaml:"providers"`
}

// LoginUIConfig contains operator-owned presentation settings. Asset files are
// loaded once at startup and may be mounted into the container read-only.
type LoginUIConfig struct {
	ServiceName     string `yaml:"service_name"`
	OperatorName    string `yaml:"operator_name"`
	ApplicationName string `yaml:"application_name"`
	LogoFile        string `yaml:"logo_file"`
	FaviconFile     string `yaml:"favicon_file"`
	AccentColor     string `yaml:"accent_color"`
	BackgroundColor string `yaml:"background_color"`
}

// ProviderConfig contains protocol-neutral UI settings and the settings used by
// the selected protocol adapter. Unused protocol fields are ignored.
type ProviderConfig struct {
	Enabled          bool     `yaml:"enabled"`
	Label            string   `yaml:"label"`
	Description      string   `yaml:"description"`
	Icon             string   `yaml:"icon"`
	IconFile         string   `yaml:"icon_file"`
	Protocol         string   `yaml:"protocol"`
	LoginURL         string   `yaml:"login_url"`
	AuthorizationURL string   `yaml:"authorization_url"`
	TokenURL         string   `yaml:"token_url"`
	UserInfoURL      string   `yaml:"user_info_url"`
	ClientID         string   `yaml:"client_id"`
	ClientSecret     string   `yaml:"client_secret"`
	ClientSecretFile string   `yaml:"client_secret_file"`
	Scopes           []string `yaml:"scopes"`
	SubjectField     string   `yaml:"subject_field"`
	UserObjectField  string   `yaml:"user_object_field"`
	TokenAuthMethod  string   `yaml:"token_auth_method"`
	OpenIDEndpoint   string   `yaml:"openid_endpoint"`
}

func defaultAuthenticationConfig() AuthenticationConfig {
	return AuthenticationConfig{
		UI: LoginUIConfig{
			ServiceName:     "GateHouse",
			OperatorName:    "",
			ApplicationName: "Your Game",
			AccentColor:     "#00e7f0",
			BackgroundColor: "#060a0b",
		},
		Providers: make(map[string]ProviderConfig),
	}
}

func (c *AuthenticationConfig) resolveSecretFiles() error {
	for name, provider := range c.Providers {
		if provider.ClientSecret != "" && provider.ClientSecretFile != "" {
			return fmt.Errorf("authentication.providers.%s must set only one of client_secret and client_secret_file", name)
		}
		if provider.ClientSecretFile != "" {
			value, err := readSecretFile(provider.ClientSecretFile)
			if err != nil {
				return fmt.Errorf("read authentication.providers.%s.client_secret_file: %w", name, err)
			}
			provider.ClientSecret = value
			provider.ClientSecretFile = ""
			c.Providers[name] = provider
		}
	}
	return nil
}

func (c AuthenticationConfig) validate() error {
	if err := c.UI.validate(); err != nil {
		return err
	}
	needsPublicOrigin := false
	goRedirectProviders := 0
	for name, provider := range c.Providers {
		if !ValidEnvironment(name) {
			return fmt.Errorf("authentication provider name %q must contain lowercase letters, digits, underscores, or hyphens", name)
		}
		if !provider.Enabled {
			continue
		}
		if provider.Label == "" {
			return fmt.Errorf("authentication.providers.%s.label is required", name)
		}
		if provider.Icon != "" && provider.IconFile != "" {
			return fmt.Errorf("authentication.providers.%s must set only one of icon and icon_file", name)
		}
		switch provider.Protocol {
		case ProviderProtocolOAuth2:
			needsPublicOrigin = true
			if err := provider.validateOAuth2(name); err != nil {
				return err
			}
		case ProviderProtocolSteamOpenID:
			needsPublicOrigin = true
			if err := validateHTTPSURL(provider.OpenIDEndpoint); err != nil {
				return fmt.Errorf("authentication.providers.%s.openid_endpoint: %w", name, err)
			}
		case ProviderProtocolGORedirect:
			goRedirectProviders++
			if err := validateHTTPSURL(provider.LoginURL); err != nil {
				return fmt.Errorf("authentication.providers.%s.login_url: %w", name, err)
			}
		default:
			return fmt.Errorf("authentication.providers.%s.protocol must be oauth2, steam_openid, or go_redirect", name)
		}
	}
	if goRedirectProviders > 1 {
		return errors.New("only one enabled go_redirect provider is supported")
	}

	if needsPublicOrigin {
		origin, err := url.Parse(c.PublicOrigin)
		if err != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil ||
			(origin.Path != "" && origin.Path != "/") || origin.RawQuery != "" || origin.Fragment != "" {
			return errors.New("authentication.public_origin must be an HTTPS origin without a path, query, or fragment")
		}
	}
	return nil
}

// GORedirectIssuer returns the provider name used to namespace identities
// received through the GeneralsOnline LoginCode completion callback.
func (c AuthenticationConfig) GORedirectIssuer() string {
	for name, provider := range c.Providers {
		if provider.Enabled && provider.Protocol == ProviderProtocolGORedirect {
			return name
		}
	}
	return ""
}

var loginColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (c LoginUIConfig) validate() error {
	for field, value := range map[string]string{
		"service_name":     c.ServiceName,
		"application_name": c.ApplicationName,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("authentication.ui.%s is required", field)
		}
		if len(value) > 80 {
			return fmt.Errorf("authentication.ui.%s must be at most 80 characters", field)
		}
	}
	if len(c.OperatorName) > 80 {
		return errors.New("authentication.ui.operator_name must be at most 80 characters")
	}
	for field, value := range map[string]string{
		"accent_color":     c.AccentColor,
		"background_color": c.BackgroundColor,
	} {
		if !loginColorPattern.MatchString(value) {
			return fmt.Errorf("authentication.ui.%s must be a six-digit hex color", field)
		}
	}
	return nil
}

func (c ProviderConfig) validateOAuth2(name string) error {
	for field, value := range map[string]string{
		"authorization_url": c.AuthorizationURL,
		"token_url":         c.TokenURL,
		"user_info_url":     c.UserInfoURL,
	} {
		if err := validateHTTPSURL(value); err != nil {
			return fmt.Errorf("authentication.providers.%s.%s: %w", name, field, err)
		}
	}
	if strings.TrimSpace(c.ClientID) == "" {
		return fmt.Errorf("authentication.providers.%s.client_id is required", name)
	}
	if c.ClientSecret == "" {
		return fmt.Errorf("authentication.providers.%s.client_secret is required", name)
	}
	if strings.TrimSpace(c.SubjectField) == "" {
		return fmt.Errorf("authentication.providers.%s.subject_field is required", name)
	}
	if c.TokenAuthMethod != "client_secret_post" && c.TokenAuthMethod != "client_secret_basic" {
		return fmt.Errorf("authentication.providers.%s.token_auth_method must be client_secret_post or client_secret_basic", name)
	}
	return nil
}

func validateHTTPSURL(value string) error {
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return errors.New("must be an absolute HTTPS URL")
	}
	return nil
}
