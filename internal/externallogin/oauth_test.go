package externallogin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestOAuthAuthenticatesConfiguredSubjectFromWrappedOrFlatProfile(t *testing.T) {
	t.Parallel()

	providerServer := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/token.php":
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			if request.Form.Get("client_id") != "client" || request.Form.Get("client_secret") != "secret" {
				t.Errorf("token form = %v", request.Form)
			}
			_ = json.NewEncoder(writer).Encode(map[string]string{
				"access_token": request.Form.Get("code"),
				"token_type":   "Bearer",
			})
		case "/resource.php":
			switch request.Header.Get("Authorization") {
			case "Bearer wrapped":
				_ = json.NewEncoder(writer).Encode(map[string]any{
					"user": map[string]any{"id": 4242, "members_display_name": "Provider Name", "email": "ignored@example.com"},
				})
			case "Bearer flat":
				_ = json.NewEncoder(writer).Encode(map[string]any{
					"id": 4243, "members_display_name": "Flat Provider Name", "email": "ignored@example.com",
				})
			default:
				t.Errorf("authorization = %q", request.Header.Get("Authorization"))
				http.Error(writer, "unexpected token", http.StatusUnauthorized)
			}
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

	identity, err := provider.Authenticate(context.Background(), url.Values{"code": {"wrapped"}})
	if err != nil {
		t.Fatal(err)
	}
	if identity.Subject != "4242" {
		t.Fatalf("identity = %+v", identity)
	}

	identity, err = provider.Authenticate(context.Background(), url.Values{"code": {"flat"}})
	if err != nil {
		t.Fatal(err)
	}
	if identity.Subject != "4243" {
		t.Fatalf("flat identity = %+v", identity)
	}
}
