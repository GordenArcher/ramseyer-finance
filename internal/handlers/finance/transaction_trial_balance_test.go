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

func TestTransactionLifecycleUpdatesTrialBalanceImmediately(t *testing.T) {
	setupTestDB(t)
	if _, err := buildTrialBalanceData(2026); err != nil {
		t.Fatalf("initialize 2026 Trial Balance before transaction entry: %v", err)
	}

	offeringID := categoryID(t, "income", "Adult Service Offertory")
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
	assertTrialBalanceCategoryAmount(t, 2026, offeringID, 125.5)

	var transactionID int64
	if err := db.DB.QueryRow("SELECT id FROM transactions WHERE category_id = ?", offeringID).Scan(&transactionID); err != nil {
		t.Fatalf("load created transaction: %v", err)
	}
	printingID := categoryID(t, "expenditure", "Printing & Stationery")
	updateValues := url.Values{
		"id":          {strconv.FormatInt(transactionID, 10)},
		"date":        {"2027-02-10"},
		"type":        {"expenditure"},
		"category_id": {strconv.FormatInt(printingID, 10)},
		"description": {"Annual stationery"},
		"amount":      {"40"},
		"return_to":   {"/transactions?year=2027"},
	}
	updateRequest := httptest.NewRequest(http.MethodPost, "/api/transaction/update", strings.NewReader(updateValues.Encode()))
	updateRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	updateRecorder := httptest.NewRecorder()
	UpdateTransaction(updateRecorder, updateRequest)
	if updateRecorder.Code != http.StatusSeeOther {
		t.Fatalf("update transaction status = %d, want 303: %s", updateRecorder.Code, updateRecorder.Body.String())
	}
	assertTrialBalanceCategoryAmount(t, 2026, offeringID, 0)
	assertTrialBalanceCategoryAmount(t, 2027, printingID, 40)

	deleteValues := url.Values{
		"id":        {strconv.FormatInt(transactionID, 10)},
		"return_to": {"/transactions?year=2027"},
	}
	deleteRequest := httptest.NewRequest(http.MethodPost, "/api/transaction/delete", strings.NewReader(deleteValues.Encode()))
	deleteRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	deleteRecorder := httptest.NewRecorder()
	DeleteTransaction(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != http.StatusSeeOther {
		t.Fatalf("delete transaction status = %d, want 303: %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	assertTrialBalanceCategoryAmount(t, 2027, printingID, 0)
}

func TestExistingYearBackfillsTransactionsEnteredAfterInitialization(t *testing.T) {
	setupTestDB(t)
	if _, err := buildTrialBalanceData(2026); err != nil {
		t.Fatalf("initialize Trial Balance: %v", err)
	}
	if _, err := db.DB.Exec("DELETE FROM settings WHERE key = ?", trialBalanceLiveSyncSettingPrefix+"2026"); err != nil {
		t.Fatalf("remove live-sync marker to emulate previous release: %v", err)
	}

	offeringID := categoryID(t, "income", "Adult Service Offertory")
	if _, err := db.DB.Exec(`
		INSERT INTO transactions
			(date, type, category, category_id, description, amount, note_ref, created_at, updated_at)
		VALUES ('2026-05-01', 'income', 'Adult Service Offertory', ?, '', 75, '4',
			datetime('now','localtime','+1 minute'), datetime('now','localtime','+1 minute'))
	`, offeringID); err != nil {
		t.Fatalf("insert transaction created after Trial Balance initialization: %v", err)
	}

	if _, err := buildTrialBalanceData(2026); err != nil {
		t.Fatalf("reopen Trial Balance with compatibility backfill: %v", err)
	}
	assertTrialBalanceCategoryAmount(t, 2026, offeringID, 75)
}

func assertTrialBalanceCategoryAmount(t *testing.T, year int, categoryID int64, expected float64) {
	t.Helper()
	var accountType string
	var debit, credit float64
	if err := db.DB.QueryRow(`
		SELECT account_type, debit, credit
		FROM trial_balance_entries
		WHERE year = ? AND source_category_id = ?
	`, year, categoryID).Scan(&accountType, &debit, &credit); err != nil {
		t.Fatalf("load Trial Balance category %d/%d: %v", year, categoryID, err)
	}
	if actual := trialBalanceAmount(accountType, debit, credit); actual != expected {
		t.Fatalf("Trial Balance category %d/%d amount = %.2f, want %.2f", year, categoryID, actual, expected)
	}
}
