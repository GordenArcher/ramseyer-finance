package finance

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTrialBalanceRendersAsReadOnlyCalculatedLookup(t *testing.T) {
	setupTestDB(t)

	recorder := httptest.NewRecorder()
	TrialBalance(recorder, httptest.NewRequest(http.MethodGet, "/trial-balance?year=2026", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, expected := range []string{
		"Calculated financial lookup",
		"Transaction Entry",
		"Read-only totals for quick lookup",
		"Record Transaction",
		"Review Source Entries",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("Trial Balance page is missing %q", expected)
		}
	}
	for _, forbidden := range []string{`name="amount_`, "/api/trial-balance/save", "/api/trial-balance/add", "Save 2026 Trial Balance"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("Trial Balance still exposes competing write control %q", forbidden)
		}
	}
}

func TestTrialBalanceFlagsHistoricalEntryWithoutCorrespondingAccount(t *testing.T) {
	setupTestDB(t)
	offeringID := categoryID(t, "income", "Adult Service Offertory")
	insertTransaction(t, "2026-04-05", "income", "Adult Service Offertory", offeringID, 125.5)
	data, err := buildTrialBalanceData(2026)
	if err != nil {
		t.Fatalf("build Trial Balance: %v", err)
	}
	if data.UnpairedCount != 1 {
		t.Fatalf("unpaired count = %d, want 1", data.UnpairedCount)
	}
	if data.Difference == 0 {
		t.Fatalf("incomplete historical entry was silently presented as balanced")
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
