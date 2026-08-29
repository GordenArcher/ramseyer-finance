package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"ramseyer-finance/internal/db"
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
