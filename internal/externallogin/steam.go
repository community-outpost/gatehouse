package externallogin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const openIDNamespace = "http://specs.openid.net/auth/2.0"
const openIDIdentifierSelect = "http://specs.openid.net/auth/2.0/identifier_select"

var steamClaimedIDPattern = regexp.MustCompile(`^https://steamcommunity\.com/openid/id/([0-9]{17,20})$`)

const steamNonceLifetime = 10 * time.Minute

type Steam struct {
	endpoint   string
	returnURL  string
	realm      string
	httpClient *http.Client
	nonceMu    sync.Mutex
	usedNonces map[string]time.Time
}

func NewSteam(endpoint, returnURL, realm string, client *http.Client) (*Steam, error) {
	for name, value := range map[string]string{"endpoint": endpoint, "return URL": returnURL, "realm": realm} {
		parsed, err := url.Parse(value)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return nil, fmt.Errorf("Steam OpenID %s must be an absolute HTTPS URL", name)
		}
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &Steam{
		endpoint: endpoint, returnURL: returnURL, realm: realm, httpClient: client,
		usedNonces: make(map[string]time.Time),
	}, nil
}

func (s *Steam) AuthorizationURL(state string) (string, error) {
	endpoint, err := url.Parse(s.endpoint)
	if err != nil {
		return "", err
	}
	returnTo, err := url.Parse(s.returnURL)
	if err != nil {
		return "", err
	}
	query := returnTo.Query()
	query.Set("state", state)
	returnTo.RawQuery = query.Encode()

	values := endpoint.Query()
	values.Set("openid.ns", openIDNamespace)
	values.Set("openid.mode", "checkid_setup")
	values.Set("openid.return_to", returnTo.String())
	values.Set("openid.realm", s.realm)
	values.Set("openid.identity", openIDIdentifierSelect)
	values.Set("openid.claimed_id", openIDIdentifierSelect)
	endpoint.RawQuery = values.Encode()
	return endpoint.String(), nil
}

func (s *Steam) Authenticate(ctx context.Context, values url.Values) (Identity, error) {
	if values.Get("openid.mode") == "cancel" {
		return Identity{}, errors.New("Steam authentication was cancelled")
	}
	if values.Get("openid.ns") != openIDNamespace || values.Get("openid.mode") != "id_res" {
		return Identity{}, errors.New("invalid Steam OpenID response")
	}
	if values.Get("openid.op_endpoint") != s.endpoint {
		return Identity{}, errors.New("Steam OpenID response has an unexpected provider endpoint")
	}
	expectedReturnTo, err := s.expectedReturnTo(values.Get("state"))
	if err != nil || values.Get("openid.return_to") != expectedReturnTo {
		return Identity{}, errors.New("Steam OpenID response has an invalid return URL")
	}
	if err := validateSteamSignedFields(values); err != nil {
		return Identity{}, err
	}
	responseNonce := values.Get("openid.response_nonce")
	nonceTime, err := steamNonceTime(responseNonce)
	if err != nil || time.Since(nonceTime) < -steamNonceLifetime || time.Since(nonceTime) > steamNonceLifetime {
		return Identity{}, errors.New("Steam OpenID response has an invalid or stale nonce")
	}
	claimedID := values.Get("openid.claimed_id")
	if claimedID == "" || claimedID != values.Get("openid.identity") {
		return Identity{}, errors.New("Steam OpenID identity does not match claimed identity")
	}
	matches := steamClaimedIDPattern.FindStringSubmatch(claimedID)
	if matches == nil {
		return Identity{}, errors.New("Steam OpenID response has an invalid claimed identity")
	}

	verification := url.Values{}
	for key, entries := range values {
		if strings.HasPrefix(key, "openid.") {
			for _, entry := range entries {
				verification.Add(key, entry)
			}
		}
	}
	verification.Set("openid.mode", "check_authentication")
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, strings.NewReader(verification.Encode()))
	if err != nil {
		return Identity{}, fmt.Errorf("create Steam verification request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := s.httpClient.Do(request)
	if err != nil {
		return Identity{}, fmt.Errorf("verify Steam response: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Identity{}, fmt.Errorf("Steam verification returned HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if err != nil {
		return Identity{}, fmt.Errorf("read Steam verification response: %w", err)
	}
	verificationResponse := make(map[string]string)
	for _, line := range strings.Split(string(body), "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), ":")
		if found {
			verificationResponse[key] = value
		}
	}
	if verificationResponse["ns"] != openIDNamespace || verificationResponse["is_valid"] != "true" {
		return Identity{}, errors.New("Steam rejected the OpenID response")
	}
	if !s.acceptNonce(responseNonce, nonceTime) {
		return Identity{}, errors.New("Steam OpenID response nonce has already been used")
	}
	return Identity{Subject: matches[1]}, nil
}

func (s *Steam) expectedReturnTo(state string) (string, error) {
	if state == "" {
		return "", errors.New("Steam OpenID response is missing state")
	}
	returnTo, err := url.Parse(s.returnURL)
	if err != nil {
		return "", err
	}
	query := returnTo.Query()
	query.Set("state", state)
	returnTo.RawQuery = query.Encode()
	return returnTo.String(), nil
}

func validateSteamSignedFields(values url.Values) error {
	signed := make(map[string]struct{})
	for _, field := range strings.Split(values.Get("openid.signed"), ",") {
		signed[strings.TrimSpace(field)] = struct{}{}
	}
	for _, field := range []string{"op_endpoint", "return_to", "response_nonce", "assoc_handle", "claimed_id", "identity"} {
		if _, ok := signed[field]; !ok || values.Get("openid."+field) == "" {
			return fmt.Errorf("Steam OpenID response does not sign required field %q", field)
		}
	}
	return nil
}

func steamNonceTime(nonce string) (time.Time, error) {
	const timestampLength = len("2006-01-02T15:04:05Z")
	if len(nonce) < timestampLength || len(nonce) > 255 {
		return time.Time{}, errors.New("invalid nonce length")
	}
	return time.Parse("2006-01-02T15:04:05Z", nonce[:timestampLength])
}

func (s *Steam) acceptNonce(nonce string, nonceTime time.Time) bool {
	s.nonceMu.Lock()
	defer s.nonceMu.Unlock()

	cutoff := time.Now().Add(-steamNonceLifetime)
	for value, createdAt := range s.usedNonces {
		if createdAt.Before(cutoff) {
			delete(s.usedNonces, value)
		}
	}
	if _, exists := s.usedNonces[nonce]; exists {
		return false
	}
	s.usedNonces[nonce] = nonceTime
	return true
}
