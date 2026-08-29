package startup

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"ramseyer-finance/internal/db"
	"strings"
	"testing"
)

func TestFreshStartWithoutBackupClearsFinancialRecords(t *testing.T) {
	if err := db.Initialize(filepath.Join(t.TempDir(), "startup.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	defer db.Close()
	if _, err := db.DB.Exec(`
		INSERT INTO transactions (date, type, category, amount)
		VALUES ('2026-01-01', 'income', 'Test income', 100)
	`); err != nil {
		t.Fatalf("insert transaction: %v", err)
	}

	form := url.Values{"safety_backup": {"no"}}
	request := httptest.NewRequest(http.MethodPost, "/api/startup/fresh", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()

	FreshStart(recorder, request)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("fresh-start status = %d, want %d", recorder.Code, http.StatusSeeOther)
	}
	if location := recorder.Header().Get("Location"); location != "/startup?step=complete" {
		t.Fatalf("fresh-start redirect = %q, want completion screen", location)
	}
	var transactionCount int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM transactions`).Scan(&transactionCount); err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	if transactionCount != 0 {
		t.Fatalf("transaction count = %d, want 0", transactionCount)
	}

	var recoveryCount int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM backup_events WHERE kind = 'fresh-start-recovery'`).Scan(&recoveryCount); err != nil {
		t.Fatalf("count recovery events: %v", err)
	}
	if recoveryCount != 0 {
		t.Fatalf("no-backup choice created %d recovery events, want 0", recoveryCount)
	}
}
