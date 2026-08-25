package config

import (
	"errors"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

var environmentPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
var mysqlIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)

type Config struct {
	ListenAddress        string         `yaml:"listen_address"`
	InboundAPIKey        string         `yaml:"inbound_api_key"`
	InboundAPIKeyFile    string         `yaml:"inbound_api_key_file"`
	BackendTimeout       Duration       `yaml:"backend_timeout"`
	ShutdownTimeout      Duration       `yaml:"shutdown_timeout"`
	MaxCallbackBodyBytes int64          `yaml:"max_callback_body_bytes"`
	TrustedProxies       []string       `yaml:"trusted_proxies"`
	MySQL                MySQLConfig    `yaml:"mysql"`
	Backends             BackendsConfig `yaml:"backends"`
}

type MySQLConfig struct {
	DSN                        string   `yaml:"dsn"`
	DSNFile                    string   `yaml:"dsn_file"`
	UsersTable                 string   `yaml:"users_table"`
	PendingLoginsTable         string   `yaml:"pending_logins_table"`
	StartupTimeout             Duration `yaml:"startup_timeout"`
	AdvisoryLockTimeoutSeconds int      `yaml:"advisory_lock_timeout_seconds"`
}

type BackendsConfig struct {
	Docker DockerConfig             `yaml:"docker"`
	Static map[string]BackendConfig `yaml:"static"`
}

type DockerConfig struct {
	Enabled         bool                            `yaml:"enabled"`
	Host            string                          `yaml:"host"`
	Network         string                          `yaml:"network"`
	RefreshInterval Duration                        `yaml:"refresh_interval"`
	LabelPrefix     string                          `yaml:"label_prefix"`
	DefaultScheme   string                          `yaml:"default_scheme"`
	DefaultPort     int                             `yaml:"default_port"`
	DefaultPath     string                          `yaml:"default_path"`
	Overrides       map[string]DockerOverrideConfig `yaml:"overrides"`
}

type DockerOverrideConfig struct {
	Scheme     string `yaml:"scheme"`
	Host       string `yaml:"host"`
	Port       int    `yaml:"port"`
	Path       string `yaml:"path"`
	APIKey     string `yaml:"api_key"`
	APIKeyFile string `yaml:"api_key_file"`
}

type BackendConfig struct {
	CallbackURL string `yaml:"callback_url"`
	APIKey      string `yaml:"api_key"`
	APIKeyFile  string `yaml:"api_key_file"`
}

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalText(value []byte) error {
	parsed, err := time.ParseDuration(string(value))
	if err != nil {
		return err
	}
	d.Duration = parsed
	return nil
}

func Load() (Config, error) {
	cfg := Config{
		ListenAddress:        ":8080",
		BackendTimeout:       Duration{Duration: 5 * time.Second},
		ShutdownTimeout:      Duration{Duration: 10 * time.Second},
		MaxCallbackBodyBytes: 64 * 1024,
		MySQL: MySQLConfig{
			UsersTable:                 "users",
			PendingLoginsTable:         "pending_logins",
			StartupTimeout:             Duration{Duration: 10 * time.Second},
			AdvisoryLockTimeoutSeconds: 5,
		},
		Backends: BackendsConfig{
			Docker: DockerConfig{
				Enabled:         true,
				Host:            "unix:///var/run/docker.sock",
				RefreshInterval: Duration{Duration: 5 * time.Second},
				LabelPrefix:     "com.community-outpost.gatehouse",
				DefaultScheme:   "http",
				DefaultPort:     8080,
				DefaultPath:     "/env/{environment}/contract/1/LoginCode",
				Overrides:       make(map[string]DockerOverrideConfig),
			},
			Static: make(map[string]BackendConfig),
		},
	}

	if path := strings.TrimSpace(os.Getenv("GATEHOUSE_CONFIG")); path != "" {
		contents, err := os.ReadFile(path)
		if err != nil {
			return Config{}, fmt.Errorf("read config file: %w", err)
		}
		decoder := yaml.NewDecoder(strings.NewReader(string(contents)))
		decoder.KnownFields(true)
		if err := decoder.Decode(&cfg); err != nil {
			return Config{}, fmt.Errorf("decode config file: %w", err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return Config{}, errors.New("config file must contain exactly one YAML document")
		}
	}

	if value := os.Getenv("GATEHOUSE_LISTEN_ADDRESS"); value != "" {
		cfg.ListenAddress = value
	}
	if value := os.Getenv("GATEHOUSE_MYSQL_DSN"); value != "" {
		cfg.MySQL.DSN = value
	}
	if value := os.Getenv("GATEHOUSE_MYSQL_DSN_FILE"); value != "" {
		cfg.MySQL.DSNFile = value
	}
	if value := os.Getenv("GATEHOUSE_INBOUND_API_KEY"); value != "" {
		cfg.InboundAPIKey = value
	}
	if value := os.Getenv("GATEHOUSE_INBOUND_API_KEY_FILE"); value != "" {
		cfg.InboundAPIKeyFile = value
	}
	if value := os.Getenv("GATEHOUSE_BACKEND_TIMEOUT"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse GATEHOUSE_BACKEND_TIMEOUT: %w", err)
		}
		cfg.BackendTimeout.Duration = parsed
	}
	if value := os.Getenv("GATEHOUSE_SHUTDOWN_TIMEOUT"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse GATEHOUSE_SHUTDOWN_TIMEOUT: %w", err)
		}
		cfg.ShutdownTimeout.Duration = parsed
	}
	if value := os.Getenv("GATEHOUSE_MAX_CALLBACK_BODY_BYTES"); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("parse GATEHOUSE_MAX_CALLBACK_BODY_BYTES: %w", err)
		}
		cfg.MaxCallbackBodyBytes = parsed
	}
	if value := os.Getenv("GATEHOUSE_MYSQL_ADVISORY_LOCK_TIMEOUT_SECONDS"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf("parse GATEHOUSE_MYSQL_ADVISORY_LOCK_TIMEOUT_SECONDS: %w", err)
		}
		cfg.MySQL.AdvisoryLockTimeoutSeconds = parsed
	}
	if value := os.Getenv("GATEHOUSE_DOCKER_HOST"); value != "" {
		cfg.Backends.Docker.Host = value
	}

	if err := cfg.resolveSecretFiles(); err != nil {
		return Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) resolveSecretFiles() error {
	if c.MySQL.DSN != "" && c.MySQL.DSNFile != "" {
		return errors.New("set only one of mysql.dsn and mysql.dsn_file")
	}
	if c.MySQL.DSNFile != "" {
		value, err := readSecretFile(c.MySQL.DSNFile)
		if err != nil {
			return fmt.Errorf("read mysql.dsn_file: %w", err)
		}
		c.MySQL.DSN = value
	}
	if c.InboundAPIKey != "" && c.InboundAPIKeyFile != "" {
		return errors.New("set only one of inbound_api_key and inbound_api_key_file")
	}
	if c.InboundAPIKeyFile != "" {
		value, err := readSecretFile(c.InboundAPIKeyFile)
		if err != nil {
			return fmt.Errorf("read inbound_api_key_file: %w", err)
		}
		c.InboundAPIKey = value
	}
	for environment, override := range c.Backends.Docker.Overrides {
		if override.APIKey != "" && override.APIKeyFile != "" {
			return fmt.Errorf("Docker backend override %q must set only one of api_key and api_key_file", environment)
		}
		if override.APIKeyFile != "" {
			value, err := readSecretFile(override.APIKeyFile)
			if err != nil {
				return fmt.Errorf("read Docker backend override %q api_key_file: %w", environment, err)
			}
			override.APIKey = value
			c.Backends.Docker.Overrides[environment] = override
		}
	}
	for environment, backend := range c.Backends.Static {
		if backend.APIKey != "" && backend.APIKeyFile != "" {
			return fmt.Errorf("backend %q must set only one of api_key and api_key_file", environment)
		}
		if backend.APIKeyFile != "" {
			value, err := readSecretFile(backend.APIKeyFile)
			if err != nil {
				return fmt.Errorf("read backend %q api_key_file: %w", environment, err)
			}
			backend.APIKey = value
			c.Backends.Static[environment] = backend
		}
	}
	return nil
}

func readSecretFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("secret path is not a regular file")
	}
	if info.Size() > 64*1024 {
		return "", errors.New("secret file exceeds 64 KiB")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	value := strings.TrimSuffix(strings.TrimSuffix(string(contents), "\n"), "\r")
	if value == "" {
		return "", errors.New("secret file is empty")
	}
	return value, nil
}

func (c *Config) Validate() error {
	if strings.TrimSpace(c.ListenAddress) == "" {
		return errors.New("listen_address is required")
	}
	if strings.TrimSpace(c.MySQL.DSN) == "" {
		return errors.New("mysql.dsn is required")
	}
	if c.InboundAPIKey == "" {
		return errors.New("inbound_api_key is required")
	}
	if c.BackendTimeout.Duration <= 0 {
		return errors.New("backend_timeout must be positive")
	}
	if c.ShutdownTimeout.Duration <= 0 {
		return errors.New("shutdown_timeout must be positive")
	}
	if c.MaxCallbackBodyBytes <= 0 {
		return errors.New("max_callback_body_bytes must be positive")
	}
	normalizedProxies := make([]string, 0, len(c.TrustedProxies))
	seenProxies := make(map[string]struct{}, len(c.TrustedProxies))
	for _, value := range c.TrustedProxies {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			return fmt.Errorf("trusted_proxies entry %q must be an IP CIDR", value)
		}
		normalized := prefix.Masked().String()
		if _, exists := seenProxies[normalized]; exists {
			continue
		}
		seenProxies[normalized] = struct{}{}
		normalizedProxies = append(normalizedProxies, normalized)
	}
	c.TrustedProxies = normalizedProxies
	if err := c.MySQL.validate(); err != nil {
		return err
	}
	if c.Backends.Docker.Enabled {
		if err := c.Backends.Docker.validate(); err != nil {
			return err
		}
	}
	if err := c.normalizeDockerOverrides(); err != nil {
		return err
	}

	normalized := make(map[string]BackendConfig, len(c.Backends.Static))
	for environment, backend := range c.Backends.Static {
		environment = strings.ToLower(strings.TrimSpace(environment))
		if !ValidEnvironment(environment) {
			return fmt.Errorf("invalid backend environment %q", environment)
		}
		if backend.CallbackURL == "" {
			return fmt.Errorf("static backend %q requires callback_url", environment)
		}
		parsed, err := url.ParseRequestURI(backend.CallbackURL)
		if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return fmt.Errorf("backend %q callback_url must be an absolute HTTP(S) URL", environment)
		}
		if _, exists := normalized[environment]; exists {
			return fmt.Errorf("duplicate backend environment %q after normalization", environment)
		}
		normalized[environment] = backend
	}
	c.Backends.Static = normalized
	return nil
}

func (c *Config) normalizeDockerOverrides() error {
	normalized := make(map[string]DockerOverrideConfig, len(c.Backends.Docker.Overrides))
	for environment, override := range c.Backends.Docker.Overrides {
		environment = strings.ToLower(strings.TrimSpace(environment))
		if !ValidEnvironment(environment) {
			return fmt.Errorf("invalid Docker backend override environment %q", environment)
		}
		if override.APIKey != "" && override.APIKeyFile != "" {
			return fmt.Errorf("Docker backend override %q must set only one of api_key and api_key_file", environment)
		}
		if override.Scheme == "" && override.Host == "" && override.Port == 0 && override.Path == "" && override.APIKey == "" && override.APIKeyFile == "" {
			return fmt.Errorf("Docker backend override %q must set at least one field", environment)
		}
		if override.Scheme != "" && override.Scheme != "http" && override.Scheme != "https" {
			return fmt.Errorf("Docker backend override %q scheme must be http or https", environment)
		}
		if override.Port < 0 || override.Port > 65535 {
			return fmt.Errorf("Docker backend override %q port must be between 1 and 65535", environment)
		}
		if override.Path != "" {
			path := strings.ReplaceAll(override.Path, "{environment}", "environment")
			parsed, err := url.ParseRequestURI(path)
			if err != nil || !strings.HasPrefix(path, "/") || parsed.IsAbs() {
				return fmt.Errorf("Docker backend override %q path must be an absolute request path", environment)
			}
		}
		if strings.ContainsAny(override.Host, "/?#") {
			return fmt.Errorf("Docker backend override %q host is invalid", environment)
		}
		if _, exists := normalized[environment]; exists {
			return fmt.Errorf("duplicate Docker backend override environment %q after normalization", environment)
		}
		normalized[environment] = override
	}
	c.Backends.Docker.Overrides = normalized
	return nil
}

func (c MySQLConfig) validate() error {
	for name, value := range map[string]string{
		"users_table":          c.UsersTable,
		"pending_logins_table": c.PendingLoginsTable,
	} {
		if !mysqlIdentifierPattern.MatchString(value) {
			return fmt.Errorf("mysql.%s must contain only letters, digits, and underscores and be at most 64 characters", name)
		}
	}
	if c.StartupTimeout.Duration <= 0 {
		return errors.New("mysql.startup_timeout must be positive")
	}
	if c.AdvisoryLockTimeoutSeconds <= 0 {
		return errors.New("mysql.advisory_lock_timeout_seconds must be positive")
	}
	return nil
}

func (c DockerConfig) validate() error {
	if !strings.HasPrefix(c.Host, "unix://") && !strings.HasPrefix(c.Host, "http://") && !strings.HasPrefix(c.Host, "https://") {
		return errors.New("docker.host must use unix, http, or https")
	}
	if c.RefreshInterval.Duration <= 0 {
		return errors.New("docker.refresh_interval must be positive")
	}
	if strings.TrimSpace(c.LabelPrefix) == "" {
		return errors.New("docker.label_prefix is required")
	}
	if c.DefaultScheme != "http" && c.DefaultScheme != "https" {
		return errors.New("docker.default_scheme must be http or https")
	}
	if c.DefaultPort < 1 || c.DefaultPort > 65535 {
		return errors.New("docker.default_port must be between 1 and 65535")
	}
	if !strings.HasPrefix(c.DefaultPath, "/") {
		return errors.New("docker.default_path must begin with /")
	}
	return nil
}

func ValidEnvironment(value string) bool {
	return environmentPattern.MatchString(value)
}
