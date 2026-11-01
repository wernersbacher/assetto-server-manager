package accounts

import (
	"testing"
	"time"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
)

func TestFilterAuditEntries(t *testing.T) {
	day := func(d int, h int) time.Time { return time.Date(2026, 5, d, h, 0, 0, 0, time.Local) }

	entries := []*smmodel.AuditEntry{
		{User: "Alice", Method: "POST", Time: day(1, 10)},
		{User: "bob", Method: "GET", Time: day(2, 23)},
		{User: "alice", Method: "DELETE", Time: day(3, 0)},
	}

	if got := FilterAuditEntries(entries, AuditFilter{}); len(got) != 3 {
		t.Fatalf("no filter keeps everything, got %d", len(got))
	}

	if got := FilterAuditEntries(entries, AuditFilter{User: "ALI"}); len(got) != 2 {
		t.Fatalf("user filter, got %d", len(got))
	}

	if got := FilterAuditEntries(entries, AuditFilter{Method: "get"}); len(got) != 1 || got[0].User != "bob" {
		t.Fatalf("method filter, got %v", got)
	}

	// the To day is included completely, the next day is not
	got := FilterAuditEntries(entries, AuditFilter{From: day(2, 0), To: day(2, 0)})

	if len(got) != 1 || got[0].User != "bob" {
		t.Fatalf("date filter, got %v", got)
	}
}

func TestPaginateAuditEntries(t *testing.T) {
	entries := make([]*smmodel.AuditEntry, 5)

	got, page, pages := paginateAuditEntries(entries, 7, 2)

	if len(got) != 1 || page != 2 || pages != 3 {
		t.Fatalf("unexpected page %d entries, page %d of %d", len(got), page, pages)
	}

	if got, page, pages := paginateAuditEntries(nil, 0, 2); len(got) != 0 || page != 0 || pages != 0 {
		t.Fatalf("unexpected empty page")
	}
}

func TestFilterAccounts(t *testing.T) {
	accounts := []*smmodel.Account{
		{Name: "Alice", GUID: "76561190000000001"},
		{Name: "bob"},
	}

	if got := filterAccounts(accounts, " "); len(got) != 2 {
		t.Fatalf("empty query keeps all, got %d", len(got))
	}

	if got := filterAccounts(accounts, "ALI"); len(got) != 1 || got[0].Name != "Alice" {
		t.Fatalf("name search, got %v", got)
	}

	if got := filterAccounts(accounts, "0000001"); len(got) != 1 {
		t.Fatalf("guid search, got %v", got)
	}
}
