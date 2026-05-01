package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"ramseyer-finance/internal/db"
	"strconv"
	"strings"
	"testing"
	"time"
)

func setupTestDB(t *testing.T) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")
	if err := db.Initialize(path); err != nil {
		t.Fatalf("initialize test database: %v", err)
	}

	t.Cleanup(db.Close)
}

func categoryID(t *testing.T, categoryType, name string) int64 {
	t.Helper()

	var id int64
	if err := db.DB.QueryRow(
		`SELECT id FROM categories WHERE type=? AND name=?`,
		categoryType,
		name,
	).Scan(&id); err != nil {
		t.Fatalf("lookup category %s/%s: %v", categoryType, name, err)
	}

	return id
}

func insertTransaction(t *testing.T, transactionDate, transactionType, category string, categoryID int64, amount float64) {
	t.Helper()

	if _, err := db.DB.Exec(
		`INSERT INTO transactions (date, type, category, category_id, description, amount)
		VALUES (?, ?, ?, ?, '', ?)`,
		transactionDate,
		transactionType,
		category,
		categoryID,
		amount,
	); err != nil {
		t.Fatalf("insert transaction %s/%s: %v", transactionType, category, err)
	}
}

func TestBuildDashboardDataUsesOpeningBalances(t *testing.T) {
	setupTestDB(t)

	if _, err := db.DB.Exec(
		`INSERT INTO opening_balances (year, account_type, amount) VALUES
		(2026, 'bank', 100),
		(2026, 'cash', 20),
		(2026, 'momo', 30)`,
	); err != nil {
		t.Fatalf("insert opening balances: %v", err)
	}

	insertTransaction(t, "2026-04-10", "income", "Offering", categoryID(t, "income", "Offering"), 300)
	insertTransaction(t, "2026-04-12", "expenditure", "Stationery", categoryID(t, "expenditure", "Stationery"), 90)
	insertTransaction(t, "2026-04-29", "asset", "Bank", categoryID(t, "asset", "Bank"), 25)
	insertTransaction(t, "2026-05-01", "asset", "Bank", categoryID(t, "asset", "Bank"), 40)

	data, err := buildDashboardData(
		time.Date(2026, time.April, 30, 10, 0, 0, 0, time.UTC),
		"dashboard",
		"",
		"",
	)
	if err != nil {
		t.Fatalf("build dashboard data: %v", err)
	}

	if data.MonthIncome != 300 {
		t.Fatalf("month income = %v, want 300", data.MonthIncome)
	}
	if data.MonthExpense != 90 {
		t.Fatalf("month expense = %v, want 90", data.MonthExpense)
	}
	if data.BankBalance != 125 {
		t.Fatalf("bank balance = %v, want 125", data.BankBalance)
	}
}

func TestBuildBalanceDataUsesYearEndLogic(t *testing.T) {
	setupTestDB(t)

	if _, err := db.DB.Exec(
		`INSERT INTO opening_balances (year, account_type, amount) VALUES (2026, 'bank', 100)`,
	); err != nil {
		t.Fatalf("insert opening balance: %v", err)
	}

	insertTransaction(t, "2025-06-01", "asset", "Receivables (Debtors)", categoryID(t, "asset", "Receivables (Debtors)"), 60)
	insertTransaction(t, "2025-07-01", "liability", "Payables (Creditors)", categoryID(t, "liability", "Payables (Creditors)"), 30)
	insertTransaction(t, "2025-12-31", "asset", "Bank", categoryID(t, "asset", "Bank"), 500)
	insertTransaction(t, "2026-02-01", "asset", "Bank", categoryID(t, "asset", "Bank"), 40)
	insertTransaction(t, "2026-03-15", "income", "Tithe", categoryID(t, "income", "Tithe"), 200)
	insertTransaction(t, "2026-03-20", "expenditure", "Stationery", categoryID(t, "expenditure", "Stationery"), 80)

	data, err := buildBalanceData(2026)
	if err != nil {
		t.Fatalf("build balance data: %v", err)
	}

	if amountForBalanceLine(data.CurrentAssets, "Bank") != 140 {
		t.Fatalf("bank amount = %v, want 140", amountForBalanceLine(data.CurrentAssets, "Bank"))
	}
	if amountForBalanceLine(data.CurrentAssets, "Receivables (Debtors)") != 60 {
		t.Fatalf("receivables amount = %v, want 60", amountForBalanceLine(data.CurrentAssets, "Receivables (Debtors)"))
	}
	if data.TotalLiabilities != 30 {
		t.Fatalf("total liabilities = %v, want 30", data.TotalLiabilities)
	}
	if data.PriorTotalAssets != 560 {
		t.Fatalf("prior total assets = %v, want 560", data.PriorTotalAssets)
	}
	if data.PriorTotalLiabilities != 30 {
		t.Fatalf("prior total liabilities = %v, want 30", data.PriorTotalLiabilities)
	}
	if data.IncomeSurplus != 120 {
		t.Fatalf("income surplus = %v, want 120", data.IncomeSurplus)
	}
	if data.PriorIncomeSurplus != 0 {
		t.Fatalf("prior income surplus = %v, want 0", data.PriorIncomeSurplus)
	}
	if data.TotalEquity != 170 {
		t.Fatalf("total equity = %v, want 170", data.TotalEquity)
	}
	if data.AccumulatedFund != 50 {
		t.Fatalf("accumulated fund = %v, want 50", data.AccumulatedFund)
	}
}

func TestBuildNotesDataIncludesDirectParentTransactions(t *testing.T) {
	setupTestDB(t)

	insertTransaction(t, "2026-01-05", "income", "Offering", categoryID(t, "income", "Offering"), 30)
	insertTransaction(t, "2026-01-06", "income", "Children Service", categoryID(t, "income", "Children Service"), 70)

	data, err := buildNotesData(2026)
	if err != nil {
		t.Fatalf("build notes data: %v", err)
	}

	section := noteSectionByNumber(t, data.Notes, "1")
	if section.Total != 100 {
		t.Fatalf("note total = %v, want 100", section.Total)
	}
	if amountForNoteLine(section.Lines, "Offering") != 30 {
		t.Fatalf("offering line = %v, want 30", amountForNoteLine(section.Lines, "Offering"))
	}
	if amountForNoteLine(section.Lines, "Children Service") != 70 {
		t.Fatalf("children service line = %v, want 70", amountForNoteLine(section.Lines, "Children Service"))
	}
	if section.PriorTotal != 0 {
		t.Fatalf("prior note total = %v, want 0", section.PriorTotal)
	}
}

func TestBuildAnnualDataIncludesPriorYearComparatives(t *testing.T) {
	setupTestDB(t)

	offeringID := categoryID(t, "income", "Offering")
	stationeryID := categoryID(t, "expenditure", "Stationery")

	insertTransaction(t, "2025-02-01", "income", "Offering", offeringID, 80)
	insertTransaction(t, "2025-02-10", "expenditure", "Stationery", stationeryID, 30)
	insertTransaction(t, "2026-02-01", "income", "Offering", offeringID, 100)
	insertTransaction(t, "2026-02-10", "expenditure", "Stationery", stationeryID, 40)

	if _, err := db.DB.Exec(
		`INSERT INTO budgets (year, category_id, amount) VALUES (2026, ?, 90)`,
		offeringID,
	); err != nil {
		t.Fatalf("insert budget: %v", err)
	}

	data, err := buildAnnualData(2026)
	if err != nil {
		t.Fatalf("build annual data: %v", err)
	}

	offeringLine := annualLineByName(t, data.IncomeLines, "Offering")
	if offeringLine.PriorAmount != 80 {
		t.Fatalf("offering prior amount = %v, want 80", offeringLine.PriorAmount)
	}
	if offeringLine.Amount != 100 {
		t.Fatalf("offering current amount = %v, want 100", offeringLine.Amount)
	}
	if offeringLine.Budget != 90 {
		t.Fatalf("offering budget = %v, want 90", offeringLine.Budget)
	}
	if offeringLine.Variance != 10 {
		t.Fatalf("offering variance = %v, want 10", offeringLine.Variance)
	}

	if data.TotalPriorIncome != 80 {
		t.Fatalf("prior total income = %v, want 80", data.TotalPriorIncome)
	}
	if data.TotalPriorExpense != 30 {
		t.Fatalf("prior total expense = %v, want 30", data.TotalPriorExpense)
	}
	if data.PriorSurplus != 50 {
		t.Fatalf("prior surplus = %v, want 50", data.PriorSurplus)
	}
}

func TestBuildNotesDataGroupsComparativeExpenditureNotes(t *testing.T) {
	setupTestDB(t)

	insertTransaction(t, "2025-06-01", "expenditure", "Salaries & Allowances", categoryID(t, "expenditure", "Salaries & Allowances"), 50)
	insertTransaction(t, "2026-06-01", "expenditure", "Repairs & Maintenance", categoryID(t, "expenditure", "Repairs & Maintenance"), 40)

	data, err := buildNotesData(2026)
	if err != nil {
		t.Fatalf("build notes data: %v", err)
	}

	section := noteSectionByNumber(t, data.Notes, "10")
	if section.Total != 40 {
		t.Fatalf("note 10 total = %v, want 40", section.Total)
	}
	if section.PriorTotal != 50 {
		t.Fatalf("note 10 prior total = %v, want 50", section.PriorTotal)
	}
	if amountForNoteLine(section.Lines, "Repairs & Maintenance") != 40 {
		t.Fatalf("repairs line = %v, want 40", amountForNoteLine(section.Lines, "Repairs & Maintenance"))
	}
	if priorAmountForNoteLine(section.Lines, "Salaries & Allowances") != 50 {
		t.Fatalf("salaries prior line = %v, want 50", priorAmountForNoteLine(section.Lines, "Salaries & Allowances"))
	}
}

func TestQuarterlyReportRenders(t *testing.T) {
	setupTestDB(t)

	req := httptest.NewRequest(http.MethodGet, "/quarterly?year=2026", nil)
	recorder := httptest.NewRecorder()
	QuarterlyReport(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200, body: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "Q1 (Jan-Mar)") {
		t.Fatalf("quarterly report body did not include quarter label: %s", recorder.Body.String())
	}
}

func TestRunAutoBackupIfDueCreatesBackupAndUpdatesLastRun(t *testing.T) {
	setupTestDB(t)
	t.Setenv("RAMSEYER_FINANCE_BACKUP_DIR", t.TempDir())

	if err := saveAutoBackupConfig(true, "weekly"); err != nil {
		t.Fatalf("save auto backup config: %v", err)
	}

	now := time.Date(2026, time.May, 1, 9, 30, 0, 0, time.UTC)
	ran, path, err := RunAutoBackupIfDue(now)
	if err != nil {
		t.Fatalf("run auto backup if due: %v", err)
	}
	if !ran {
		t.Fatalf("expected auto backup to run")
	}
	if !strings.Contains(path, "ramseyer-finance-auto-weekly-") {
		t.Fatalf("backup path = %q, want weekly auto backup filename", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("auto backup file not created: %v", err)
	}

	lastRunValue, err := db.GetSetting(settingAutoBackupLastRun)
	if err != nil {
		t.Fatalf("get auto backup last run: %v", err)
	}
	if lastRunValue == "" {
		t.Fatalf("expected last run setting to be saved")
	}

	ranAgain, _, err := RunAutoBackupIfDue(now.Add(24 * time.Hour))
	if err != nil {
		t.Fatalf("second auto backup run: %v", err)
	}
	if ranAgain {
		t.Fatalf("auto backup should not run again before interval is due")
	}
}

func TestTransactionsPageRendersEditForm(t *testing.T) {
	setupTestDB(t)

	insertTransaction(t, "2026-01-15", "income", "Offering", categoryID(t, "income", "Offering"), 25)

	req := httptest.NewRequest(http.MethodGet, "/transactions?year=2026&edit=1", nil)
	recorder := httptest.NewRecorder()
	TransactionsPage(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status code = %d, want 200, body: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "Transaction Register") {
		t.Fatalf("register heading missing from body: %s", body)
	}
	if !strings.Contains(body, "Edit Transaction #1") {
		t.Fatalf("edit form missing from body: %s", body)
	}
	if !strings.Contains(body, "data-auto-open-modal=\"edit-transaction-modal\"") {
		t.Fatalf("edit modal auto-open flag missing from body: %s", body)
	}
}

func TestUpdateAndDeleteTransactionHandlers(t *testing.T) {
	setupTestDB(t)

	insertTransaction(t, "2026-01-15", "income", "Offering", categoryID(t, "income", "Offering"), 25)

	updateForm := url.Values{
		"id":          {"1"},
		"date":        {"2026-01-20"},
		"type":        {"expenditure"},
		"category_id": {strconv.FormatInt(categoryID(t, "expenditure", "Stationery"), 10)},
		"description": {"Reclassified stationery"},
		"amount":      {"55.50"},
		"return_to":   {"/transactions?year=2026"},
	}
	updateReq := httptest.NewRequest(http.MethodPost, "/api/transaction/update", strings.NewReader(updateForm.Encode()))
	updateReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	updateRecorder := httptest.NewRecorder()
	UpdateTransaction(updateRecorder, updateReq)

	if updateRecorder.Code != http.StatusSeeOther {
		t.Fatalf("update status code = %d, want 303", updateRecorder.Code)
	}

	var (
		transactionType string
		categoryName    string
		noteRef         string
		description     string
		amount          float64
	)
	if err := db.DB.QueryRow(
		`SELECT type, category, note_ref, description, amount FROM transactions WHERE id=1`,
	).Scan(&transactionType, &categoryName, &noteRef, &description, &amount); err != nil {
		t.Fatalf("query updated transaction: %v", err)
	}
	if transactionType != "expenditure" {
		t.Fatalf("updated type = %q, want expenditure", transactionType)
	}
	if categoryName != "Stationery" {
		t.Fatalf("updated category = %q, want Stationery", categoryName)
	}
	if noteRef != "11" {
		t.Fatalf("updated note_ref = %q, want 11", noteRef)
	}
	if description != "Reclassified stationery" {
		t.Fatalf("updated description = %q", description)
	}
	if amount != 55.50 {
		t.Fatalf("updated amount = %v, want 55.50", amount)
	}

	deleteForm := url.Values{
		"id":        {"1"},
		"return_to": {"/transactions?year=2026"},
	}
	deleteReq := httptest.NewRequest(http.MethodPost, "/api/transaction/delete", strings.NewReader(deleteForm.Encode()))
	deleteReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	deleteRecorder := httptest.NewRecorder()
	DeleteTransaction(deleteRecorder, deleteReq)

	if deleteRecorder.Code != http.StatusSeeOther {
		t.Fatalf("delete status code = %d, want 303", deleteRecorder.Code)
	}

	var remaining int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM transactions WHERE id=1`).Scan(&remaining); err != nil {
		t.Fatalf("count deleted transaction: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("remaining transactions = %d, want 0", remaining)
	}
}

func TestLoadTransactionRowsPaginates(t *testing.T) {
	setupTestDB(t)

	offeringID := categoryID(t, "income", "Offering")
	for index := 1; index <= 30; index++ {
		insertTransaction(
			t,
			fmt.Sprintf("2026-01-%02d", (index%28)+1),
			"income",
			"Offering",
			offeringID,
			float64(index),
		)
	}

	rows, totalCount, err := loadTransactionRows(2026, "", "", 2, 25)
	if err != nil {
		t.Fatalf("load paginated transaction rows: %v", err)
	}
	if totalCount != 30 {
		t.Fatalf("totalCount = %d, want 30", totalCount)
	}
	if len(rows) != 5 {
		t.Fatalf("page 2 row count = %d, want 5", len(rows))
	}
}

func TestExportTransactionsCSVExcelAndPDF(t *testing.T) {
	setupTestDB(t)

	insertTransaction(t, "2026-01-15", "income", "Offering", categoryID(t, "income", "Offering"), 25)

	for _, scenario := range []struct {
		format      string
		contentType string
		bodyPrefix  string
	}{
		{format: "csv", contentType: "text/csv; charset=utf-8", bodyPrefix: "ID,Date,Type"},
		{format: "excel", contentType: "application/vnd.ms-excel", bodyPrefix: "<?xml version=\"1.0\"?>"},
		{format: "pdf", contentType: "application/pdf", bodyPrefix: "%PDF-1.4"},
	} {
		req := httptest.NewRequest(
			http.MethodGet,
			fmt.Sprintf("/api/transactions/export?format=%s&start_date=2026-01-01&end_date=2026-01-31", scenario.format),
			nil,
		)
		recorder := httptest.NewRecorder()
		ExportTransactions(recorder, req)

		if recorder.Code != http.StatusOK {
			t.Fatalf("%s export status code = %d, want 200", scenario.format, recorder.Code)
		}
		if recorder.Header().Get("Content-Type") != scenario.contentType {
			t.Fatalf("%s content type = %q, want %q", scenario.format, recorder.Header().Get("Content-Type"), scenario.contentType)
		}
		if !strings.HasPrefix(recorder.Body.String(), scenario.bodyPrefix) {
			t.Fatalf("%s body prefix mismatch: %q", scenario.format, recorder.Body.String())
		}
	}
}

func amountForBalanceLine(lines []BalanceLine, name string) float64 {
	for _, line := range lines {
		if line.Name == name {
			return line.Amount
		}
	}
	return 0
}

func noteSectionByNumber(t *testing.T, sections []NoteSection, number string) NoteSection {
	t.Helper()

	for _, section := range sections {
		if section.Number == number {
			return section
		}
	}
	t.Fatalf("note section %s not found", number)
	return NoteSection{}
}

func amountForNoteLine(lines []NoteLine, name string) float64 {
	for _, line := range lines {
		if line.Name == name {
			return line.Amount
		}
	}
	return 0
}

func priorAmountForNoteLine(lines []NoteLine, name string) float64 {
	for _, line := range lines {
		if line.Name == name {
			return line.PriorAmount
		}
	}
	return 0
}

func annualLineByName(t *testing.T, lines []LineRow, name string) LineRow {
	t.Helper()

	for _, line := range lines {
		if line.Name == name {
			return line
		}
	}
	t.Fatalf("annual line %s not found", name)
	return LineRow{}
}
