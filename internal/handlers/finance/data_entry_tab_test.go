package finance

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDataEntryTabFallsBackToIncomeForUnknownValues(t *testing.T) {
	cases := map[string]string{
		"":             "income",
		"income":       "income",
		"expenditure":  "expenditure",
		"asset":        "asset",
		"liability":    "liability",
		" expenditure": "expenditure",
		"settings":     "income",
	}
	for requested, want := range cases {
		if got := dataEntryTab(requested); got != want {
			t.Fatalf("dataEntryTab(%q) = %q, want %q", requested, got, want)
		}
	}
}

func TestDataEntryPageOpensTheRequestedTab(t *testing.T) {
	setupTestDB(t)

	recorder := httptest.NewRecorder()
	DataEntryPage(recorder, httptest.NewRequest(http.MethodGet, "/data-entry?tab=expenditure", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}

	body := recorder.Body.String()
	if !strings.Contains(body, `id="expenditure-panel" class="page-tab-panel active"`) {
		t.Fatalf("expenditure panel was not left active")
	}
	if strings.Contains(body, `id="income-panel" class="page-tab-panel active"`) {
		t.Fatalf("income panel stayed active after an expenditure redirect")
	}
	if strings.Contains(body, "Bulk Import (Excel)") {
		t.Fatalf("the old shared bulk card is still on the page")
	}
	if !strings.Contains(body, "/api/data-entry/export?type=expenditure") {
		t.Fatalf("the expenditure panel has no Save as Excel link")
	}
	for _, transactionType := range []string{"income", "expenditure", "asset", "liability"} {
		link := "/api/data-entry/template?type=" + transactionType
		if !strings.Contains(body, link) {
			t.Fatalf("the %s panel has no template link", transactionType)
		}
	}
}
