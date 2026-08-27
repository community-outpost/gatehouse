package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/community-outpost/gatehouse/internal/callback"
	"github.com/community-outpost/gatehouse/internal/externallogin"
)

func TestFirstExternalLoginAlwaysPromptsForLocalDisplayName(t *testing.T) {
	t.Parallel()

	dispatcher := &fakeDispatcher{delivery: callback.DeliveryBackend}
	resolver := &fakePrincipalResolver{createdUserID: 99}
	provider := fakeLoginProvider{identity: externallogin.Identity{Subject: "4242"}}
	server := newTestServer(t, dispatcher, testLogger())
	if err := server.ConfigureLogin(resolver, LoginOptions{
		StateSecret: "state-secret",
		Providers: map[string]LoginProvider{
			"gamereplays": {Label: "GameReplays", Authenticator: provider},
		},
	}); err != nil {
		t.Fatal(err)
	}

	beginRequest := httptest.NewRequest(http.MethodGet, "/login/gamereplays?gamecode=abc123&env=example_alpha", nil)
	beginResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(beginResponse, beginRequest)
	if beginResponse.Code != http.StatusFound {
		t.Fatalf("begin status = %d body = %s", beginResponse.Code, beginResponse.Body)
	}
	loginCookie := responseCookie(t, beginResponse.Result(), loginCookieName("gamereplays"))
	authorizationURL, err := url.Parse(beginResponse.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}

	returnRequest := httptest.NewRequest(http.MethodGet,
		"/login/gamereplays/return?state="+url.QueryEscape(authorizationURL.Query().Get("state"))+"&code=provider-code", nil)
	returnRequest.AddCookie(loginCookie)
	returnResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(returnResponse, returnRequest)
	if returnResponse.Code != http.StatusOK || !strings.Contains(returnResponse.Body.String(), "Choose your display name") {
		t.Fatalf("return status = %d body = %s", returnResponse.Code, returnResponse.Body)
	}
	for _, expected := range []string{
		"/assets/display-name.js", "data-display-name-form", "data-random-name", `aria-live="polite"`,
		`minlength="3"`, `maxlength="16"`,
	} {
		if !strings.Contains(returnResponse.Body.String(), expected) {
			t.Errorf("display-name page does not contain %q", expected)
		}
	}
	if csp := returnResponse.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") {
		t.Errorf("Content-Security-Policy does not allow the same-origin validation script: %q", csp)
	}
	if resolver.createCalls != 0 || dispatcher.calls != 0 {
		t.Fatalf("created = %d dispatched = %d before display-name submission", resolver.createCalls, dispatcher.calls)
	}

	enrollmentCookie := responseCookie(t, returnResponse.Result(), enrollmentCookieName("gamereplays"))
	match := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(returnResponse.Body.String())
	if len(match) != 2 {
		t.Fatalf("CSRF token not found in form: %s", returnResponse.Body)
	}
	form := url.Values{"csrf": {match[1]}, "display_name": {"  Local   General  "}}
	completeRequest := httptest.NewRequest(http.MethodPost, "/login/gamereplays/return", strings.NewReader(form.Encode()))
	completeRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	completeRequest.AddCookie(enrollmentCookie)
	completeResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(completeResponse, completeRequest)

	if completeResponse.Code != http.StatusOK || !strings.Contains(completeResponse.Body.String(), "Login complete") {
		t.Fatalf("complete status = %d body = %s", completeResponse.Code, completeResponse.Body)
	}
	if resolver.createCalls != 1 || resolver.displayName != "Local General" {
		t.Fatalf("create calls = %d display name = %q", resolver.createCalls, resolver.displayName)
	}
	if dispatcher.calls != 1 || dispatcher.received.UserID != 99 {
		t.Fatalf("dispatcher calls = %d callback = %+v", dispatcher.calls, dispatcher.received)
	}
}

func TestDisplayNameScriptAsset(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeDispatcher{}, testLogger())
	request := httptest.NewRequest(http.MethodGet, "/assets/display-name.js", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "text/javascript; charset=utf-8" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	for _, expected := range []string{"setCustomValidity", "reportValidity", "allowedSymbols", "getRandomValues", "adjectives", "nouns"} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Errorf("validation script does not contain %q", expected)
		}
	}
}

func TestLoginRequiresEnvironmentFromGame(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeDispatcher{}, testLogger())
	if err := server.ConfigureLogin(&fakePrincipalResolver{}, LoginOptions{
		StateSecret: "state-secret",
		Providers: map[string]LoginProvider{
			"gamereplays": {Label: "GameReplays", Authenticator: fakeLoginProvider{}},
		},
	}); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/login/gamereplays?gamecode=abc123", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", response.Code, response.Body)
	}
}

func TestLoginChooserUsesConfiguredBrandingAndProviderIcon(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeDispatcher{delivery: callback.DeliveryBackend}, testLogger())
	if err := server.ConfigureLogin(&fakePrincipalResolver{}, LoginOptions{
		StateSecret: "state-secret",
		Branding: LoginBranding{
			ServiceName:     "Operator Gate",
			OperatorName:    "Example Operator",
			ApplicationName: "Example Game",
			AccentColor:     "#12dce8",
			BackgroundColor: "#020405",
		},
		Providers: map[string]LoginProvider{
			"discord": {
				Label:       "Discord",
				Description: "Use the community Discord",
				Icon:        "discord",
				RedirectURL: "https://example.com/login",
			},
		},
	}); err != nil {
		t.Fatal(err)
	}

	pageRequest := httptest.NewRequest(http.MethodGet, "/login?gamecode=abc123&env=example_alpha", nil)
	pageResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(pageResponse, pageRequest)
	body := pageResponse.Body.String()
	for _, expected := range []string{
		"Sign in to Example Game",
		"Operated by Example Operator",
		"Use the community Discord",
		"/assets/providers/discord",
		"--accent: #12dce8; --page: #020405",
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("login page does not contain %q", expected)
		}
	}

	iconRequest := httptest.NewRequest(http.MethodGet, "/assets/providers/discord", nil)
	iconResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(iconResponse, iconRequest)
	if iconResponse.Code != http.StatusOK || iconResponse.Header().Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("icon status = %d content-type = %q", iconResponse.Code, iconResponse.Header().Get("Content-Type"))
	}
}

type fakeLoginProvider struct {
	identity externallogin.Identity
}

func (f fakeLoginProvider) AuthorizationURL(state string) (string, error) {
	return "https://provider.example/auth?state=" + url.QueryEscape(state), nil
}

func (f fakeLoginProvider) Authenticate(context.Context, url.Values) (externallogin.Identity, error) {
	return f.identity, nil
}

type fakePrincipalResolver struct {
	createdUserID   int64
	createCalls     int
	displayName     string
	issuer          string
	subject         string
	nameUnavailable bool
}

func (f *fakePrincipalResolver) FindUser(context.Context, string, string) (int64, bool, error) {
	return 0, false, nil
}

func (f *fakePrincipalResolver) DisplayNameAvailable(context.Context, string) (bool, error) {
	return !f.nameUnavailable, nil
}

func (f *fakePrincipalResolver) ResolveOrCreateUser(_ context.Context, issuer, subject, displayName string) (int64, error) {
	f.createCalls++
	f.displayName = displayName
	f.issuer = issuer
	f.subject = subject
	return f.createdUserID, nil
}

func responseCookie(t *testing.T, response *http.Response, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Cookies() {
		if cookie.Name == name && cookie.MaxAge >= 0 {
			return cookie
		}
	}
	t.Fatalf("response cookie %q not found", name)
	return nil
}
