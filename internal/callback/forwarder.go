package callback

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

type BackendTarget struct {
	CallbackURL string
	APIKey      string
}

type BackendResolver interface {
	Resolve(environment string) (BackendTarget, bool)
}

type HTTPForwarder struct {
	resolver BackendResolver
	client   *http.Client
	timeout  time.Duration
	logger   *slog.Logger
}

func NewHTTPForwarder(resolver BackendResolver, timeout time.Duration, logger *slog.Logger) *HTTPForwarder {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 10
	transport.IdleConnTimeout = 90 * time.Second
	return &HTTPForwarder{
		resolver: resolver,
		client: &http.Client{
			Transport: transport,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		timeout: timeout,
		logger:  logger,
	}
}

func (f *HTTPForwarder) TryForward(ctx context.Context, authCallback AuthCallback, requestID, inboundAPIKey string) (bool, error) {
	backend, ok := f.resolver.Resolve(authCallback.Environment)
	if !ok {
		return false, nil
	}
	body, err := authCallback.JSON()
	if err != nil {
		return false, fmt.Errorf("encode backend callback: %w", err)
	}

	requestContext, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestContext, http.MethodPost, backend.CallbackURL, bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("create backend callback: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	apiKey := backend.APIKey
	if apiKey == "" {
		apiKey = inboundAPIKey
	}
	request.Header.Set("X-Api-Key", apiKey)
	request.Header.Set("User-Agent", "gatehouse/1")
	if requestID != "" {
		request.Header.Set("X-Request-ID", requestID)
	}

	response, err := f.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		var netError net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netError) && netError.Timeout()) {
			f.logger.Warn("backend callback timed out", "environment", authCallback.Environment, "timeout", f.timeout)
			return false, nil
		}
		f.logger.Warn("backend callback failed", "environment", authCallback.Environment, "error", err)
		return false, nil
	}
	defer response.Body.Close()

	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return true, nil
	}
	f.logger.Warn("backend rejected callback", "environment", authCallback.Environment, "status", response.StatusCode)
	return false, nil
}
