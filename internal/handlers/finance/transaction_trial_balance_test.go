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

func TestTransactionLifecycleRecalculatesBalancedTrialBalance(t *testing.T) {
	setupTestDB(t)
	offeringID := categoryID(t, "income", "Adult Service Offertory")
	cashID := categoryID(t, "asset", "Cash on hand")

	addValues := url.Values{
		"date":                {"2026-04-05"},
		"type":                {"income"},
		"category_id":         {strconv.FormatInt(offeringID, 10)},
		"counter_category_id": {strconv.FormatInt(cashID, 10)},
		"description":         {"Sunday service"},
		"amount":              {"125.50"},
		"return_to":           {"/data-entry"},
	}
	addRequest := httptest.NewRequest(http.MethodPost, "/api/transaction/add", strings.NewReader(addValues.Encode()))
	addRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	addRecorder := httptest.NewRecorder()
	AddTransaction(addRecorder, addRequest)
	if addRecorder.Code != http.StatusSeeOther {
		t.Fatalf("add transaction status = %d, want 303: %s", addRecorder.Code, addRecorder.Body.String())
	}
	assertCalculatedTrialBalanceAmount(t, 2026, offeringID, 125.5)
	assertCalculatedTrialBalanceAmount(t, 2026, cashID, 125.5)
	assertCalculatedTrialBalanceBalanced(t, 2026)

	var transactionID int64
	if err := db.DB.QueryRow("SELECT id FROM transactions WHERE category_id = ?", offeringID).Scan(&transactionID); err != nil {
		t.Fatalf("load created transaction: %v", err)
	}
	printingID := categoryID(t, "expenditure", "Printing & Stationery")
	updateValues := url.Values{
		"id":                  {strconv.FormatInt(transactionID, 10)},
		"date":                {"2027-02-10"},
		"type":                {"expenditure"},
		"category_id":         {strconv.FormatInt(printingID, 10)},
		"counter_category_id": {strconv.FormatInt(cashID, 10)},
		"description":         {"Annual stationery"},
		"amount":              {"40"},
		"return_to":           {"/transactions?year=2027"},
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
	assertCalculatedTrialBalanceAmount(t, 2027, cashID, -40)
	assertCalculatedTrialBalanceBalanced(t, 2027)

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

func TestCalculatedTrialBalanceCarriesPriorNetAssetsAsOpeningFund(t *testing.T) {
	setupTestDB(t)
	offeringID := categoryID(t, "income", "Adult Service Offertory")
	cashID := categoryID(t, "asset", "Cash on hand")
	insertPairedTransaction(t, "2026-12-28", "income", offeringID, cashID, 300)

	data, err := buildTrialBalanceData(2027)
	if err != nil {
		t.Fatalf("build 2027 Trial Balance: %v", err)
	}
	if amountForTrialBalanceAccount(data.Lines, "Opening Accumulated Fund") != 300 {
		t.Fatalf("2027 opening accumulated fund = %.2f, want 300", amountForTrialBalanceAccount(data.Lines, "Opening Accumulated Fund"))
	}
	if data.Difference != 0 {
		t.Fatalf("2027 Trial Balance difference = %.2f, want 0", data.Difference)
	}
}

func insertPairedTransaction(t *testing.T, date, transactionType string, categoryID, counterCategoryID int64, amount float64) {
	t.Helper()
	var category string
	if err := db.DB.QueryRow("SELECT name FROM categories WHERE id = ?", categoryID).Scan(&category); err != nil {
		t.Fatalf("load transaction category: %v", err)
	}
	if _, err := db.DB.Exec(`
		INSERT INTO transactions (date, type, category, category_id, counter_category_id, amount)
		VALUES (?, ?, ?, ?, ?, ?)
	`, date, transactionType, category, categoryID, counterCategoryID, amount); err != nil {
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
	t.Fatalf("Trial Balance category %d/%d was not rendered", year, categoryID)
}

func assertCalculatedTrialBalanceBalanced(t *testing.T, year int) {
	t.Helper()
	data, err := buildTrialBalanceData(year)
	if err != nil {
		t.Fatalf("build %d Trial Balance: %v", year, err)
	}
	if data.Difference != 0 {
		t.Fatalf("Trial Balance %d difference = %.2f, want 0", year, data.Difference)
	}
}

func amountForTrialBalanceAccount(lines []TrialBalanceLine, account string) float64 {
	for _, line := range lines {
		if line.Account == account {
			return line.Amount
		}
	}
	return 0
}
