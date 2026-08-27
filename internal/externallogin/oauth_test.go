package externallogin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestOAuthAuthenticatesConfiguredSubjectFromWrappedProfile(t *testing.T) {
	t.Parallel()

	providerServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/token.php":
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if request.Form.Get("client_id") != "client" || request.Form.Get("client_secret") != "secret" ||
				request.Form.Get("code") != "code" {
				t.Errorf("token form = %v", request.Form)
			}
			_ = json.NewEncoder(writer).Encode(map[string]string{"access_token": "access", "token_type": "Bearer"})
		case "/resource.php":
			if request.Header.Get("Authorization") != "Bearer access" {
				t.Errorf("authorization = %q", request.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"user": map[string]any{"id": 4242, "members_display_name": "Provider Name", "email": "ignored@example.com"},
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer providerServer.Close()

	provider, err := NewOAuth(OAuthConfig{
		AuthorizationURL: providerServer.URL + "/authorize.php",
		TokenURL:         providerServer.URL + "/token.php",
		UserInfoURL:      providerServer.URL + "/resource.php",
		ClientID:         "client",
		ClientSecret:     "secret",
		RedirectURL:      "https://gatehouse.example/login/gamereplays/return",
		Scopes:           []string{"user_profile"},
		SubjectField:     "id",
		UserObjectField:  "user",
		TokenAuthMethod:  "client_secret_post",
	}, providerServer.Client())
	if err != nil {
		t.Fatal(err)
	}

	authorizationURL, err := provider.AuthorizationURL("state")
	if err != nil {
		t.Fatal(err)
	}
	parsedAuthorizationURL, _ := url.Parse(authorizationURL)
	if parsedAuthorizationURL.Query().Get("scope") != "user_profile" {
		t.Fatalf("authorization URL = %s", authorizationURL)
	}

	identity, err := provider.Authenticate(context.Background(), url.Values{"code": {"code"}})
	if err != nil {
		t.Fatal(err)
	}
	if identity.Subject != "4242" {
		t.Fatalf("identity = %+v", identity)
	}
}
