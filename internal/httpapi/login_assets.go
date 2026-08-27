package httpapi

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
)

const maxLoginAssetBytes = 2 << 20

//go:embed assets/*
var loginAssets embed.FS

var builtInProviderIcons = map[string]string{
	"discord":        "assets/provider-discord.svg",
	"gamereplays":    "assets/provider-gamereplays.png",
	"generalsonline": "assets/provider-generalsonline.png",
	"steam":          "assets/provider-steam.svg",
}

type LoginAsset struct {
	Data        []byte
	ContentType string
}

type LoginBranding struct {
	ServiceName     string
	OperatorName    string
	ApplicationName string
	AccentColor     string
	BackgroundColor string
	Logo            *LoginAsset
	Favicon         *LoginAsset
}

func normalizeLoginBranding(branding LoginBranding) LoginBranding {
	if branding.ServiceName == "" {
		branding.ServiceName = "GateHouse"
	}

	if branding.ApplicationName == "" {
		branding.ApplicationName = "Your Game"
	}
	if branding.AccentColor == "" {
		branding.AccentColor = "#00e7f0"
	}
	if branding.BackgroundColor == "" {
		branding.BackgroundColor = "#060a0b"
	}
	return branding
}

func LoadLoginAsset(path string) (*LoginAsset, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("asset path is not a regular file")
	}
	if info.Size() > maxLoginAssetBytes {
		return nil, fmt.Errorf("asset exceeds %d bytes", maxLoginAssetBytes)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- branding paths come only from trusted process configuration.
	if err != nil {
		return nil, err
	}
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	contentType = strings.Split(contentType, ";")[0]
	if !strings.HasPrefix(contentType, "image/") {
		return nil, fmt.Errorf("asset content type %q is not an image", contentType)
	}
	return &LoginAsset{Data: data, ContentType: contentType}, nil
}

func (s *Server) serveBrandLogo(writer http.ResponseWriter, _ *http.Request) {
	asset := s.loginOptions.Branding.Logo
	if asset == nil {
		asset, _ = embeddedLoginAsset("assets/gatehouse-logo.png")
	}
	writeLoginAsset(writer, asset)
}

func (s *Server) serveBrandFavicon(writer http.ResponseWriter, _ *http.Request) {
	asset := s.loginOptions.Branding.Favicon
	if asset == nil {
		asset, _ = embeddedLoginAsset("assets/gatehouse-favicon.png")
	}
	writeLoginAsset(writer, asset)
}

func (s *Server) serveProviderIcon(writer http.ResponseWriter, request *http.Request) {
	provider, exists := s.loginOptions.Providers[chi.URLParam(request, "provider")]
	if !exists {
		http.NotFound(writer, request)
		return
	}
	asset := provider.IconAsset
	if asset == nil {
		path, exists := builtInProviderIcons[provider.Icon]
		if !exists {
			http.NotFound(writer, request)
			return
		}
		asset, _ = embeddedLoginAsset(path)
	}
	writeLoginAsset(writer, asset)
}

func serveDisplayNameScript(writer http.ResponseWriter, request *http.Request) {
	data, err := loginAssets.ReadFile("assets/display-name.js")
	if err != nil {
		http.NotFound(writer, request)
		return
	}
	writer.Header().Set("Cache-Control", "public, max-age=86400")
	writer.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = writer.Write(data)
}

func embeddedLoginAsset(path string) (*LoginAsset, error) {
	data, err := loginAssets.ReadFile(path)
	if err != nil {
		return nil, err
	}
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	return &LoginAsset{Data: data, ContentType: strings.Split(contentType, ";")[0]}, nil
}

func writeLoginAsset(writer http.ResponseWriter, asset *LoginAsset) {
	if asset == nil {
		http.Error(writer, "asset unavailable", http.StatusNotFound)
		return
	}
	writer.Header().Set("Cache-Control", "public, max-age=86400")
	writer.Header().Set("Content-Type", asset.ContentType)
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = bytes.NewReader(asset.Data).WriteTo(writer)
}

func providerHasIcon(provider LoginProvider) bool {
	if provider.IconAsset != nil {
		return true
	}
	_, exists := builtInProviderIcons[provider.Icon]
	return exists
}
