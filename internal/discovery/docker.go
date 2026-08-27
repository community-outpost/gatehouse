package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/community-outpost/gatehouse/internal/callback"
	"github.com/community-outpost/gatehouse/internal/config"
)

type Resolver struct {
	cfg          config.DockerConfig
	static       map[string]callback.BackendTarget
	keyOverrides map[string]string
	client       *http.Client
	baseURL      string
	logger       *slog.Logger
	discovered   atomic.Value
	roundRobin   sync.Mutex
	next         map[string]uint64
}

type backendTable map[string][]callback.BackendTarget

type dockerContainer struct {
	ID              string            `json:"Id"`
	Labels          map[string]string `json:"Labels"`
	NetworkSettings struct {
		Networks map[string]struct {
			IPAddress         string `json:"IPAddress"`
			GlobalIPv6Address string `json:"GlobalIPv6Address"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

func New(cfg config.DockerConfig, staticBackends map[string]config.BackendConfig, logger *slog.Logger) (*Resolver, error) {
	resolver := &Resolver{
		cfg:          cfg,
		static:       make(map[string]callback.BackendTarget),
		keyOverrides: make(map[string]string),
		logger:       logger,
		next:         make(map[string]uint64),
	}
	resolver.discovered.Store(backendTable{})
	for environment, override := range cfg.Overrides {
		resolver.keyOverrides[environment] = override.APIKey
	}
	for environment, backend := range staticBackends {
		if backend.CallbackURL != "" {
			resolver.static[environment] = callback.BackendTarget{
				CallbackURL: backend.CallbackURL,
				APIKey:      backend.APIKey,
			}
		}
	}
	if !cfg.Enabled {
		return resolver, nil
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	switch {
	case strings.HasPrefix(cfg.Host, "unix://"):
		socketPath := strings.TrimPrefix(cfg.Host, "unix://")
		if socketPath == "" {
			return nil, errors.New("docker socket path is empty")
		}
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		}
		resolver.baseURL = "http://docker"
	default:
		resolver.baseURL = strings.TrimRight(cfg.Host, "/")
	}
	resolver.client = &http.Client{Transport: transport, Timeout: 3 * time.Second}
	return resolver, nil
}

func (r *Resolver) Start(ctx context.Context) {
	if !r.cfg.Enabled {
		return
	}
	if err := r.Refresh(ctx); err != nil {
		r.logger.Warn("initial Docker backend discovery failed", "error", err)
	}
	go func() {
		ticker := time.NewTicker(r.cfg.RefreshInterval.Duration)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := r.Refresh(ctx); err != nil {
					r.logger.Warn("Docker backend discovery failed; retaining previous backends", "error", err)
				}
			}
		}
	}()
}

func (r *Resolver) Resolve(environment string) (callback.BackendTarget, bool) {
	if target, ok := r.static[environment]; ok {
		return target, true
	}
	backends := r.discovered.Load().(backendTable)[environment]
	if len(backends) == 0 {
		return callback.BackendTarget{}, false
	}
	r.roundRobin.Lock()
	index := r.next[environment] % uint64(len(backends))
	r.next[environment]++
	r.roundRobin.Unlock()
	target := backends[index]
	target.APIKey = r.keyOverrides[environment]
	return target, true
}

func (r *Resolver) Refresh(ctx context.Context) error {
	filters, err := json.Marshal(map[string][]string{
		"label":  {r.cfg.LabelPrefix + ".environment"},
		"status": {"running"},
	})
	if err != nil {
		return err
	}
	endpoint := r.baseURL + "/containers/json?filters=" + url.QueryEscape(string(filters))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create Docker request: %w", err)
	}
	response, err := r.client.Do(request)
	if err != nil {
		return fmt.Errorf("list Docker containers: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("Docker returned %s", response.Status)
	}
	var containers []dockerContainer
	decoder := json.NewDecoder(io.LimitReader(response.Body, 4*1024*1024))
	if err := decoder.Decode(&containers); err != nil {
		return fmt.Errorf("decode Docker response: %w", err)
	}
	sort.Slice(containers, func(i, j int) bool { return containers[i].ID < containers[j].ID })
	backends := make(backendTable)
	for _, container := range containers {
		target, environment, err := r.backendFor(container)
		if err != nil {
			r.logger.Warn("ignoring invalid Docker backend label", "container_id", shortID(container.ID), "error", err)
			continue
		}
		if environment == "" {
			continue
		}
		backends[environment] = append(backends[environment], target)
	}
	r.discovered.Store(backends)
	r.logger.Debug("Docker backends refreshed", "environments", len(backends), "containers", len(containers))
	return nil
}

func (r *Resolver) backendFor(container dockerContainer) (callback.BackendTarget, string, error) {
	prefix := r.cfg.LabelPrefix + "."
	environment, present := container.Labels[prefix+"environment"]
	if !present {
		return callback.BackendTarget{}, "", nil
	}
	environment = strings.ToLower(strings.TrimSpace(environment))
	if !config.ValidEnvironment(environment) {
		return callback.BackendTarget{}, "", fmt.Errorf("invalid environment %q", environment)
	}

	override := r.cfg.Overrides[environment]
	scheme := override.Scheme
	if scheme == "" {
		scheme = labelOrDefault(container.Labels, prefix+"scheme", r.cfg.DefaultScheme)
	}
	if scheme != "http" && scheme != "https" {
		return callback.BackendTarget{}, "", fmt.Errorf("environment %q has invalid scheme %q", environment, scheme)
	}
	port := r.cfg.DefaultPort
	if override.Port != 0 {
		port = override.Port
	} else if value := strings.TrimSpace(container.Labels[prefix+"port"]); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 65535 {
			return callback.BackendTarget{}, "", fmt.Errorf("environment %q has invalid port %q", environment, value)
		}
		port = parsed
	}
	path := override.Path
	if path == "" {
		path = labelOrDefault(container.Labels, prefix+"path", r.cfg.DefaultPath)
	}
	path = strings.ReplaceAll(path, "{environment}", environment)
	parsedPath, err := url.ParseRequestURI(path)
	if err != nil || !strings.HasPrefix(path, "/") || parsedPath.IsAbs() {
		return callback.BackendTarget{}, "", fmt.Errorf("environment %q has invalid path %q", environment, path)
	}
	host := strings.TrimSpace(override.Host)
	if host == "" {
		host = r.containerAddress(container)
	}
	if host == "" {
		return callback.BackendTarget{}, "", fmt.Errorf("environment %q has no address on a usable Docker network", environment)
	}
	if strings.ContainsAny(host, "/?#") {
		return callback.BackendTarget{}, "", fmt.Errorf("environment %q has invalid host %q", environment, host)
	}
	targetURL := url.URL{Scheme: scheme, Host: net.JoinHostPort(host, strconv.Itoa(port)), Path: parsedPath.Path, RawQuery: parsedPath.RawQuery}
	return callback.BackendTarget{CallbackURL: targetURL.String()}, environment, nil
}

func (r *Resolver) containerAddress(container dockerContainer) string {
	if r.cfg.Network != "" {
		return networkAddress(container, r.cfg.Network)
	}
	names := make([]string, 0, len(container.NetworkSettings.Networks))
	for name := range container.NetworkSettings.Networks {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if address := networkAddress(container, name); address != "" {
			return address
		}
	}
	return ""
}

func networkAddress(container dockerContainer, network string) string {
	settings, ok := container.NetworkSettings.Networks[network]
	if !ok {
		return ""
	}
	if settings.IPAddress != "" {
		return settings.IPAddress
	}
	return settings.GlobalIPv6Address
}

func labelOrDefault(labels map[string]string, name, fallback string) string {
	if value := strings.TrimSpace(labels[name]); value != "" {
		return value
	}
	return fallback
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}
