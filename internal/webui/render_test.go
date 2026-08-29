package webui

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoginFeedbackStaysInsidePINForm(t *testing.T) {
	recorder := httptest.NewRecorder()
	RenderStandaloneTemplate(recorder, "login", struct {
		Mode        string
		Message     string
		MessageTone string
	}{
		Mode:        "unlock",
		Message:     "Incorrect PIN",
		MessageTone: "warning",
	})

	body := recorder.Body.String()
	formStart := strings.Index(body, `<form`)
	alertStart := strings.Index(body, `data-auto-dismiss-alert`)
	formEnd := strings.Index(body, `</form>`)
	if formStart < 0 || alertStart < 0 || formEnd < 0 {
		t.Fatalf("login form or feedback alert did not render: %s", body)
	}
	if !(formStart < alertStart && alertStart < formEnd) {
		t.Fatalf("login feedback rendered outside the PIN form and can displace the two-column grid")
	}
}

func TestStartupBackupStepExplainsResetAndRecoveryLifetime(t *testing.T) {
	recorder := httptest.NewRecorder()
	RenderStandaloneTemplate(recorder, "startup", struct {
		Step        string
		Message     string
		MessageTone string
	}{
		Step: "backup",
	})

	body := recorder.Body.String()
	for _, expected := range []string{
		"Would you like a safety backup first?",
		"expire automatically after three days",
		"Starting fresh clears transactions, Trial Balances, budgets, opening balances",
		`name="safety_backup" value="yes" checked`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("startup backup screen missing %q: %s", expected, body)
		}
	}
}
