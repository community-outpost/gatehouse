package mysqlstore

import (
	"regexp"
	"testing"
)

func TestQuotedTableQuotesTable(t *testing.T) {
	t.Parallel()
	if got, want := quotedTable("pending_logins"), "`pending_logins`"; got != want {
		t.Fatalf("quotedTable() = %q, want %q", got, want)
	}
}

func TestRandomDisplayName(t *testing.T) {
	t.Parallel()
	pattern := regexp.MustCompile(`^GO-[A-Z2-7]{13}$`)
	for range 100 {
		name, err := randomDisplayName()
		if err != nil {
			t.Fatal(err)
		}
		if !pattern.MatchString(name) {
			t.Fatalf("randomDisplayName() = %q", name)
		}
		if len(name) > 16 {
			t.Fatalf("randomDisplayName() length = %d, name = %q", len(name), name)
		}
	}
}
