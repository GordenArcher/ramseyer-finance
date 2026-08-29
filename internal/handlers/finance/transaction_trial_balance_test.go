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

func TestTransactionLifecycleRecalculatesSingleEntryTrialBalance(t *testing.T) {
	setupTestDB(t)
	offeringID := categoryID(t, "income", "Adult Service Offertory")
	cashID := categoryID(t, "asset", "Cash on hand")

	addValues := url.Values{
		"date":        {"2026-04-05"},
		"type":        {"income"},
		"category_id": {strconv.FormatInt(offeringID, 10)},
		"description": {"Sunday service"},
		"amount":      {"125.50"},
		"return_to":   {"/data-entry"},
	}
	addRequest := httptest.NewRequest(http.MethodPost, "/api/transaction/add", strings.NewReader(addValues.Encode()))
	addRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addRecorder := httptest.NewRecorder()
	AddTransaction(addRecorder, addRequest)
	if addRecorder.Code != http.StatusSeeOther {
		t.Fatalf("add transaction status = %d, want 303: %s", addRecorder.Code, addRecorder.Body.String())
	}
	assertCalculatedTrialBalanceAmount(t, 2026, offeringID, 125.5)
	assertCalculatedTrialBalanceAmount(t, 2026, cashID, 0)

	var transactionID int64
	var paymentMethod string
	if err := db.DB.QueryRow("SELECT id, payment_method FROM transactions WHERE category_id = ?", offeringID).Scan(&transactionID, &paymentMethod); err != nil {
		t.Fatalf("load created transaction: %v", err)
	}
	if paymentMethod != "cash" {
		t.Fatalf("default payment method = %q, want cash", paymentMethod)
	}
	printingID := categoryID(t, "expenditure", "Printing & Stationery")
	updateValues := url.Values{
		"id":             {strconv.FormatInt(transactionID, 10)},
		"date":           {"2027-02-10"},
		"type":           {"expenditure"},
		"category_id":    {strconv.FormatInt(printingID, 10)},
		"payment_method": {"cash"},
		"description":    {"Annual stationery"},
		"amount":         {"40"},
		"return_to":      {"/transactions?year=2027"},
	}
	updateRequest := httptest.NewRequest(http.MethodPost, "/api/transaction/update", strings.NewReader(updateValues.Encode()))
	updateRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	updateRecorder := httptest.NewRecorder()
	UpdateTransaction(updateRecorder, updateRequest)
	if updateRecorder.Code != http.StatusSeeOther {
		t.Fatalf("update transaction status = %d, want 303: %s", updateRecorder.Code, updateRecorder.Body.String())
	}
	assertCalculatedTrialBalanceAmount(t, 2026, offeringID, 0)
	assertCalculatedTrialBalanceAmount(t, 2027, printingID, 40)
	assertCalculatedTrialBalanceAmount(t, 2027, cashID, 0)

	deleteValues := url.Values{"id": {strconv.FormatInt(transactionID, 10)}, "return_to": {"/transactions?year=2027"}}
	deleteRequest := httptest.NewRequest(http.MethodPost, "/api/transaction/delete", strings.NewReader(deleteValues.Encode()))
	deleteRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	deleteRecorder := httptest.NewRecorder()
	DeleteTransaction(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != http.StatusSeeOther {
		t.Fatalf("delete transaction status = %d, want 303: %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	assertCalculatedTrialBalanceAmount(t, 2027, printingID, 0)
	assertCalculatedTrialBalanceAmount(t, 2027, cashID, 0)
}

func TestTransactionPaymentMethodsAreOptionalVisibleFlags(t *testing.T) {
	setupTestDB(t)
	offeringID := categoryID(t, "income", "Adult Service Offertory")

	scenarios := []struct {
		method         string
		wantStored     string
		transactionDay string
	}{
		{method: "", wantStored: "cash", transactionDay: "2026-05-03"},
		{method: "momo", wantStored: "momo", transactionDay: "2026-05-10"},
		{method: "cheque", wantStored: "cheque", transactionDay: "2026-05-17"},
		{method: "bank", wantStored: "bank", transactionDay: "2026-05-24"},
	}

	for _, scenario := range scenarios {
		values := url.Values{
			"date":           {scenario.transactionDay},
			"type":           {"income"},
			"category_id":    {strconv.FormatInt(offeringID, 10)},
			"payment_method": {scenario.method},
			"description":    {"Sunday service " + scenario.transactionDay},
			"amount":         {"10"},
			"return_to":      {"/data-entry"},
		}
		request := httptest.NewRequest(http.MethodPost, "/api/transaction/add", strings.NewReader(values.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		recorder := httptest.NewRecorder()
		AddTransaction(recorder, request)
		if recorder.Code != http.StatusSeeOther {
			t.Fatalf("add %q payment method status = %d, want 303: %s", scenario.method, recorder.Code, recorder.Body.String())
		}

		var storedMethod string
		if err := db.DB.QueryRow(`
			SELECT payment_method
			FROM transactions t
			WHERE t.date = ?
		`, scenario.transactionDay).Scan(&storedMethod); err != nil {
			t.Fatalf("load %q payment classification: %v", scenario.method, err)
		}
		if storedMethod != scenario.wantStored {
			t.Fatalf("payment %q stored as method=%q, want method=%q", scenario.method, storedMethod, scenario.wantStored)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/data-entry", nil)
	recorder := httptest.NewRecorder()
	DataEntryPage(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("data entry status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	if strings.Contains(body, "counter_category_id") || strings.Contains(body, "Corresponding account") {
		t.Fatalf("data entry still exposes an internal corresponding-account field")
	}
	for _, label := range []string{"Cash", "Momo", "Cheque", "Bank"} {
		if !strings.Contains(body, ">"+label+"</option>") {
			t.Fatalf("data entry omitted %s payment method", label)
		}
	}
}

func TestCalculatedTrialBalanceKeepsTransactionsInTheirOwnYear(t *testing.T) {
	setupTestDB(t)
	offeringID := categoryID(t, "income", "Adult Service Offertory")
	cashID := categoryID(t, "asset", "Cash on hand")
	insertPairedTransaction(t, "2026-12-28", "income", offeringID, cashID, 300)

	data, err := buildTrialBalanceData(2027)
	if err != nil {
		t.Fatalf("build 2027 Trial Balance: %v", err)
	}
	if amountForTrialBalanceAccount(data.Lines, "Adult Service Offertory") != 0 {
		t.Fatalf("2026 offering leaked into the 2027 Trial Balance")
	}
	if data.IncomeTotal != 0 || data.AssetTotal != 0 {
		t.Fatalf("2027 totals include prior-year activity: %#v", data)
	}
}

func TestSundayEntriesAddTogetherAndRemainYearScopedEverywhere(t *testing.T) {
	setupTestDB(t)
	offeringID := categoryID(t, "income", "Adult Service Offertory")
	insertTransaction(t, "2025-12-28", "income", "Adult Service Offertory", offeringID, 900)
	insertTransaction(t, "2026-01-04", "income", "Adult Service Offertory", offeringID, 3000)
	insertTransaction(t, "2026-01-11", "income", "Adult Service Offertory", offeringID, 7000)

	trial2026, err := buildTrialBalanceData(2026)
	if err != nil {
		t.Fatalf("build 2026 Trial Balance: %v", err)
	}
	if amount := amountForTrialBalanceAccount(trial2026.Lines, "Adult Service Offertory"); amount != 10000 {
		t.Fatalf("2026 Sunday offering total = %.2f, want 10000", amount)
	}
	trial2025, err := buildTrialBalanceData(2025)
	if err != nil {
		t.Fatalf("build 2025 Trial Balance: %v", err)
	}
	if amount := amountForTrialBalanceAccount(trial2025.Lines, "Adult Service Offertory"); amount != 900 {
		t.Fatalf("2025 Sunday offering total = %.2f, want 900", amount)
	}

	notes2026, err := buildNotesData(2026)
	if err != nil {
		t.Fatalf("build 2026 notes: %v", err)
	}
	if total := noteSectionByNumber(t, notes2026.Notes, "4").Total; total != 10000 {
		t.Fatalf("2026 Note 4 total = %.2f, want 10000", total)
	}
	annual2026, err := buildAnnualData(2026)
	if err != nil {
		t.Fatalf("build 2026 annual report: %v", err)
	}
	if annual2026.TotalIncome != 10000 {
		t.Fatalf("2026 annual income = %.2f, want 10000", annual2026.TotalIncome)
	}
}

func insertPairedTransaction(t *testing.T, date, transactionType string, categoryID, counterCategoryID int64, amount float64) {
	t.Helper()
	var category string
	if err := db.DB.QueryRow("SELECT name FROM categories WHERE id = ?", categoryID).Scan(&category); err != nil {
		t.Fatalf("load transaction category: %v", err)
	}
	if _, err := db.DB.Exec(`
		INSERT INTO transactions (date, type, category, category_id, amount)
		VALUES (?, ?, ?, ?, ?)
	`, date, transactionType, category, categoryID, amount); err != nil {
		t.Fatalf("insert paired transaction: %v", err)
	}
}

func assertCalculatedTrialBalanceAmount(t *testing.T, year int, categoryID int64, expected float64) {
	t.Helper()
	data, err := buildTrialBalanceData(year)
	if err != nil {
		t.Fatalf("build %d Trial Balance: %v", year, err)
	}
	for _, line := range data.Lines {
		if line.ID == categoryID {
			if line.Amount != expected {
				t.Fatalf("Trial Balance category %d/%d amount = %.2f, want %.2f", year, categoryID, line.Amount, expected)
			}
			return
		}
	}
	if expected == 0 {
		return
	}
	t.Fatalf("Trial Balance category %d/%d was not rendered", year, categoryID)
}

func amountForTrialBalanceAccount(lines []TrialBalanceLine, account string) float64 {
	for _, line := range lines {
		if line.Account == account {
			return line.Amount
		}
	}
	return 0
}
