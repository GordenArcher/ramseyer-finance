package finance

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"ramseyer-finance/internal/db"
)

func TestTrialBalanceRendersAsPrimarySingleAmountEntry(t *testing.T) {
	setupTestDB(t)

	recorder := httptest.NewRecorder()
	TrialBalance(recorder, httptest.NewRequest(http.MethodGet, "/trial-balance?year=2026", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		"Primary financial entry",
		"Enter balances here",
		"Opening Accumulated Fund",
		"Opening funds and equity",
		`data-open-modal="trial-balance-add-row"`,
		`name="amount_`,
		"Save 2026 Trial Balance",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("Trial Balance page is missing %q", expected)
		}
	}
	if strings.Contains(body, `name="account_type_`) || strings.Contains(body, `name="note_ref_`) {
		t.Fatalf("row-level mapping selectors remained on the primary entry surface")
	}
	if selectCount := strings.Count(body, "<select"); selectCount != 2 {
		t.Fatalf("Trial Balance rendered %d selectors, want only the two add-row modal selectors", selectCount)
	}
}

func TestSaveTrialBalanceConvertsSignedNaturalAmount(t *testing.T) {
	setupTestDB(t)
	if _, err := buildTrialBalanceData(2026); err != nil {
		t.Fatalf("initialize Trial Balance: %v", err)
	}

	var entryID int64
	if err := db.DB.QueryRow(`
		SELECT id FROM trial_balance_entries
		WHERE year = 2026 AND account_type = 'income' AND account_name = 'Adult Service Offertory'
	`).Scan(&entryID); err != nil {
		t.Fatalf("load test Trial Balance row: %v", err)
	}

	values := url.Values{
		"year":     {"2026"},
		"entry_id": {strconv.FormatInt(entryID, 10)},
		"amount_" + strconv.FormatInt(entryID, 10): {"125.50"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/trial-balance/save", strings.NewReader(values.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	SaveTrialBalance(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("save status = %d, want 303: %s", recorder.Code, recorder.Body.String())
	}

	var debit, credit float64
	if err := db.DB.QueryRow("SELECT debit, credit FROM trial_balance_entries WHERE id = ?", entryID).Scan(&debit, &credit); err != nil {
		t.Fatalf("load saved Trial Balance amount: %v", err)
	}
	if debit != 0 || credit != 125.5 {
		t.Fatalf("saved income balance = debit %.2f, credit %.2f; want credit 125.50", debit, credit)
	}
}

func TestCategoriesFiltersSearchTheFullRegister(t *testing.T) {
	setupTestDB(t)

	request := httptest.NewRequest(http.MethodGet, "/categories?q=Adult+Service&type=income&status=active&note=4", nil)
	recorder := httptest.NewRecorder()
	CategoriesPage(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("categories status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "Adult Service Offertory") {
		t.Fatalf("full-register search did not return the matching account")
	}
	if strings.Contains(body, "Welfare Offertory") {
		t.Fatalf("full-register search returned a non-matching account")
	}
	if !strings.Contains(body, `name="q" value="Adult Service"`) {
		t.Fatalf("category search value was not preserved")
	}
}
