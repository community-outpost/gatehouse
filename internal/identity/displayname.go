package identity

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var ErrDisplayNameUnavailable = errors.New("display name is unavailable")

const (
	MinDisplayNameLength      = 3
	MaxDisplayNameLength      = 16
	allowedDisplayNameSymbols = " _-.()[]{}!?@#$%^&*+=~'"
)

func NormalizeDisplayName(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", errors.New("display name is not valid UTF-8")
	}
	value = strings.Join(strings.Fields(value), " ")
	length := utf8.RuneCountInString(value)
	if length < MinDisplayNameLength || length > MaxDisplayNameLength {
		return "", errors.New("display name must contain 3 to 16 characters")
	}
	for _, character := range value {
		if character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9' || strings.ContainsRune(allowedDisplayNameSymbols, character) {
			continue
		}
		return "", errors.New("display name contains an unsupported character")
	}
	return value, nil
}

func ValidStoredDisplayName(value string) bool {
	normalized, err := NormalizeDisplayName(value)
	return err == nil && normalized == value
}
