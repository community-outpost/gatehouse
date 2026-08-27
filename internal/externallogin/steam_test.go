package externallogin

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestSteamAuthenticateValidatesBoundAssertion(t *testing.T) {
	t.Parallel()

	provider := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Error(err)
		}
		if request.Form.Get("openid.mode") != "check_authentication" {
			t.Errorf("openid.mode = %q", request.Form.Get("openid.mode"))
		}
		fmt.Fprintf(writer, "ns:%s\nis_valid:true\n", openIDNamespace)
	}))
	defer provider.Close()

	steam, err := NewSteam(provider.URL, "https://gatehouse.example/login/steam/return", "https://gatehouse.example/", provider.Client())
	if err != nil {
		t.Fatal(err)
	}
	values := validSteamAssertion(provider.URL, "state-token")
	identity, err := steam.Authenticate(t.Context(), values)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if identity.Subject != "76561198000000000" {
		t.Fatalf("subject = %q", identity.Subject)
	}
	if _, err := steam.Authenticate(t.Context(), values); err == nil {
		t.Fatal("replayed assertion was accepted")
	}
}

func TestSteamAuthenticateRejectsAssertionForAnotherReturnURL(t *testing.T) {
	t.Parallel()

	called := false
	provider := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	defer provider.Close()
	steam, err := NewSteam(provider.URL, "https://gatehouse.example/login/steam/return", "https://gatehouse.example/", provider.Client())
	if err != nil {
		t.Fatal(err)
	}
	values := validSteamAssertion(provider.URL, "state-token")
	values.Set("openid.return_to", "https://attacker.example/return")

	if _, err := steam.Authenticate(t.Context(), values); err == nil {
		t.Fatal("assertion for another return URL was accepted")
	}
	if called {
		t.Fatal("provider verification was attempted for an unbound assertion")
	}
}

func TestSteamAuthenticateRejectsUnsignedIdentity(t *testing.T) {
	t.Parallel()

	provider := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer provider.Close()
	steam, err := NewSteam(provider.URL, "https://gatehouse.example/login/steam/return", "https://gatehouse.example/", provider.Client())
	if err != nil {
		t.Fatal(err)
	}
	values := validSteamAssertion(provider.URL, "state-token")
	values.Set("openid.signed", "op_endpoint,return_to,response_nonce,assoc_handle,claimed_id")

	if _, err := steam.Authenticate(t.Context(), values); err == nil {
		t.Fatal("assertion with unsigned identity was accepted")
	}
}

func validSteamAssertion(endpoint, state string) url.Values {
	returnTo := "https://gatehouse.example/login/steam/return?state=" + url.QueryEscape(state)
	return url.Values{
		"state":                 {state},
		"openid.ns":             {openIDNamespace},
		"openid.mode":           {"id_res"},
		"openid.op_endpoint":    {endpoint},
		"openid.return_to":      {returnTo},
		"openid.response_nonce": {time.Now().UTC().Format("2006-01-02T15:04:05Z") + "nonce"},
		"openid.assoc_handle":   {"association"},
		"openid.claimed_id":     {"https://steamcommunity.com/openid/id/76561198000000000"},
		"openid.identity":       {"https://steamcommunity.com/openid/id/76561198000000000"},
		"openid.signed":         {"op_endpoint,return_to,response_nonce,assoc_handle,claimed_id,identity"},
		"openid.sig":            {"signature"},
	}
}
