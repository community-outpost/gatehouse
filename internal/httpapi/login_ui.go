package httpapi

import (
	"net/http"
	"strings"
	"unicode"
)

func providerMonogram(label string) string {
	runes := []rune(strings.TrimSpace(label))
	initials := make([]rune, 0, 2)
	for index, character := range runes {
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) {
			continue
		}
		if index == 0 || unicode.IsUpper(character) {
			initials = append(initials, unicode.ToUpper(character))
			if len(initials) == 2 {
				return string(initials)
			}
		}
	}
	for _, character := range runes {
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) {
			continue
		}
		upper := unicode.ToUpper(character)
		if len(initials) == 0 || initials[len(initials)-1] != upper {
			initials = append(initials, upper)
		}
		if len(initials) == 2 {
			break
		}
	}
	if len(initials) == 0 {
		return "ID"
	}
	return string(initials)
}

func setLoginPageHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src 'self'; script-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("X-Frame-Options", "DENY")
}
