package httpapi

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/community-outpost/gatehouse/internal/callback"
)

func TestCallbackRequiresAPIKey(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, &fakeDispatcher{}, testLogger())
	request := httptest.NewRequest(http.MethodPost, "/LoginCode", strings.NewReader(`{}`))
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestServicesCompatibleRouteDispatchesAndReportsBackendDelivery(t *testing.T) {
	t.Parallel()

	dispatcher := &fakeDispatcher{delivery: callback.DeliveryBackend}
	server := newTestServer(t, dispatcher, testLogger())
	request := httptest.NewRequest(http.MethodPost,
		"/env/example_alpha/contract/1/LoginCode",
		strings.NewReader(`{"code":"abc123","user_id":42,"success":true}`))
	request.Header.Set("X-Api-Key", "secret")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d body = %s", response.Code, body)
	}
	if response.Header().Get("X-Gatehouse-Delivery") != "backend" {
		t.Fatalf("delivery = %q", response.Header().Get("X-Gatehouse-Delivery"))
	}
	if dispatcher.received.Environment != "example_alpha" || dispatcher.received.Code != "ABC123" {
		t.Fatalf("callback = %+v", dispatcher.received)
	}
}

func TestUnsupportedContractVersionDoesNotDispatch(t *testing.T) {
	t.Parallel()

	dispatcher := &fakeDispatcher{delivery: callback.DeliveryBackend}
	server := newTestServer(t, dispatcher, testLogger())
	request := httptest.NewRequest(http.MethodPost,
		"/env/example_alpha/contract/2/LoginCode",
		strings.NewReader(`{"code":"ABC123","user_id":42,"success":true}`))
	request.Header.Set("X-Api-Key", "secret")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", response.Code)
	}
	if dispatcher.calls != 0 {
		t.Fatalf("dispatcher calls = %d", dispatcher.calls)
	}
}

func TestCanonicalRouteReportsMySQLFallback(t *testing.T) {
	t.Parallel()

	dispatcher := &fakeDispatcher{delivery: callback.DeliveryMySQLFallback}
	server := newTestServer(t, dispatcher, testLogger())
	request := httptest.NewRequest(http.MethodPost, "/LoginCode",
		strings.NewReader(`{"env":"example_legacy","code":"ABC","user_id":-1,"success":false}`))
	request.Header.Set("X-Api-Key", "secret")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d", response.Code)
	}
	if response.Header().Get("X-Gatehouse-Delivery") != "mysql-fallback" {
		t.Fatalf("delivery = %q", response.Header().Get("X-Gatehouse-Delivery"))
	}
}

func TestMissingUserIDDoesNotDispatch(t *testing.T) {
	t.Parallel()

	dispatcher := &fakeDispatcher{delivery: callback.DeliveryMySQLFallback}
	server := newTestServer(t, dispatcher, testLogger())
	request := httptest.NewRequest(http.MethodPost, "/LoginCode",
		strings.NewReader(`{"env":"example_legacy","code":"ABC","success":false}`))
	request.Header.Set("X-Api-Key", "secret")
	response := httptest.NewRecorder()

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s", response.Code, response.Body)
	}
	if dispatcher.calls != 0 {
		t.Fatalf("dispatcher calls = %d", dispatcher.calls)
	}
}

func TestCallbackLogIncludesFullBodyAndExcludesAPIKey(t *testing.T) {
	t.Parallel()

	dispatcher := &fakeDispatcher{delivery: callback.DeliveryBackend}
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	server := newTestServer(t, dispatcher, logger)
	body := `{"env":"example_debug","code":"FULLCODE","user_id":42,"success":true}`
	callbackRequest := httptest.NewRequest(http.MethodPost, "/LoginCode", strings.NewReader(body))
	callbackRequest.Header.Set("X-Api-Key", "secret")
	callbackResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(callbackResponse, callbackRequest)
	if callbackResponse.Code != http.StatusNoContent {
		t.Fatalf("callback status = %d", callbackResponse.Code)
	}

	logged := output.String()
	for _, expected := range []string{"FULLCODE", "example_debug", "callback.delivery", "backend"} {
		if !strings.Contains(logged, expected) {
			t.Fatalf("log does not contain %q: %s", expected, logged)
		}
	}
	if strings.Contains(logged, "secret") {
		t.Fatalf("log contains inbound API key: %s", logged)
	}
}

type fakeDispatcher struct {
	delivery callback.Delivery
	received callback.AuthCallback
	calls    int
}

func (f *fakeDispatcher) Dispatch(_ context.Context, received callback.AuthCallback, _, _ string) (callback.Delivery, error) {
	f.calls++
	f.received = received
	return f.delivery, nil
}

type fakeReadiness struct{}

func (fakeReadiness) Ping(context.Context) error { return nil }

func newTestServer(t *testing.T, dispatcher Dispatcher, logger *slog.Logger) *Server {
	t.Helper()
	server, err := New(dispatcher, fakeReadiness{}, "secret", 65536, nil, logger)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
