package identity

import "testing"

func TestNormalizeDisplayNameAllowsSafeCharacters(t *testing.T) {
	t.Parallel()
	name, err := NormalizeDisplayName("  Player_One-1.0  ")
	if err != nil || name != "Player_One-1.0" {
		t.Fatalf("NormalizeDisplayName() = %q, %v", name, err)
	}
	for _, symbol := range "_-.()[]{}!?@#$%^&*+=~'" {
		value := "A" + string(symbol) + "B"
		if _, err := NormalizeDisplayName(value); err != nil {
			t.Errorf("NormalizeDisplayName(%q) rejected safe symbol: %v", value, err)
		}
	}
}

func TestNormalizeDisplayNameRejectsMarkupAndFormatControls(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"<script>", `Player"One`, "Player\u202eOne", "Plаyer", "A,B", "A:B", "A;B", "A/B", `A\B`, "A|B", "A`B",
	} {
		if _, err := NormalizeDisplayName(value); err == nil {
			t.Fatalf("NormalizeDisplayName(%q) accepted unsafe input", value)
		}
	}
}

func TestNormalizeDisplayNameEnforcesGameServiceLength(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"AB", "12345678901234567"} {
		if _, err := NormalizeDisplayName(value); err == nil {
			t.Fatalf("NormalizeDisplayName(%q) accepted an unsupported length", value)
		}
	}
}
