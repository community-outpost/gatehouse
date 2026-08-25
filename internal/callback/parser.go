package callback

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/community-outpost/gatehouse/internal/config"
)

func Parse(body []byte, pathEnvironment string) (AuthCallback, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var root map[string]any
	if err := decoder.Decode(&root); err != nil {
		return AuthCallback{}, fmt.Errorf("callback body must be a JSON object: %w", err)
	}
	if root == nil {
		return AuthCallback{}, errors.New("callback body must be a JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return AuthCallback{}, errors.New("callback body must contain exactly one JSON object")
	}

	environment, _ := stringValue(root, "env", "environment")
	if environment == "" {
		environment = pathEnvironment
	}
	environment = strings.ToLower(strings.TrimSpace(environment))
	if !config.ValidEnvironment(environment) {
		return AuthCallback{}, errors.New("callback environment is missing or invalid")
	}
	if pathEnvironment != "" && !strings.EqualFold(environment, strings.TrimSpace(pathEnvironment)) {
		return AuthCallback{}, errors.New("callback body environment does not match route environment")
	}

	code, _ := stringValue(root, "code", "game_code", "gamecode")
	code = strings.ToUpper(strings.TrimSpace(code))
	if !validCode(code) {
		return AuthCallback{}, errors.New("callback code must contain 1 to 32 ASCII letters or digits")
	}

	success, ok := boolValue(root, "success")
	if !ok {
		return AuthCallback{}, errors.New("success must be a boolean")
	}
	userID, ok, err := int64Value(root, "user_id", "userId")
	if err != nil {
		return AuthCallback{}, err
	}
	if !ok {
		return AuthCallback{}, errors.New("user_id is required")
	}
	if success && userID <= 0 {
		return AuthCallback{}, errors.New("a successful callback requires a positive user_id")
	}

	root["env"] = environment
	root["code"] = code
	root["user_id"] = userID
	root["success"] = success

	return AuthCallback{
		Environment: environment,
		Code:        code,
		UserID:      userID,
		Success:     success,
		Payload:     root,
	}, nil
}

func validCode(code string) bool {
	if len(code) < 1 || len(code) > 32 {
		return false
	}
	for _, character := range code {
		if !((character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9')) {
			return false
		}
	}
	return true
}

func int64Value(object map[string]any, names ...string) (int64, bool, error) {
	value, ok := find(object, names...)
	if !ok || value == nil {
		return 0, false, nil
	}
	parsed, err := parseInt64(value)
	if err != nil {
		return 0, false, fmt.Errorf("%s must be a 64-bit integer", names[0])
	}
	return parsed, true, nil
}

func parseInt64(value any) (int64, error) {
	switch typed := value.(type) {
	case json.Number:
		return typed.Int64()
	case string:
		return strconv.ParseInt(typed, 10, 64)
	default:
		return 0, errors.New("not an integer")
	}
}

func stringValue(object map[string]any, names ...string) (string, bool) {
	value, ok := find(object, names...)
	if !ok || value == nil {
		return "", false
	}
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	return text, true
}

func boolValue(object map[string]any, names ...string) (bool, bool) {
	value, ok := find(object, names...)
	if !ok {
		return false, false
	}
	result, ok := value.(bool)
	return result, ok
}

func find(object map[string]any, names ...string) (any, bool) {
	for _, name := range names {
		for property, value := range object {
			if strings.EqualFold(property, name) {
				return value, true
			}
		}
	}
	return nil, false
}
