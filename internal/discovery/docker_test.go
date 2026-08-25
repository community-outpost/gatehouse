package discovery

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/community-outpost/gatehouse/internal/config"
)

func TestEnvironmentLabelCreatesDefaultBackend(t *testing.T) {
	t.Parallel()

	server := dockerServer(t, []map[string]any{
		{
			"Id": "container-1",
			"Labels": map[string]string{
				"com.community-outpost.gatehouse.environment": "example_alpha",
			},
			"NetworkSettings": map[string]any{"Networks": map[string]any{
				"gatehouse": map[string]string{"IPAddress": "172.30.0.12"},
			}},
		},
	})
	defer server.Close()

	resolver, err := New(dockerConfig(server.URL), nil, testLogger())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := resolver.Refresh(t.Context()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	target, ok := resolver.Resolve("example_alpha")
	if !ok {
		t.Fatal("Resolve() found no target")
	}
	want := "http://172.30.0.12:8080/env/example_alpha/contract/1/LoginCode"
	if target.CallbackURL != want || target.APIKey != "" {
		t.Fatalf("target = %+v, want URL %q", target, want)
	}
}

func TestLabelsOverrideDefaultsAndYAMLOverridesKey(t *testing.T) {
	t.Parallel()

	server := dockerServer(t, []map[string]any{
		{
			"Id": "container-1",
			"Labels": map[string]string{
				"com.community-outpost.gatehouse.environment": "example_beta",
				"com.community-outpost.gatehouse.scheme":      "https",
				"com.community-outpost.gatehouse.host":        "beta.internal",
				"com.community-outpost.gatehouse.port":        "9443",
				"com.community-outpost.gatehouse.path":        "/LoginCode?contract=1",
			},
			"NetworkSettings": map[string]any{"Networks": map[string]any{}},
		},
	})
	defer server.Close()

	cfg := dockerConfig(server.URL)
	cfg.Overrides = map[string]config.DockerOverrideConfig{
		"example_beta": {APIKey: "override-secret"},
	}
	resolver, err := New(cfg, nil, testLogger())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := resolver.Refresh(t.Context()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	target, ok := resolver.Resolve("example_beta")
	if !ok || target.CallbackURL != "https://beta.internal:9443/LoginCode?contract=1" || target.APIKey != "override-secret" {
		t.Fatalf("target = %+v, ok=%v", target, ok)
	}
}

func TestYAMLOverridesDockerLabels(t *testing.T) {
	t.Parallel()

	server := dockerServer(t, []map[string]any{
		{
			"Id": "container-1",
			"Labels": map[string]string{
				"com.community-outpost.gatehouse.environment": "example_beta",
				"com.community-outpost.gatehouse.scheme":      "http",
				"com.community-outpost.gatehouse.host":        "label.internal",
				"com.community-outpost.gatehouse.port":        "8080",
				"com.community-outpost.gatehouse.path":        "/from-label",
			},
			"NetworkSettings": map[string]any{"Networks": map[string]any{}},
		},
	})
	defer server.Close()

	cfg := dockerConfig(server.URL)
	cfg.Overrides = map[string]config.DockerOverrideConfig{
		"example_beta": {
			Scheme: "https",
			Host:   "config.internal",
			Port:   9443,
			Path:   "/env/{environment}/LoginCode?source=yaml",
		},
	}
	resolver, err := New(cfg, nil, testLogger())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := resolver.Refresh(t.Context()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	target, ok := resolver.Resolve("example_beta")
	want := "https://config.internal:9443/env/example_beta/LoginCode?source=yaml"
	if !ok || target.CallbackURL != want {
		t.Fatalf("target = %+v, ok=%v, want URL %q", target, ok, want)
	}
}

func TestStaticURLTakesPrecedence(t *testing.T) {
	t.Parallel()

	cfg := dockerConfig("http://unused")
	cfg.Enabled = false
	resolver, err := New(cfg, map[string]config.BackendConfig{
		"example_beta": {CallbackURL: "https://static.example/LoginCode", APIKey: "static-key"},
	}, testLogger())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	target, ok := resolver.Resolve("example_beta")
	if !ok || target.CallbackURL != "https://static.example/LoginCode" || target.APIKey != "static-key" {
		t.Fatalf("target = %+v, ok=%v", target, ok)
	}
}

func dockerConfig(host string) config.DockerConfig {
	return config.DockerConfig{
		Enabled:         true,
		Host:            host,
		Network:         "gatehouse",
		RefreshInterval: config.Duration{Duration: time.Second},
		LabelPrefix:     "com.community-outpost.gatehouse",
		DefaultScheme:   "http",
		DefaultPort:     8080,
		DefaultPath:     "/env/{environment}/contract/1/LoginCode",
	}
}

func dockerServer(t *testing.T, containers []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/containers/json" {
			t.Errorf("path = %q", request.URL.Path)
		}
		filters, err := url.QueryUnescape(request.URL.Query().Get("filters"))
		if err != nil || !strings.Contains(filters, "com.community-outpost.gatehouse.environment") {
			t.Errorf("filters = %q, error=%v", filters, err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(containers)
	}))
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
