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
	pattern := regexp.MustCompile(`^[A-Z][a-z]+[A-Z][a-z]+[0-9]{1,4}$`)
	for range 100 {
		name := randomDisplayName()
		if !pattern.MatchString(name) {
			t.Fatalf("randomDisplayName() = %q", name)
		}
		if len(name) > 32 {
			t.Fatalf("randomDisplayName() length = %d, name = %q", len(name), name)
		}
	}
}
