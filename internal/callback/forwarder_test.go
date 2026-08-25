package callback

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type testResolver map[string]BackendTarget

func (r testResolver) Resolve(environment string) (BackendTarget, bool) {
	target, ok := r[environment]
	return target, ok
}

func TestHTTPForwarderPassesCanonicalCallbackAndBackendKey(t *testing.T) {
	t.Parallel()

	var receivedCode string
	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Api-Key") != "backend-secret" {
			t.Errorf("X-Api-Key = %q", request.Header.Get("X-Api-Key"))
		}
		if request.Header.Get("X-Request-ID") != "request-1" {
			t.Errorf("X-Request-ID = %q", request.Header.Get("X-Request-ID"))
		}
		body, _ := io.ReadAll(request.Body)
		parsed, err := Parse(body, "")
		if err != nil {
			t.Errorf("Parse(forwarded) error = %v", err)
		} else {
			receivedCode = parsed.Code
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	forwarder := NewHTTPForwarder(testResolver{
		"example_alpha": {CallbackURL: backend.URL, APIKey: "backend-secret"},
	}, time.Second, discardLogger())
	accepted, err := forwarder.TryForward(t.Context(), testCallback(), "request-1", "incoming-secret")
	if err != nil {
		t.Fatalf("TryForward() error = %v", err)
	}
	if !accepted || receivedCode != "ABC123" {
		t.Fatalf("accepted=%v code=%q", accepted, receivedCode)
	}
}

func TestHTTPForwarderTreatsNon2xxAsFallback(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
	}))
	defer backend.Close()

	forwarder := NewHTTPForwarder(testResolver{
		"example_alpha": {CallbackURL: backend.URL, APIKey: "backend-secret"},
	}, time.Second, discardLogger())
	accepted, err := forwarder.TryForward(t.Context(), testCallback(), "", "incoming-secret")
	if err != nil {
		t.Fatalf("TryForward() error = %v", err)
	}
	if accepted {
		t.Fatal("accepted = true")
	}
}

func TestHTTPForwarderPassesInboundKeyWithoutOverride(t *testing.T) {
	t.Parallel()

	backend := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Api-Key") != "incoming-secret" {
			t.Errorf("X-Api-Key = %q", request.Header.Get("X-Api-Key"))
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()

	forwarder := NewHTTPForwarder(testResolver{
		"example_alpha": {CallbackURL: backend.URL},
	}, time.Second, discardLogger())
	accepted, err := forwarder.TryForward(t.Context(), testCallback(), "", "incoming-secret")
	if err != nil || !accepted {
		t.Fatalf("TryForward() accepted=%v error=%v", accepted, err)
	}
}
