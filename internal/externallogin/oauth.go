package externallogin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const maxProviderResponseBytes = 1 << 20

type OAuthConfig struct {
	AuthorizationURL string
	TokenURL         string
	UserInfoURL      string
	ClientID         string
	ClientSecret     string
	RedirectURL      string
	Scopes           []string
	SubjectField     string
	UserObjectField  string
	TokenAuthMethod  string
}

type OAuth struct {
	config OAuthConfig
	client *http.Client
}

func NewOAuth(config OAuthConfig, client *http.Client) (*OAuth, error) {
	for name, value := range map[string]string{
		"authorization URL": config.AuthorizationURL,
		"token URL":         config.TokenURL,
		"user-info URL":     config.UserInfoURL,
		"redirect URL":      config.RedirectURL,
	} {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return nil, fmt.Errorf("%s must be an absolute HTTPS URL", name)
		}
	}
	if config.ClientID == "" || config.ClientSecret == "" || config.SubjectField == "" {
		return nil, errors.New("OAuth client ID, client secret, and subject field are required")
	}
	if config.TokenAuthMethod != "client_secret_post" && config.TokenAuthMethod != "client_secret_basic" {
		return nil, errors.New("unsupported OAuth token authentication method")
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &OAuth{config: config, client: client}, nil
}

func (o *OAuth) AuthorizationURL(state string) (string, error) {
	endpoint, err := url.Parse(o.config.AuthorizationURL)
	if err != nil {
		return "", err
	}
	query := endpoint.Query()
	query.Set("response_type", "code")
	query.Set("client_id", o.config.ClientID)
	query.Set("redirect_uri", o.config.RedirectURL)
	query.Set("state", state)
	if len(o.config.Scopes) > 0 {
		query.Set("scope", strings.Join(o.config.Scopes, " "))
	}
	endpoint.RawQuery = query.Encode()
	return endpoint.String(), nil
}

func (o *OAuth) Authenticate(ctx context.Context, values url.Values) (Identity, error) {
	if providerError := strings.TrimSpace(values.Get("error")); providerError != "" {
		return Identity{}, fmt.Errorf("authorization denied: %s", providerError)
	}
	code := strings.TrimSpace(values.Get("code"))
	if code == "" {
		return Identity{}, errors.New("authorization response is missing code")
	}

	form := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {o.config.RedirectURL},
	}
	if o.config.TokenAuthMethod == "client_secret_post" {
		form.Set("client_id", o.config.ClientID)
		form.Set("client_secret", o.config.ClientSecret)
	}
	encodedForm := form.Encode()
	tokenRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, o.config.TokenURL, strings.NewReader(encodedForm))
	if err != nil {
		return Identity{}, fmt.Errorf("create token request: %w", err)
	}
	tokenRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if o.config.TokenAuthMethod == "client_secret_basic" {
		tokenRequest.SetBasicAuth(o.config.ClientID, o.config.ClientSecret)
	}

	var tokenResponse struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if err := o.doJSON(tokenRequest, &tokenResponse); err != nil {
		return Identity{}, fmt.Errorf("exchange authorization code: %w", err)
	}
	if tokenResponse.AccessToken == "" {
		return Identity{}, errors.New("token response is missing access_token")
	}
	tokenType := strings.TrimSpace(tokenResponse.TokenType)
	if tokenType == "" {
		tokenType = "Bearer"
	}
	if !strings.EqualFold(tokenType, "Bearer") {
		return Identity{}, fmt.Errorf("unsupported OAuth token type %q", tokenType)
	}

	userRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, o.config.UserInfoURL, nil)
	if err != nil {
		return Identity{}, fmt.Errorf("create user-info request: %w", err)
	}
	userRequest.Header.Set("Authorization", "Bearer "+tokenResponse.AccessToken)
	var profile map[string]any
	if err := o.doJSON(userRequest, &profile); err != nil {
		return Identity{}, fmt.Errorf("fetch user info: %w", err)
	}
	if o.config.UserObjectField != "" {
		value, exists := profile[o.config.UserObjectField]
		if exists && value != nil {
			nested, ok := value.(map[string]any)
			if !ok {
				return Identity{}, fmt.Errorf("user-info field %q must be an object", o.config.UserObjectField)
			}
			profile = nested
		}
	}
	subject, err := oauthSubject(profile[o.config.SubjectField])
	if err != nil {
		return Identity{}, fmt.Errorf("read user-info field %q: %w", o.config.SubjectField, err)
	}
	return Identity{Subject: subject}, nil
}

func (o *OAuth) doJSON(request *http.Request, target any) error {
	response, err := o.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxProviderResponseBytes))
		return fmt.Errorf("provider returned HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxProviderResponseBytes))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode JSON response: %w", err)
	}
	return nil
}

func oauthSubject(value any) (string, error) {
	var subject string
	switch typed := value.(type) {
	case string:
		subject = strings.TrimSpace(typed)
	case json.Number:
		if _, err := strconv.ParseUint(string(typed), 10, 64); err != nil {
			return "", errors.New("numeric subject must be an unsigned integer")
		}
		subject = string(typed)
	default:
		return "", errors.New("subject must be a string or unsigned integer")
	}
	if subject == "" || len(subject) > 255 {
		return "", errors.New("subject must contain 1 to 255 characters")
	}
	return subject, nil
}
