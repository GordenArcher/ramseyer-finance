package startup

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"ramseyer-finance/internal/db"
	"ramseyer-finance/internal/startupstate"
	"strings"
	"testing"
)

func TestFreshStartWithoutBackupClearsFinancialRecords(t *testing.T) {
	if err := db.Initialize(filepath.Join(t.TempDir(), "startup.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	defer db.Close()
	if err := startupstate.MarkPending(); err != nil {
		t.Fatalf("mark startup choice pending: %v", err)
	}
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
	required, err := startupstate.DecisionRequired()
	if err != nil {
		t.Fatalf("load startup choice state: %v", err)
	}
	if required {
		t.Fatalf("fresh-start decision remained pending after reset")
	}
}

func TestContinueCompletesOneTimeStartupChoice(t *testing.T) {
	if err := db.Initialize(filepath.Join(t.TempDir(), "startup.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	defer db.Close()
	if err := startupstate.MarkPending(); err != nil {
		t.Fatalf("mark startup choice pending: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/startup/continue", nil)
	recorder := httptest.NewRecorder()
	Continue(recorder, request)

	if location := recorder.Header().Get("Location"); location != "/" {
		t.Fatalf("continue redirect = %q, want dashboard", location)
	}
	required, err := startupstate.DecisionRequired()
	if err != nil {
		t.Fatalf("load startup choice state: %v", err)
	}
	if required {
		t.Fatalf("continue decision remained pending")
	}
}

func TestStartupPageDoesNotReopenAfterDecision(t *testing.T) {
	if err := db.Initialize(filepath.Join(t.TempDir(), "startup.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	defer db.Close()
	if err := startupstate.MarkComplete(); err != nil {
		t.Fatalf("mark startup choice complete: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/startup", nil)
	recorder := httptest.NewRecorder()
	Page(recorder, request)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("completed startup page status = %d, want redirect", recorder.Code)
	}
	if location := recorder.Header().Get("Location"); location != "/" {
		t.Fatalf("completed startup page redirect = %q, want dashboard", location)
	}
}
