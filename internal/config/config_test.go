package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestValidateNormalizesBackendEnvironment(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ListenAddress:        ":8080",
		InboundAPIKey:        "0123456789abcdef0123456789abcdef",
		BackendTimeout:       Duration{Duration: 5 * time.Second},
		ShutdownTimeout:      Duration{Duration: 10 * time.Second},
		MaxCallbackBodyBytes: 65536,
		MySQL:                validMySQLConfig("user:pass@tcp(localhost:3306)/database"),
		Backends: BackendsConfig{
			Static: map[string]BackendConfig{
				"Example_Alpha": {
					CallbackURL: "https://alpha.example/LoginCode",
					APIKey:      "backend-secret",
				},
			},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if _, ok := cfg.Backends.Static["example_alpha"]; !ok {
		t.Fatalf("Backends = %#v", cfg.Backends)
	}
}

func TestExampleConfigLoads(t *testing.T) {
	for _, variable := range []string{
		"GATEHOUSE_ALLOW_UNSAFE_INBOUND_API_KEY",
		"GATEHOUSE_BACKEND_TIMEOUT",
		"GATEHOUSE_DOCKER_HOST",
		"GATEHOUSE_INBOUND_API_KEY_FILE",
		"GATEHOUSE_MAX_CALLBACK_BODY_BYTES",
		"GATEHOUSE_MYSQL_ADVISORY_LOCK_TIMEOUT_SECONDS",
		"GATEHOUSE_MYSQL_DSN_FILE",
		"GATEHOUSE_SHUTDOWN_TIMEOUT",
	} {
		t.Setenv(variable, "")
	}
	t.Setenv("GATEHOUSE_CONFIG", filepath.Join("..", "..", "config.example.yaml"))
	t.Setenv("GATEHOUSE_MYSQL_DSN", "user:pass@tcp(db.example.internal:3306)/gatehouse")
	t.Setenv("GATEHOUSE_INBOUND_API_KEY", "secret")
	t.Setenv("GATEHOUSE_ALLOW_UNSAFE_INBOUND_API_KEY", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.MySQL.UsersTable != "users" || cfg.MySQL.PendingLoginsTable != "pending_logins" {
		t.Fatalf("MySQL config = %+v", cfg.MySQL)
	}
	if !cfg.Backends.Docker.Enabled || cfg.Backends.Docker.Network != "gatehouse" {
		t.Fatalf("Docker backend config = %+v", cfg.Backends.Docker)
	}
	if cfg.Backends.Docker.LabelPrefix != "com.community-outpost.gatehouse" {
		t.Fatalf("Docker label prefix = %q", cfg.Backends.Docker.LabelPrefix)
	}
	if cfg.Backends.Docker.DefaultPath != "/env/{environment}/contract/1/LoginCode" {
		t.Fatalf("Docker default path = %q", cfg.Backends.Docker.DefaultPath)
	}
	if len(cfg.TrustedProxies) != 0 {
		t.Fatalf("TrustedProxies = %#v", cfg.TrustedProxies)
	}
	if !cfg.AllowUnsafeInboundAPIKey {
		t.Fatal("AllowUnsafeInboundAPIKey = false")
	}
	if issuer := cfg.Authentication.GORedirectIssuer(); issuer != "generalsonline" {
		t.Fatalf("GORedirectIssuer() = %q", issuer)
	}
}

func TestResolveSecretFiles(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	mysqlPath := filepath.Join(directory, "mysql")
	keyPath := filepath.Join(directory, "key")
	if err := os.WriteFile(mysqlPath, []byte("user:pass@tcp(db.example.internal:3306)/gatehouse\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, []byte("callback-secret\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		MySQL:             MySQLConfig{DSNFile: mysqlPath},
		InboundAPIKeyFile: keyPath,
		Authentication: AuthenticationConfig{Providers: map[string]ProviderConfig{
			"provider": {ClientSecretFile: keyPath},
		}},
		Backends: BackendsConfig{
			Docker: DockerConfig{Overrides: map[string]DockerOverrideConfig{
				"example_alpha": {APIKeyFile: keyPath},
			}},
			Static: map[string]BackendConfig{
				"example_beta": {APIKeyFile: keyPath},
			},
		},
	}
	if err := cfg.resolveSecretFiles(); err != nil {
		t.Fatalf("resolveSecretFiles() error = %v", err)
	}
	if cfg.MySQL.DSN != "user:pass@tcp(db.example.internal:3306)/gatehouse" || cfg.InboundAPIKey != "callback-secret" {
		t.Fatalf("resolved config = %+v", cfg)
	}
	if cfg.MySQL.DSNFile != "" || cfg.InboundAPIKeyFile != "" ||
		cfg.Authentication.Providers["provider"].ClientSecretFile != "" ||
		cfg.Backends.Docker.Overrides["example_alpha"].APIKeyFile != "" ||
		cfg.Backends.Static["example_beta"].APIKeyFile != "" {
		t.Fatalf("resolved secret file fields were not cleared: %+v", cfg)
	}
}

func TestValidateRejectsArbitraryBackendScheme(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ListenAddress:        ":8080",
		InboundAPIKey:        "0123456789abcdef0123456789abcdef",
		BackendTimeout:       Duration{Duration: 5 * time.Second},
		ShutdownTimeout:      Duration{Duration: 10 * time.Second},
		MaxCallbackBodyBytes: 65536,
		MySQL:                validMySQLConfig("dsn"),
		Backends: BackendsConfig{
			Static: map[string]BackendConfig{
				"example_alpha": {CallbackURL: "file:///etc/passwd", APIKey: "secret"},
			},
		},
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil")
	}
}

func TestValidateRejectsStaticBackendWithoutURL(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.Backends.Static["example_alpha"] = BackendConfig{APIKey: "backend-secret"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil")
	}
}

func TestValidateRejectsStaticBackendWithoutDedicatedKey(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.Backends.Static["example_alpha"] = BackendConfig{CallbackURL: "https://alpha.example/LoginCode"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil")
	}
}

func TestValidateRejectsWeakInboundAPIKey(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.InboundAPIKey = "secret"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil")
	}
}

func TestValidateAllowsUnsafeInboundAPIKeyWhenExplicitlyEnabled(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"secret", "CHANGE_ME"} {
		key := key
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			cfg := validConfig()
			cfg.InboundAPIKey = key
			cfg.AllowUnsafeInboundAPIKey = true
			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestValidateRejectsEmptyInboundAPIKeyWithUnsafeOverride(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.InboundAPIKey = ""
	cfg.AllowUnsafeInboundAPIKey = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil")
	}
}

func TestLoadRejectsInvalidAllowUnsafeInboundAPIKeyEnvironmentOverride(t *testing.T) {
	t.Setenv("GATEHOUSE_CONFIG", "")
	t.Setenv("GATEHOUSE_ALLOW_UNSAFE_INBOUND_API_KEY", "not-a-boolean")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil")
	}
}

func TestValidateNormalizesDockerOverride(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.Backends.Docker.Overrides = map[string]DockerOverrideConfig{
		"Example_Alpha": {APIKey: "backend-secret"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if _, ok := cfg.Backends.Docker.Overrides["example_alpha"]; !ok {
		t.Fatalf("Docker overrides = %#v", cfg.Backends.Docker.Overrides)
	}
}

func TestValidateRejectsUnsafeMySQLIdentifier(t *testing.T) {
	t.Parallel()
	cfg := Config{
		ListenAddress:        ":8080",
		InboundAPIKey:        "0123456789abcdef0123456789abcdef",
		BackendTimeout:       Duration{Duration: 5 * time.Second},
		ShutdownTimeout:      Duration{Duration: 10 * time.Second},
		MaxCallbackBodyBytes: 65536,
		MySQL:                validMySQLConfig("user:pass@tcp(localhost:3306)/gatehouse"),
		Backends:             BackendsConfig{Static: map[string]BackendConfig{}},
	}
	cfg.MySQL.UsersTable = "users; DROP TABLE users"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil")
	}
}

func TestValidateRejectsInvalidTrustedProxy(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.TrustedProxies = []string{"172.18.0.0/16", "not-a-cidr"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() error = nil")
	}
}

func validMySQLConfig(dsn string) MySQLConfig {
	return MySQLConfig{
		DSN:                        dsn,
		UsersTable:                 "users",
		PendingLoginsTable:         "pending_logins",
		StartupTimeout:             Duration{Duration: 10 * time.Second},
		AdvisoryLockTimeoutSeconds: 5,
	}
}

func validConfig() Config {
	return Config{
		ListenAddress:        ":8080",
		InboundAPIKey:        "0123456789abcdef0123456789abcdef",
		BackendTimeout:       Duration{Duration: 5 * time.Second},
		ShutdownTimeout:      Duration{Duration: 10 * time.Second},
		MaxCallbackBodyBytes: 65536,
		MySQL:                validMySQLConfig("user:pass@tcp(localhost:3306)/gatehouse"),
		Backends: BackendsConfig{
			Docker: DockerConfig{
				Enabled:         true,
				Host:            "unix:///var/run/docker.sock",
				Network:         "gatehouse",
				RefreshInterval: Duration{Duration: 5 * time.Second},
				LabelPrefix:     "com.community-outpost.gatehouse",
				DefaultScheme:   "http",
				DefaultPort:     8080,
				DefaultPath:     "/env/{environment}/contract/1/LoginCode",
				Overrides:       map[string]DockerOverrideConfig{},
			},
			Static: map[string]BackendConfig{},
		},
	}
}
