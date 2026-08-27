package callback

import (
	"testing"
)

func TestParseNormalizesCallback(t *testing.T) {
	t.Parallel()

	result, err := Parse([]byte(`{
		"env":"Example_Alpha",
		"code":"abc123",
		"user_id":42,
		"success":true,
		"opaque":{"nested":true}
	}`), "")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if result.Environment != "example_alpha" {
		t.Fatalf("Environment = %q", result.Environment)
	}
	if result.Code != "ABC123" {
		t.Fatalf("Code = %q", result.Code)
	}
	if _, ok := result.Payload["opaque"]; ok {
		t.Fatalf("unexpected opaque field = %#v", result.Payload["opaque"])
	}
	if result.Payload["code"] != "ABC123" {
		t.Fatalf("canonical code = %v", result.Payload["code"])
	}
}

func TestParseRejectsUserIDOnlyInNestedMetadata(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`{
		"environment":"example_alpha",
		"game_code":"ZXCVBN",
		"success":true,
		"identity":{"id":77}
	}`), "")
	if err == nil {
		t.Fatal("Parse() error = nil")
	}
}

func TestParsePrefersCanonicalFieldsOverAliases(t *testing.T) {
	t.Parallel()

	result, err := Parse([]byte(`{
		"env":"example_alpha",
		"environment":"wrong_environment",
		"code":"CANONICAL",
		"game_code":"WRONG",
		"user_id":42,
		"userId":99,
		"success":true
	}`), "")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if result.Environment != "example_alpha" || result.Code != "CANONICAL" || result.UserID != 42 {
		t.Fatalf("callback = %+v", result)
	}
}

func TestParseRejectsEnvironmentMismatch(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`{"env":"one","code":"ABC","user_id":1,"success":true}`), "two")
	if err == nil {
		t.Fatal("Parse() error = nil")
	}
}

func TestParseRejectsFailedCallbackWithoutUser(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`{"env":"example_native","code":"ABC","success":false}`), "")
	if err == nil {
		t.Fatal("Parse() error = nil")
	}
}
