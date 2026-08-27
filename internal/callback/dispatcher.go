package callback

import (
	"context"
	"log/slog"
)

type Store interface {
	EnsureUser(context.Context, int64) error
	SavePendingLogin(context.Context, AuthCallback) error
}

type Forwarder interface {
	TryForward(context.Context, AuthCallback, string) (bool, error)
}

type Service struct {
	store     Store
	forwarder Forwarder
	logger    *slog.Logger
}

func NewService(store Store, forwarder Forwarder, logger *slog.Logger) *Service {
	return &Service{store: store, forwarder: forwarder, logger: logger}
}

func (s *Service) Dispatch(ctx context.Context, authCallback AuthCallback, requestID string) (Delivery, error) {
	if authCallback.Success {
		if err := s.store.EnsureUser(ctx, authCallback.UserID); err != nil {
			return "", err
		}
	}

	accepted, err := s.forwarder.TryForward(ctx, authCallback, requestID)
	if err != nil {
		return "", err
	}
	if accepted {
		s.logger.Info("authentication callback accepted by backend",
			"environment", authCallback.Environment,
			"user_id", authCallback.UserID)
		return DeliveryBackend, nil
	}

	if err := s.store.SavePendingLogin(ctx, authCallback); err != nil {
		return "", err
	}
	s.logger.Warn("authentication callback used pending-logins MySQL fallback",
		"environment", authCallback.Environment,
		"user_id", authCallback.UserID)
	return DeliveryMySQLFallback, nil
}
