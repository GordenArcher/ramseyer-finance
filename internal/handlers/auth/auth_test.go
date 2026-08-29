package auth

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

func TestUnlockRedirectsToStartupChoice(t *testing.T) {
	if err := db.Initialize(filepath.Join(t.TempDir(), "auth.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	defer db.Close()
	if err := persistPIN("1234"); err != nil {
		t.Fatalf("persist PIN: %v", err)
	}
	if err := startupstate.MarkPending(); err != nil {
		t.Fatalf("mark startup choice pending: %v", err)
	}

	form := url.Values{"pin": {"1234"}}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/unlock", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()

	Unlock(recorder, request)

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("unlock status = %d, want %d", recorder.Code, http.StatusSeeOther)
	}
	if location := recorder.Header().Get("Location"); location != "/startup" {
		t.Fatalf("unlock redirect = %q, want /startup", location)
	}
	if len(recorder.Result().Cookies()) == 0 {
		t.Fatal("unlock did not create an authenticated session cookie")
	}
}

func TestUnlockSkipsStartupChoiceAfterFirstDecision(t *testing.T) {
	if err := db.Initialize(filepath.Join(t.TempDir(), "auth.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	defer db.Close()
	if err := persistPIN("1234"); err != nil {
		t.Fatalf("persist PIN: %v", err)
	}
	if err := startupstate.MarkComplete(); err != nil {
		t.Fatalf("mark startup choice complete: %v", err)
	}

	form := url.Values{"pin": {"1234"}}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/unlock", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	Unlock(recorder, request)

	if location := recorder.Header().Get("Location"); location != "/" {
		t.Fatalf("completed startup redirect = %q, want dashboard", location)
	}
}

func TestSetupPINCreatesOneTimeStartupChoice(t *testing.T) {
	if err := db.Initialize(filepath.Join(t.TempDir(), "auth.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	defer db.Close()

	form := url.Values{"pin": {"1234"}}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/setup", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	SetupPIN(recorder, request)

	if location := recorder.Header().Get("Location"); location != "/startup" {
		t.Fatalf("first PIN redirect = %q, want one-time startup choice", location)
	}
	required, err := startupstate.DecisionRequired()
	if err != nil {
		t.Fatalf("load startup choice state: %v", err)
	}
	if !required {
		t.Fatalf("fresh PIN setup did not mark the startup choice pending")
	}
}
