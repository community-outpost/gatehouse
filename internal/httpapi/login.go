package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/community-outpost/gatehouse/internal/callback"
	"github.com/community-outpost/gatehouse/internal/config"
	"github.com/community-outpost/gatehouse/internal/externallogin"
	"github.com/community-outpost/gatehouse/internal/identity"
)

const loginLifetime = 5 * time.Minute

type PrincipalResolver interface {
	FindUser(context.Context, string, string) (int64, bool, error)
	DisplayNameAvailable(context.Context, string) (bool, error)
	ResolveOrCreateUser(context.Context, string, string, string) (int64, error)
}

type LoginProvider struct {
	Label         string
	Description   string
	Icon          string
	IconAsset     *LoginAsset
	Authenticator externallogin.Provider
	RedirectURL   string
}

type LoginOptions struct {
	GORedirectIssuer string
	Branding         LoginBranding
	Providers        map[string]LoginProvider
	StateSecret      string
}

type loginState struct {
	Nonce       string `json:"nonce"`
	Provider    string `json:"provider"`
	Environment string `json:"environment"`
	GameCode    string `json:"game_code"`
	ExpiresAt   int64  `json:"expires_at"`
}

type enrollmentState struct {
	Nonce       string `json:"nonce"`
	Provider    string `json:"provider"`
	Subject     string `json:"subject"`
	Environment string `json:"environment"`
	GameCode    string `json:"game_code"`
	ExpiresAt   int64  `json:"expires_at"`
}

func (s *Server) ConfigureLogin(resolver PrincipalResolver, options LoginOptions) error {
	if resolver == nil {
		return errors.New("principal resolver is required")
	}
	if options.GORedirectIssuer != "" && !config.ValidEnvironment(options.GORedirectIssuer) {
		return errors.New("go_redirect issuer is invalid")
	}
	if options.StateSecret == "" {
		return errors.New("login state secret is required")
	}
	options.Branding = normalizeLoginBranding(options.Branding)
	if options.Providers == nil {
		options.Providers = map[string]LoginProvider{}
	}
	for name, provider := range options.Providers {
		if !config.ValidEnvironment(name) || provider.Label == "" ||
			(provider.Authenticator == nil) == (provider.RedirectURL == "") {
			return fmt.Errorf("invalid login provider %q", name)
		}
		if provider.Icon == "" {
			provider.Icon = name
		}
		options.Providers[name] = provider
	}
	if options.GORedirectIssuer != "" {
		provider, ok := options.Providers[options.GORedirectIssuer]
		if !ok || provider.RedirectURL == "" {
			return errors.New("go_redirect issuer must name a configured redirect provider")
		}
	}
	s.principalResolver = resolver
	s.loginOptions = options
	s.stateKey = sha256.Sum256([]byte("gatehouse browser login state\x00" + options.StateSecret))
	return nil
}

func (s *Server) loginChooser(writer http.ResponseWriter, request *http.Request) {
	gameCode, environment, ok := s.loginParameters(writer, request)
	if !ok {
		return
	}
	choices := make([]providerChoice, 0, len(s.loginOptions.Providers))
	for name, provider := range s.loginOptions.Providers {
		description := provider.Description
		if description == "" {
			description = "Continue with " + provider.Label
		}
		choices = append(choices, providerChoice{
			Path:        name,
			Label:       provider.Label,
			Description: description,
			Monogram:    providerMonogram(provider.Label),
			HasIcon:     providerHasIcon(provider),
		})
	}
	sort.Slice(choices, func(i, j int) bool { return choices[i].Label < choices[j].Label })
	if len(choices) == 0 {
		writeProblem(writer, http.StatusNotFound, "No login providers are configured", "")
		return
	}
	setLoginPageHeaders(writer)
	_ = loginPage.Execute(writer, map[string]any{
		"Brand": s.loginOptions.Branding, "Providers": choices, "GameCode": gameCode, "Environment": environment,
	})
}

func (s *Server) beginLogin(writer http.ResponseWriter, request *http.Request) {
	providerName := chi.URLParam(request, "provider")
	gameCode, environment, ok := s.loginParameters(writer, request)
	if !ok {
		return
	}
	provider, exists := s.loginOptions.Providers[providerName]
	if !exists {
		writeProblem(writer, http.StatusNotFound, "Unknown login provider", "")
		return
	}
	if provider.RedirectURL != "" {
		target, _ := url.Parse(provider.RedirectURL)
		query := target.Query()
		query.Set("gamecode", gameCode)
		query.Set("env", environment)
		target.RawQuery = query.Encode()
		http.Redirect(writer, request, target.String(), http.StatusFound)
		return
	}

	state := loginState{
		Nonce:       randomToken(),
		Provider:    providerName,
		Environment: environment,
		GameCode:    gameCode,
		ExpiresAt:   time.Now().Add(loginLifetime).Unix(),
	}
	if state.Nonce == "" {
		writeProblem(writer, http.StatusInternalServerError, "Could not start login", "")
		return
	}
	cookieValue, err := s.signState(state)
	if err != nil {
		s.logger.Error("sign login state", "error", err)
		writeProblem(writer, http.StatusInternalServerError, "Could not start login", "")
		return
	}
	s.setLoginCookie(writer, loginCookieName(providerName), providerName, cookieValue)
	authorizationURL, err := provider.Authenticator.AuthorizationURL(state.Nonce)
	if err != nil {
		s.logger.Error("build provider authorization URL", "provider", providerName, "error", err)
		writeProblem(writer, http.StatusBadGateway, "Could not start login", "")
		return
	}
	http.Redirect(writer, request, authorizationURL, http.StatusFound)
}

func (s *Server) finishLogin(writer http.ResponseWriter, request *http.Request) {
	providerName := chi.URLParam(request, "provider")
	provider, exists := s.loginOptions.Providers[providerName]
	if !exists || provider.Authenticator == nil {
		writeProblem(writer, http.StatusNotFound, "Unknown login provider", "")
		return
	}
	state, err := s.readLoginState(request, providerName)
	s.clearCookie(writer, loginCookieName(providerName), providerName)
	if err != nil || subtle.ConstantTimeCompare([]byte(state.Nonce), []byte(request.URL.Query().Get("state"))) != 1 {
		writeProblem(writer, http.StatusBadRequest, "Invalid or expired login state", "")
		return
	}
	identity, err := provider.Authenticator.Authenticate(request.Context(), request.URL.Query())
	if err != nil {
		s.logger.Warn("provider authentication failed", "provider", providerName, "error", err)
		s.writeLoginResult(writer, http.StatusBadGateway, "Login failed", "Authentication could not be completed. You can return to the game and try again.")
		return
	}
	if userID, found, err := s.principalResolver.FindUser(request.Context(), providerName, identity.Subject); err != nil {
		s.loginUnavailable(writer, providerName, err)
		return
	} else if found {
		s.deliverBrowserLogin(writer, request, state.Environment, state.GameCode, providerName, userID)
		return
	}

	s.startEnrollment(writer, providerName, identity.Subject, state)
}

func (s *Server) completeEnrollment(writer http.ResponseWriter, request *http.Request) {
	providerName := chi.URLParam(request, "provider")
	provider, exists := s.loginOptions.Providers[providerName]
	if !exists || provider.Authenticator == nil {
		writeProblem(writer, http.StatusNotFound, "Unknown login provider", "")
		return
	}
	state, err := s.readEnrollmentState(request, providerName)
	if err != nil {
		s.clearCookie(writer, enrollmentCookieName(providerName), providerName)
		writeProblem(writer, http.StatusBadRequest, "Invalid or expired signup", "Please sign in again.")
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 16*1024)
	if err := request.ParseForm(); err != nil || subtle.ConstantTimeCompare([]byte(state.Nonce), []byte(request.Form.Get("csrf"))) != 1 {
		writeProblem(writer, http.StatusBadRequest, "Invalid signup request", "")
		return
	}
	displayName, err := normalizeDisplayName(request.Form.Get("display_name"))
	if err != nil {
		s.writeDisplayNameForm(writer, state.Nonce, formDisplayName(request.Form.Get("display_name")), "Use 3–16 ASCII letters, numbers, spaces, brackets, or the supported symbols shown below.")
		return
	}
	available, err := s.principalResolver.DisplayNameAvailable(request.Context(), displayName)
	if err != nil {
		s.loginUnavailable(writer, providerName, err)
		return
	}
	if !available {
		s.writeDisplayNameForm(writer, state.Nonce, displayName, "That display name is already in use.")
		return
	}
	userID, err := s.principalResolver.ResolveOrCreateUser(request.Context(), providerName, state.Subject, displayName)
	if errors.Is(err, identity.ErrDisplayNameUnavailable) {
		s.writeDisplayNameForm(writer, state.Nonce, displayName, "That display name is already in use.")
		return
	}
	if err != nil {
		s.loginUnavailable(writer, providerName, err)
		return
	}
	s.clearCookie(writer, enrollmentCookieName(providerName), providerName)
	s.deliverBrowserLogin(writer, request, state.Environment, state.GameCode, providerName, userID)
}

func (s *Server) startEnrollment(writer http.ResponseWriter, providerName, subject string, login loginState) {
	state := enrollmentState{
		Nonce:       randomToken(),
		Provider:    providerName,
		Subject:     subject,
		Environment: login.Environment,
		GameCode:    login.GameCode,
		ExpiresAt:   time.Now().Add(loginLifetime).Unix(),
	}
	if state.Nonce == "" {
		writeProblem(writer, http.StatusInternalServerError, "Could not continue login", "")
		return
	}
	value, err := s.signState(state)
	if err != nil {
		writeProblem(writer, http.StatusInternalServerError, "Could not continue login", "")
		return
	}
	s.setLoginCookie(writer, enrollmentCookieName(providerName), providerName, value)
	s.writeDisplayNameForm(writer, state.Nonce, "", "")
}

func (s *Server) deliverBrowserLogin(writer http.ResponseWriter, request *http.Request, environment, gameCode, providerName string, userID int64) {
	authCallback := callback.AuthCallback{
		Environment: environment,
		Code:        gameCode,
		UserID:      userID,
		Success:     true,
		Payload: map[string]any{
			"env":     environment,
			"code":    gameCode,
			"user_id": userID,
			"success": true,
		},
	}
	if _, err := s.dispatcher.Dispatch(request.Context(), authCallback, middleware.GetReqID(request.Context())); err != nil {
		s.logger.Error("deliver browser login", "provider", providerName, "user_id", userID, "error", err)
		s.writeLoginResult(writer, http.StatusServiceUnavailable, "Login unavailable", "The login could not be delivered. Please try again.")
		return
	}
	s.writeLoginResult(writer, http.StatusOK, "Login complete", "You can close this window and return to the game.")
}

func (s *Server) loginUnavailable(writer http.ResponseWriter, provider string, err error) {
	s.logger.Error("resolve authenticated principal", "provider", provider, "error", err)
	s.writeLoginResult(writer, http.StatusServiceUnavailable, "Login unavailable", "Your account could not be prepared. Please try again.")
}

func (s *Server) loginParameters(writer http.ResponseWriter, request *http.Request) (string, string, bool) {
	gameCode := strings.ToUpper(strings.TrimSpace(request.URL.Query().Get("gamecode")))
	if !callback.ValidCode(gameCode) {
		writeProblem(writer, http.StatusBadRequest, "Invalid game code", "")
		return "", "", false
	}
	environment := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("env")))
	if !config.ValidEnvironment(environment) {
		writeProblem(writer, http.StatusBadRequest, "Invalid environment", "")
		return "", "", false
	}
	return gameCode, environment, true
}

func (s *Server) signState(state any) (string, error) {
	payload, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.stateKey[:])
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *Server) readLoginState(request *http.Request, provider string) (loginState, error) {
	var state loginState
	if err := s.readSignedCookie(request, loginCookieName(provider), &state); err != nil {
		return state, err
	}
	if state.Provider != provider || state.ExpiresAt < time.Now().Unix() ||
		!callback.ValidCode(state.GameCode) || !config.ValidEnvironment(state.Environment) {
		return loginState{}, errors.New("invalid or expired state")
	}
	return state, nil
}

func (s *Server) readEnrollmentState(request *http.Request, provider string) (enrollmentState, error) {
	var state enrollmentState
	if err := s.readSignedCookie(request, enrollmentCookieName(provider), &state); err != nil {
		return state, err
	}
	if state.Provider != provider || state.Subject == "" || state.ExpiresAt < time.Now().Unix() ||
		!callback.ValidCode(state.GameCode) || !config.ValidEnvironment(state.Environment) {
		return enrollmentState{}, errors.New("invalid or expired enrollment state")
	}
	return state, nil
}

func (s *Server) readSignedCookie(request *http.Request, name string, target any) error {
	cookie, err := request.Cookie(name)
	if err != nil {
		return err
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return errors.New("invalid signed cookie")
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return errors.New("invalid cookie signature")
	}
	mac := hmac.New(sha256.New, s.stateKey[:])
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return errors.New("invalid cookie signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(payload, target) != nil {
		return errors.New("invalid cookie payload")
	}
	return nil
}

func (s *Server) setLoginCookie(writer http.ResponseWriter, name, provider, value string) {
	http.SetCookie(writer, &http.Cookie{
		Name: name, Value: value, Path: "/login/" + provider + "/return",
		MaxAge: int(loginLifetime.Seconds()), Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearCookie(writer http.ResponseWriter, name, provider string) {
	http.SetCookie(writer, &http.Cookie{
		Name: name, Path: "/login/" + provider + "/return",
		MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) writeDisplayNameForm(writer http.ResponseWriter, csrf, displayName, message string) {
	setLoginPageHeaders(writer)
	_ = displayNamePage.Execute(writer, map[string]any{
		"Brand": s.loginOptions.Branding, "CSRF": csrf, "DisplayName": displayName, "Error": message,
		"MinLength": identity.MinDisplayNameLength, "MaxLength": identity.MaxDisplayNameLength,
	})
}

func normalizeDisplayName(value string) (string, error) {
	return identity.NormalizeDisplayName(value)
}

func formDisplayName(value string) string {
	value = strings.TrimSpace(value)
	if _, err := normalizeDisplayName(value); err != nil {
		return ""
	}
	return value
}

func randomToken() string {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(value)
}

func loginCookieName(provider string) string      { return "gatehouse_login_" + provider }
func enrollmentCookieName(provider string) string { return "gatehouse_enroll_" + provider }

func (s *Server) writeLoginResult(writer http.ResponseWriter, status int, title, message string) {
	setLoginPageHeaders(writer)
	writer.WriteHeader(status)
	_ = loginResultPage.Execute(writer, map[string]any{
		"Brand": s.loginOptions.Branding, "Title": title, "Message": message, "Success": status < http.StatusBadRequest,
	})
}

func localUserSubject(userID int64) string {
	return strconv.FormatInt(userID, 10)
}
