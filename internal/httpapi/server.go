package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httplog/v3"

	"github.com/community-outpost/gatehouse/internal/callback"
)

type Readiness interface {
	Ping(context.Context) error
}

type Dispatcher interface {
	Dispatch(context.Context, callback.AuthCallback, string, string) (callback.Delivery, error)
}

type Server struct {
	dispatcher         Dispatcher
	readiness          Readiness
	keyHash            [sha256.Size]byte
	maxBodyBytes       int64
	clientIPMiddleware func(http.Handler) http.Handler
	logger             *slog.Logger
}

func New(dispatcher Dispatcher, readiness Readiness, apiKey string, maxBodyBytes int64, trustedProxies []string, logger *slog.Logger) (*Server, error) {
	clientIPMiddleware, err := trustedProxyClientIP(trustedProxies)
	if err != nil {
		return nil, err
	}
	return &Server{
		dispatcher:         dispatcher,
		readiness:          readiness,
		keyHash:            sha256.Sum256([]byte(apiKey)),
		maxBodyBytes:       maxBodyBytes,
		clientIPMiddleware: clientIPMiddleware,
		logger:             logger,
	}, nil
}

func (s *Server) Handler() http.Handler {
	router := chi.NewRouter()
	router.Use(s.clientIPMiddleware)
	router.Use(middleware.RequestID)
	router.Use(httplog.RequestLogger(s.logger, &httplog.Options{
		Level:         slog.LevelInfo,
		RecoverPanics: true,
		Skip: func(request *http.Request, _ int) bool {
			return request.URL.Path == "/healthz" || request.URL.Path == "/readyz"
		},
		LogRequestHeaders:  []string{"Content-Type"},
		LogResponseHeaders: []string{"Content-Type", "X-Gatehouse-Delivery"},
		LogRequestBody: func(request *http.Request) bool {
			return request.Method == http.MethodPost && isCallbackPath(request.URL.Path)
		},
		LogBodyMaxLen: int(s.maxBodyBytes),
	}))
	router.Get("/healthz", s.health)
	router.Get("/readyz", s.ready)
	router.Group(func(protected chi.Router) {
		protected.Use(s.authenticate)
		protected.Post("/LoginCode", s.receiveCallback)
		protected.Post("/env/{environment}/contract/{contractVersion}/LoginCode", s.receiveCallback)
	})
	return router
}

func (s *Server) health(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "healthy"})
}

func (s *Server) ready(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := context.WithTimeout(request.Context(), 2*time.Second)
	defer cancel()
	if err := s.readiness.Ping(ctx); err != nil {
		s.logger.Error("readiness check failed", "error", err)
		writeProblem(writer, http.StatusServiceUnavailable, "MySQL is unavailable", "")
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !s.validAPIKey(request.Header.Get("X-Api-Key")) {
			writeProblem(writer, http.StatusUnauthorized, "Unauthorized", "")
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func (s *Server) receiveCallback(writer http.ResponseWriter, request *http.Request) {
	contractVersion := chi.URLParam(request, "contractVersion")
	if contractVersion != "" && contractVersion != "1" {
		writeProblem(writer, http.StatusBadRequest, "Only contract version 1 is supported", "")
		return
	}

	request.Body = http.MaxBytesReader(writer, request.Body, s.maxBodyBytes)
	body, err := io.ReadAll(request.Body)
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			writeProblem(writer, http.StatusRequestEntityTooLarge, "Callback body is too large", "")
			return
		}
		writeProblem(writer, http.StatusBadRequest, "Could not read callback body", "")
		return
	}

	authCallback, err := callback.Parse(body, chi.URLParam(request, "environment"))
	if err != nil {
		_ = httplog.SetError(request.Context(), err)
		writeProblem(writer, http.StatusBadRequest, "Invalid callback", err.Error())
		return
	}
	httplog.SetAttrs(request.Context(),
		slog.String("callback.environment", authCallback.Environment),
		slog.Int64("callback.user_id", authCallback.UserID),
		slog.Bool("callback.success", authCallback.Success),
		slog.String("request.id", middleware.GetReqID(request.Context())),
	)
	delivery, err := s.dispatcher.Dispatch(request.Context(), authCallback, middleware.GetReqID(request.Context()), request.Header.Get("X-Api-Key"))
	if err != nil {
		_ = httplog.SetError(request.Context(), err)
		if errors.Is(err, context.Canceled) {
			return
		}
		s.logger.Error("callback delivery failed",
			"environment", authCallback.Environment,
			"user_id", authCallback.UserID,
			"error", err)
		writeProblem(writer, http.StatusServiceUnavailable, "Callback delivery is temporarily unavailable", "")
		return
	}

	writer.Header().Set("X-Gatehouse-Delivery", string(delivery))
	httplog.SetAttrs(request.Context(), slog.String("callback.delivery", string(delivery)))
	if delivery == callback.DeliveryBackend {
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	writer.WriteHeader(http.StatusAccepted)
}

func isCallbackPath(path string) bool {
	return path == "/LoginCode" ||
		(strings.HasPrefix(path, "/env/") && strings.HasSuffix(path, "/LoginCode"))
}

func (s *Server) validAPIKey(candidate string) bool {
	candidateHash := sha256.Sum256([]byte(candidate))
	return candidate != "" && subtle.ConstantTimeCompare(candidateHash[:], s.keyHash[:]) == 1
}

func writeProblem(writer http.ResponseWriter, status int, title, detail string) {
	response := map[string]any{"status": status, "title": title}
	if detail != "" {
		response["detail"] = detail
	}
	writer.Header().Set("Content-Type", "application/problem+json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(response)
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
