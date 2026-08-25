package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTrustedProxyUsesRightmostUntrustedForwardedAddress(t *testing.T) {
	t.Parallel()

	middleware, err := trustedProxyClientIP([]string{"172.18.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}
	var remoteAddress string
	handler := middleware(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		remoteAddress = request.RemoteAddr
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "172.18.0.2:43120"
	request.Header.Set("X-Forwarded-For", "198.51.100.7, 203.0.113.9")

	handler.ServeHTTP(httptest.NewRecorder(), request)

	if remoteAddress != "203.0.113.9" {
		t.Fatalf("RemoteAddr = %q", remoteAddress)
	}
}

func TestTrustedProxySkipsTrustedAddressesInForwardedChain(t *testing.T) {
	t.Parallel()

	middleware, err := trustedProxyClientIP([]string{"172.18.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}
	var remoteAddress string
	handler := middleware(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		remoteAddress = request.RemoteAddr
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "172.18.0.2:43120"
	request.Header.Set("X-Forwarded-For", "198.51.100.7, 172.18.0.3")

	handler.ServeHTTP(httptest.NewRecorder(), request)

	if remoteAddress != "198.51.100.7" {
		t.Fatalf("RemoteAddr = %q", remoteAddress)
	}
}

func TestUntrustedPeerCannotOverrideClientAddress(t *testing.T) {
	t.Parallel()

	middleware, err := trustedProxyClientIP([]string{"172.18.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}
	var remoteAddress string
	handler := middleware(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		remoteAddress = request.RemoteAddr
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "203.0.113.20:43120"
	request.Header.Set("X-Forwarded-For", "198.51.100.7")

	handler.ServeHTTP(httptest.NewRecorder(), request)

	if remoteAddress != "203.0.113.20:43120" {
		t.Fatalf("RemoteAddr = %q", remoteAddress)
	}
}

func TestMalformedForwardedChainFailsClosed(t *testing.T) {
	t.Parallel()

	middleware, err := trustedProxyClientIP([]string{"172.18.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}
	var remoteAddress string
	handler := middleware(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		remoteAddress = request.RemoteAddr
	}))
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.RemoteAddr = "172.18.0.2:43120"
	request.Header.Set("X-Forwarded-For", "198.51.100.7, invalid")

	handler.ServeHTTP(httptest.NewRecorder(), request)

	if remoteAddress != "172.18.0.2:43120" {
		t.Fatalf("RemoteAddr = %q", remoteAddress)
	}
}
