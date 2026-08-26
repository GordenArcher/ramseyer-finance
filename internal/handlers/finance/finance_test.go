package finance

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"ramseyer-finance/internal/db"
	backupservice "ramseyer-finance/internal/handlers/backup"
	updatehandlers "ramseyer-finance/internal/handlers/updates"
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

	insertTransaction(t, "2026-04-10", "income", "Offerings", categoryID(t, "income", "Offerings"), 300)
	insertTransaction(t, "2026-04-12", "expenditure", "Printing & Stationery", categoryID(t, "expenditure", "Printing & Stationery"), 90)
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

	balanceData, err := buildBalanceData(2026)
	if err != nil {
		t.Fatalf("build balance data with legacy cash openings: %v", err)
	}
	if amountForBalanceLine(balanceData.CurrentAssets, "Cash & Cash Equivalents") != 215 {
		t.Fatalf("Note 26 balance = %v, want 215", amountForBalanceLine(balanceData.CurrentAssets, "Cash & Cash Equivalents"))
	}
	notesData, err := buildNotesData(2026)
	if err != nil {
		t.Fatalf("build notes with legacy cash openings: %v", err)
	}
	if noteSectionByNumber(t, notesData.Notes, "26").Total != 215 {
		t.Fatalf("Note 26 notes total = %v, want 215", noteSectionByNumber(t, notesData.Notes, "26").Total)
	}
}

func TestBuildDashboardDataIncludesDailyTotalsAndAccumulatedTransactions(t *testing.T) {
	setupTestDB(t)

	offeringID := categoryID(t, "income", "Offerings")
	stationeryID := categoryID(t, "expenditure", "Printing & Stationery")
	bankID := categoryID(t, "asset", "Bank")

	insertTransaction(t, "2026-05-01", "income", "Offerings", offeringID, 150)
	insertTransaction(t, "2026-05-01", "expenditure", "Printing & Stationery", stationeryID, 40)
	insertTransaction(t, "2026-05-01", "asset", "Bank", bankID, 20)
	insertTransaction(t, "2026-04-30", "income", "Offerings", offeringID, 999)

	data, err := buildDashboardData(
		time.Date(2026, time.May, 1, 14, 0, 0, 0, time.UTC),
		"dashboard",
		"",
		"",
	)
	if err != nil {
		t.Fatalf("build dashboard data: %v", err)
	}

	if data.DailyIncome != 150 {
		t.Fatalf("daily income = %v, want 150", data.DailyIncome)
	}
	if data.DailyExpense != 40 {
		t.Fatalf("daily expense = %v, want 40", data.DailyExpense)
	}
	if data.DailyAsset != 20 {
		t.Fatalf("daily asset = %v, want 20", data.DailyAsset)
	}
	if data.DailyLiability != 0 {
		t.Fatalf("daily liability = %v, want 0", data.DailyLiability)
	}
	if data.DailyNetIncome != 110 {
		t.Fatalf("daily net income = %v, want 110", data.DailyNetIncome)
	}
	if data.DailyPostedCount != 3 {
		t.Fatalf("daily posted count = %d, want 3", data.DailyPostedCount)
	}
	if len(data.DailyTransactions) != 3 {
		t.Fatalf("daily transactions len = %d, want 3", len(data.DailyTransactions))
	}
	if data.DailyTransactions[0].Amount != 150 || data.DailyTransactions[1].Amount != 40 || data.DailyTransactions[2].Amount != 20 {
		t.Fatalf("daily transactions were not accumulated in same-day posting order: %#v", data.DailyTransactions)
	}
	if data.CurrentDayLabel != "Friday, 01 May 2026" {
		t.Fatalf("current day label = %q, want %q", data.CurrentDayLabel, "Friday, 01 May 2026")
	}
}

func TestBuildBalanceDataUsesYearEndLogic(t *testing.T) {
	setupTestDB(t)

	if _, err := db.DB.Exec(
		`INSERT INTO account_opening_balances (year, category_id, amount) VALUES
		(2026, ?, 100),
		(2026, ?, 60),
		(2026, ?, 30)`,
		categoryID(t, "asset", "Cash & Cash Equivalents"),
		categoryID(t, "asset", "Accounts Receivable & Prepayments"),
		categoryID(t, "liability", "Accounts Payable & Accruals"),
	); err != nil {
		t.Fatalf("insert account opening balances: %v", err)
	}
	if _, err := db.DB.Exec(
		`INSERT INTO fund_rollforwards (year, opening_balance, prior_year_adjustment) VALUES (2026, 50, 0)`,
	); err != nil {
		t.Fatalf("insert accumulated fund opening: %v", err)
	}

	insertTransaction(t, "2026-02-01", "asset", "Bank", categoryID(t, "asset", "Bank"), 40)
	insertTransaction(t, "2026-03-15", "income", "Tithes", categoryID(t, "income", "Tithes"), 200)
	insertTransaction(t, "2026-03-20", "expenditure", "Printing & Stationery", categoryID(t, "expenditure", "Printing & Stationery"), 80)

	data, err := buildBalanceData(2026)
	if err != nil {
		t.Fatalf("build balance data: %v", err)
	}

	if amountForBalanceLine(data.CurrentAssets, "Cash & Cash Equivalents") != 140 {
		t.Fatalf("cash amount = %v, want 140", amountForBalanceLine(data.CurrentAssets, "Cash & Cash Equivalents"))
	}
	if amountForBalanceLine(data.CurrentAssets, "Accounts Receivable & Prepayments") != 60 {
		t.Fatalf("receivables amount = %v, want 60", amountForBalanceLine(data.CurrentAssets, "Accounts Receivable & Prepayments"))
	}
	if data.TotalLiabilities != 30 {
		t.Fatalf("total liabilities = %v, want 30", data.TotalLiabilities)
	}
	if data.PriorTotalAssets != 0 {
		t.Fatalf("prior total assets = %v, want 0", data.PriorTotalAssets)
	}
	if data.PriorTotalLiabilities != 0 {
		t.Fatalf("prior total liabilities = %v, want 0", data.PriorTotalLiabilities)
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
	if data.BalanceDifference != 0 {
		t.Fatalf("balance difference = %v, want 0", data.BalanceDifference)
	}
}

func TestBuildNotesDataIncludesDirectParentTransactions(t *testing.T) {
	setupTestDB(t)

	insertTransaction(t, "2026-01-05", "income", "Offerings", categoryID(t, "income", "Offerings"), 30)
	insertTransaction(t, "2026-01-06", "income", "Children's Service Offerings", categoryID(t, "income", "Children's Service Offerings"), 70)

	data, err := buildNotesData(2026)
	if err != nil {
		t.Fatalf("build notes data: %v", err)
	}

	section := noteSectionByNumber(t, data.Notes, "4")
	if section.Total != 100 {
		t.Fatalf("note total = %v, want 100", section.Total)
	}
	if amountForNoteLine(section.Lines, "Offerings") != 30 {
		t.Fatalf("offering line = %v, want 30", amountForNoteLine(section.Lines, "Offerings"))
	}
	if amountForNoteLine(section.Lines, "Children's Service Offerings") != 70 {
		t.Fatalf("children service line = %v, want 70", amountForNoteLine(section.Lines, "Children's Service Offerings"))
	}
	if section.PriorTotal != 0 {
		t.Fatalf("prior note total = %v, want 0", section.PriorTotal)
	}
}

func TestBuildAnnualDataIncludesPriorYearComparatives(t *testing.T) {
	setupTestDB(t)

	offeringID := categoryID(t, "income", "Offerings")
	stationeryID := categoryID(t, "expenditure", "Printing & Stationery")

	insertTransaction(t, "2025-02-01", "income", "Offerings", offeringID, 80)
	insertTransaction(t, "2025-02-10", "expenditure", "Printing & Stationery", stationeryID, 30)
	insertTransaction(t, "2026-02-01", "income", "Offerings", offeringID, 100)
	insertTransaction(t, "2026-02-10", "expenditure", "Printing & Stationery", stationeryID, 40)

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

	offeringLine := annualLineByName(t, data.IncomeLines, "Offerings")
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

func TestSaveCategoryAndArchiveFlow(t *testing.T) {
	setupTestDB(t)

	parentID := categoryID(t, "income", "Offerings")

	form := url.Values{
		"type":      {"income"},
		"name":      {"Special Sunday"},
		"parent_id": {strconv.FormatInt(parentID, 10)},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/category/save", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()

	SaveCategory(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("save category status = %d, want %d", recorder.Code, http.StatusSeeOther)
	}

	var (
		newCategoryID int64
		noteRef       string
		isActive      int
	)
	if err := db.DB.QueryRow(
		`SELECT id, note_ref, is_active FROM categories WHERE type = 'income' AND name = 'Special Sunday'`,
	).Scan(&newCategoryID, &noteRef, &isActive); err != nil {
		t.Fatalf("lookup saved category: %v", err)
	}
	if noteRef != "4" {
		t.Fatalf("child note ref = %q, want 4", noteRef)
	}
	if isActive != 1 {
		t.Fatalf("new category active flag = %d, want 1", isActive)
	}

	dashboardData, err := buildDashboardData(time.Date(2026, time.January, 3, 0, 0, 0, 0, time.UTC), "dashboard", "", "")
	if err != nil {
		t.Fatalf("build dashboard data after category create: %v", err)
	}
	if !containsCategoryOption(dashboardData.IncomeCats, "Special Sunday") {
		t.Fatalf("new category was not available in active income choices")
	}

	archiveForm := url.Values{"id": {strconv.FormatInt(newCategoryID, 10)}}
	archiveRequest := httptest.NewRequest(http.MethodPost, "/api/category/toggle", strings.NewReader(archiveForm.Encode()))
	archiveRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	archiveRecorder := httptest.NewRecorder()

	ToggleCategoryStatus(archiveRecorder, archiveRequest)
	if archiveRecorder.Code != http.StatusSeeOther {
		t.Fatalf("archive category status = %d, want %d", archiveRecorder.Code, http.StatusSeeOther)
	}

	dashboardData, err = buildDashboardData(time.Date(2026, time.January, 3, 0, 0, 0, 0, time.UTC), "dashboard", "", "")
	if err != nil {
		t.Fatalf("build dashboard data after archive: %v", err)
	}
	if containsCategoryOption(dashboardData.IncomeCats, "Special Sunday") {
		t.Fatalf("archived category still appeared in active income choices")
	}
}

func TestBuildBalanceDataIncludesDynamicTopLevelAsset(t *testing.T) {
	setupTestDB(t)

	result, err := db.DB.Exec(`
		INSERT INTO categories (type, name, parent_id, note_ref, report_section, is_active)
		VALUES ('asset', 'Inventory', 0, '', 'current_asset', 1)
	`)
	if err != nil {
		t.Fatalf("insert dynamic asset category: %v", err)
	}
	inventoryID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("inventory category id: %v", err)
	}

	insertTransaction(t, "2026-06-10", "asset", "Inventory", inventoryID, 55)

	data, err := buildBalanceData(2026)
	if err != nil {
		t.Fatalf("build balance data with dynamic asset: %v", err)
	}

	if amountForBalanceLine(data.CurrentAssets, "Inventory") != 55 {
		t.Fatalf("inventory balance = %v, want 55", amountForBalanceLine(data.CurrentAssets, "Inventory"))
	}
	if data.TotalAssets != 55 {
		t.Fatalf("total assets = %v, want 55", data.TotalAssets)
	}
}

func TestArchiveUsedCategoryShowsSpecificMessage(t *testing.T) {
	setupTestDB(t)

	categoryID := categoryID(t, "income", "Offerings")
	insertTransaction(t, "2026-02-10", "income", "Offerings", categoryID, 125)

	form := url.Values{
		"id":        {strconv.FormatInt(categoryID, 10)},
		"return_to": {"/categories?page=1"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/category/toggle", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()

	ToggleCategoryStatus(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("archive used category status = %d, want %d", recorder.Code, http.StatusSeeOther)
	}

	location := recorder.Header().Get("Location")
	if !strings.Contains(location, "already+in+use+by+1+transaction") {
		t.Fatalf("archive used category redirect = %q, want specific usage message", location)
	}

	var isActive int
	if err := db.DB.QueryRow(`SELECT is_active FROM categories WHERE id = ?`, categoryID).Scan(&isActive); err != nil {
		t.Fatalf("load archived flag after blocked archive: %v", err)
	}
	if isActive != 1 {
		t.Fatalf("used category active flag = %d, want 1", isActive)
	}
}

func TestDashboardGreetingRotationUsesConfiguredNameAndNonRepeatingCycle(t *testing.T) {
	setupTestDB(t)

	if err := db.SetSetting(dashboardDisplayNameSettingKey, "Basaa"); err != nil {
		t.Fatalf("set display name: %v", err)
	}
	if err := db.SetSetting(dashboardGreetingTimeSettingKey, "10:00"); err != nil {
		t.Fatalf("set greeting rotation time: %v", err)
	}

	beforeCutoff, err := buildDashboardData(
		time.Date(2026, time.January, 10, 9, 0, 0, 0, time.UTC),
		"dashboard",
		"",
		"",
	)
	if err != nil {
		t.Fatalf("build dashboard data before cutoff: %v", err)
	}
	if beforeCutoff.GreetingIntro != "Good morning, Basaa." {
		t.Fatalf("greeting intro = %q, want %q", beforeCutoff.GreetingIntro, "Good morning, Basaa.")
	}
	if beforeCutoff.GreetingTimeLabel != "10:00 AM" {
		t.Fatalf("greeting time label = %q, want 10:00 AM", beforeCutoff.GreetingTimeLabel)
	}

	sameSlot, err := buildDashboardData(
		time.Date(2026, time.January, 10, 9, 45, 0, 0, time.UTC),
		"dashboard",
		"",
		"",
	)
	if err != nil {
		t.Fatalf("build dashboard data in same slot: %v", err)
	}
	if sameSlot.GreetingMessage != beforeCutoff.GreetingMessage {
		t.Fatalf("same-slot greeting changed from %q to %q", beforeCutoff.GreetingMessage, sameSlot.GreetingMessage)
	}

	afterCutoff, err := buildDashboardData(
		time.Date(2026, time.January, 10, 10, 5, 0, 0, time.UTC),
		"dashboard",
		"",
		"",
	)
	if err != nil {
		t.Fatalf("build dashboard data after cutoff: %v", err)
	}
	if afterCutoff.GreetingMessage == beforeCutoff.GreetingMessage {
		t.Fatalf("greeting did not advance after cutoff")
	}

	if err := db.SetSetting(dashboardGreetingStateSettingKey, ""); err != nil {
		t.Fatalf("reset dashboard greeting state: %v", err)
	}

	seen := map[string]struct{}{}
	for day := 0; day < 200; day++ {
		data, err := buildDashboardData(
			time.Date(2026, time.January, 1+day, 11, 0, 0, 0, time.UTC),
			"dashboard",
			"",
			"",
		)
		if err != nil {
			t.Fatalf("build dashboard data for cycle day %d: %v", day, err)
		}
		seen[data.GreetingMessage] = struct{}{}
	}

	if len(seen) != 200 {
		t.Fatalf("unique greetings seen = %d, want 200", len(seen))
	}
}

func TestDashboardGreetingCanBeDisabledAndHidesSetupHintWhenNamed(t *testing.T) {
	setupTestDB(t)

	if err := db.SetSetting(dashboardDisplayNameSettingKey, "Basaa"); err != nil {
		t.Fatalf("set display name: %v", err)
	}

	enabledData, err := buildDashboardData(
		time.Date(2026, time.January, 10, 18, 0, 0, 0, time.UTC),
		"dashboard",
		"",
		"",
	)
	if err != nil {
		t.Fatalf("build dashboard data with greeting enabled: %v", err)
	}
	if enabledData.ShowSetupHint {
		t.Fatalf("setup hint should be hidden when a name is configured")
	}
	if enabledData.GreetingIntro != "Good evening, Basaa." {
		t.Fatalf("greeting intro = %q, want %q", enabledData.GreetingIntro, "Good evening, Basaa.")
	}

	if err := db.SetSetting(dashboardGreetingEnabledSettingKey, "0"); err != nil {
		t.Fatalf("disable dashboard greeting: %v", err)
	}

	disabledData, err := buildDashboardData(
		time.Date(2026, time.January, 10, 18, 0, 0, 0, time.UTC),
		"dashboard",
		"",
		"",
	)
	if err != nil {
		t.Fatalf("build dashboard data with greeting disabled: %v", err)
	}
	if disabledData.GreetingMessage != "" {
		t.Fatalf("disabled greeting message = %q, want empty", disabledData.GreetingMessage)
	}
	if disabledData.GreetingIntro != "" {
		t.Fatalf("disabled greeting intro = %q, want empty", disabledData.GreetingIntro)
	}
	if disabledData.ShowSetupHint {
		t.Fatalf("setup hint should be hidden when the greeting is disabled")
	}
}

func TestBuildNotesDataGroupsComparativeExpenditureNotes(t *testing.T) {
	setupTestDB(t)

	insertTransaction(t, "2025-06-01", "expenditure", "Ministers' Duty Allowance", categoryID(t, "expenditure", "Ministers' Duty Allowance"), 50)
	insertTransaction(t, "2026-06-01", "expenditure", "Fuel Allowance", categoryID(t, "expenditure", "Fuel Allowance"), 40)

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
	if amountForNoteLine(section.Lines, "Fuel Allowance") != 40 {
		t.Fatalf("fuel allowance line = %v, want 40", amountForNoteLine(section.Lines, "Fuel Allowance"))
	}
	if priorAmountForNoteLine(section.Lines, "Ministers' Duty Allowance") != 50 {
		t.Fatalf("minister allowance prior line = %v, want 50", priorAmountForNoteLine(section.Lines, "Ministers' Duty Allowance"))
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

func TestWorkbookStatementPagesRender(t *testing.T) {
	setupTestDB(t)

	statements := []struct {
		name    string
		path    string
		handler http.HandlerFunc
		heading string
	}{
		{name: "financial performance", path: "/annual?year=2026", handler: AnnualReport, heading: "Statement of Financial Performance"},
		{name: "financial position", path: "/balance-sheet?year=2026", handler: BalanceSheet, heading: "Statement of Financial Position"},
		{name: "trial balance", path: "/trial-balance?year=2026", handler: TrialBalance, heading: "Trial Balance"},
		{name: "cash flow", path: "/cash-flow?year=2026", handler: CashFlowStatement, heading: "Statement of Cash Flows"},
		{name: "fixed assets", path: "/fixed-assets?year=2026", handler: FixedAssetSchedule, heading: "Non-Current Assets Schedule"},
		{name: "notes", path: "/notes?year=2026", handler: NotesPage, heading: "Notes to the Financial Statements"},
	}

	for _, statement := range statements {
		t.Run(statement.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			statement.handler(recorder, httptest.NewRequest(http.MethodGet, statement.path, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200, body: %s", recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), statement.heading) {
				t.Fatalf("heading %q missing from body", statement.heading)
			}
		})
	}
}

func TestFixedAssetScheduleUsesWorkbookRatesAndOpenings(t *testing.T) {
	setupTestDB(t)

	buildingID := categoryID(t, "asset", "Buildings - Chapel")
	if _, err := db.DB.Exec(`
		INSERT INTO fixed_asset_openings
			(year, category_id, opening_cost, opening_accumulated_depreciation)
		VALUES (2026, ?, 1000, 100)
	`, buildingID); err != nil {
		t.Fatalf("insert fixed-asset opening: %v", err)
	}
	insertTransaction(t, "2026-02-01", "asset", "Buildings - Chapel", buildingID, 500)
	insertTransaction(t, "2026-02-01", "asset", "Bank", categoryID(t, "asset", "Bank"), -500)
	if _, err := db.DB.Exec(`
		INSERT INTO fund_rollforwards (year, opening_balance, prior_year_adjustment)
		VALUES (2026, 900, 0)
	`); err != nil {
		t.Fatalf("insert fixed-asset fund opening: %v", err)
	}

	data, err := buildFixedAssetData(2026)
	if err != nil {
		t.Fatalf("build fixed-asset schedule: %v", err)
	}
	var building FixedAssetLine
	for _, line := range data.Lines {
		if line.Name == "Buildings - Chapel" {
			building = line
			break
		}
	}
	if building.Rate != 0.02 || building.OpeningCost != 1000 || building.Additions != 500 {
		t.Fatalf("building schedule inputs = %#v", building)
	}
	if building.Charge != 30 || building.ClosingAccumulatedDep != 130 || building.CarryingAmount != 1370 {
		t.Fatalf("building schedule results = %#v", building)
	}
	trialBalance, err := buildTrialBalanceData(2026)
	if err != nil {
		t.Fatalf("build fixed-asset trial balance: %v", err)
	}
	if trialBalance.Difference != 0 {
		t.Fatalf("fixed-asset trial-balance difference = %v, want 0", trialBalance.Difference)
	}
}

func TestCashFlowUsesSelectedYearOpeningsAndReconciles(t *testing.T) {
	setupTestDB(t)

	if _, err := db.DB.Exec(`
		INSERT INTO account_opening_balances (year, category_id, amount) VALUES
			(2026, ?, 100),
			(2026, ?, 20),
			(2026, ?, 30),
			(2026, ?, 10)
	`,
		categoryID(t, "asset", "Cash & Cash Equivalents"),
		categoryID(t, "asset", "Inventories"),
		categoryID(t, "asset", "Accounts Receivable & Prepayments"),
		categoryID(t, "liability", "Accounts Payable & Accruals"),
	); err != nil {
		t.Fatalf("insert cash-flow openings: %v", err)
	}
	if _, err := db.DB.Exec(`
		INSERT INTO fund_rollforwards (year, opening_balance, prior_year_adjustment)
		VALUES (2026, 140, 0)
	`); err != nil {
		t.Fatalf("insert fund opening: %v", err)
	}

	insertTransaction(t, "2026-03-01", "income", "Offerings", categoryID(t, "income", "Offerings"), 80)
	insertTransaction(t, "2026-03-02", "expenditure", "Printing & Stationery", categoryID(t, "expenditure", "Printing & Stationery"), 20)
	insertTransaction(t, "2026-03-03", "asset", "Stationery", categoryID(t, "asset", "Stationery"), 5)
	insertTransaction(t, "2026-03-04", "asset", "Other Receivables", categoryID(t, "asset", "Other Receivables"), -10)
	insertTransaction(t, "2026-03-05", "liability", "Other Payables", categoryID(t, "liability", "Other Payables"), 5)
	insertTransaction(t, "2026-03-06", "asset", "Bank", categoryID(t, "asset", "Bank"), 70)

	cashFlow, err := buildCashFlowData(2026)
	if err != nil {
		t.Fatalf("build cash flow: %v", err)
	}
	if cashFlow.OpeningCash != 100 || cashFlow.NetOperatingCash != 70 || cashFlow.ReportedClosingCash != 170 {
		t.Fatalf("cash-flow reconciliation inputs = %#v", cashFlow)
	}
	if cashFlow.ReconciliationDifference != 0 {
		t.Fatalf("cash-flow difference = %v, want 0", cashFlow.ReconciliationDifference)
	}

	trialBalance, err := buildTrialBalanceData(2026)
	if err != nil {
		t.Fatalf("build trial balance: %v", err)
	}
	if trialBalance.Difference != 0 {
		t.Fatalf("trial-balance difference = %v, want 0", trialBalance.Difference)
	}
}

func TestRunAutoBackupIfDueCreatesBackupAndUpdatesLastRun(t *testing.T) {
	setupTestDB(t)
	t.Setenv("RAMSEYER_FINANCE_BACKUP_DIR", t.TempDir())

	if err := backupservice.SaveAutoBackupConfig(true, "weekly"); err != nil {
		t.Fatalf("save auto backup config: %v", err)
	}

	now := time.Date(2026, time.May, 1, 9, 30, 0, 0, time.UTC)
	ran, path, err := backupservice.RunAutoBackupIfDue(now)
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

	lastRunValue, err := db.GetSetting(backupservice.SettingAutoBackupLastRun)
	if err != nil {
		t.Fatalf("get auto backup last run: %v", err)
	}
	if lastRunValue == "" {
		t.Fatalf("expected last run setting to be saved")
	}

	ranAgain, _, err := backupservice.RunAutoBackupIfDue(now.Add(24 * time.Hour))
	if err != nil {
		t.Fatalf("second auto backup run: %v", err)
	}
	if ranAgain {
		t.Fatalf("auto backup should not run again before interval is due")
	}
}

func TestBackupPageUsesSearchableInAppRestoreLibrary(t *testing.T) {
	setupTestDB(t)

	backupRoot := t.TempDir()
	t.Setenv("RAMSEYER_FINANCE_BACKUP_DIR", backupRoot)
	autoDir := filepath.Join(backupRoot, "Ramseyer Finance Backups", "Auto")
	safetyDir := filepath.Join(backupRoot, "Ramseyer Finance Backups", "Safety")
	if err := os.MkdirAll(autoDir, 0o755); err != nil {
		t.Fatalf("create auto backup test directory: %v", err)
	}
	if err := os.MkdirAll(safetyDir, 0o755); err != nil {
		t.Fatalf("create safety backup test directory: %v", err)
	}

	files := []struct {
		path    string
		content string
	}{
		{path: filepath.Join(backupRoot, "ramseyer-finance-backup-2026-08-01.db"), content: "manual"},
		{path: filepath.Join(autoDir, "ramseyer-finance-auto-weekly-2026-08-02-090000.sqlite"), content: "automatic"},
		{path: filepath.Join(safetyDir, "ramseyer-finance-pre-restore-2026-08-03-090000.sqlite3"), content: "safety"},
		// Unrelated database files in a broad Downloads-style root must not be presented as
		// restore points merely because they share SQLite's extension.
		{path: filepath.Join(backupRoot, "unrelated-application.db"), content: "unrelated"},
	}
	for _, file := range files {
		if err := os.WriteFile(file.path, []byte(file.content), 0o600); err != nil {
			t.Fatalf("write backup fixture %s: %v", file.path, err)
		}
	}

	candidates, err := backupservice.LoadRestoreCandidates()
	if err != nil {
		t.Fatalf("load restore candidates: %v", err)
	}
	if len(candidates) != 3 {
		t.Fatalf("restore candidate count = %d, want 3: %#v", len(candidates), candidates)
	}
	kinds := map[string]bool{}
	for _, candidate := range candidates {
		kinds[candidate.Kind] = true
		if candidate.Name == "unrelated-application.db" {
			t.Fatalf("unrelated root database was exposed in restore library")
		}
	}
	for _, expectedKind := range []string{"manual", "automatic", "safety"} {
		if !kinds[expectedKind] {
			t.Fatalf("restore library missing %s candidate: %#v", expectedKind, candidates)
		}
	}

	recorder := httptest.NewRecorder()
	backupservice.BackupPage(recorder, httptest.NewRequest(http.MethodGet, "/backup", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("backup page status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, marker := range []string{
		"/static/styles/custom-select.css",
		"/static/scripts/custom-select.js",
		"data-backup-search",
		"data-custom-selector",
		"data-custom-selector-search",
		"data-backup-kind-filter",
		"data-backup-age-filter",
		"data-backup-extension-filter",
		"data-backup-sort",
		"data-backup-drop-zone",
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("custom restore control %q missing from backup page", marker)
		}
	}
	if strings.Contains(body, "data-native-file-picker") || strings.Contains(body, "Choose Backup File") {
		t.Fatalf("native restore picker remained in backup page")
	}
	for _, nativeFilter := range []string{
		`<select id="backup-kind-filter"`,
		`<select id="backup-age-filter"`,
		`<select id="backup-extension-filter"`,
		`<select id="backup-sort"`,
	} {
		if strings.Contains(body, nativeFilter) {
			t.Fatalf("native backup filter %q remained in backup page", nativeFilter)
		}
	}
}

func TestManagedRestorePathRejectsArbitraryFilesAndAllowsHistory(t *testing.T) {
	setupTestDB(t)

	backupRoot := t.TempDir()
	t.Setenv("RAMSEYER_FINANCE_BACKUP_DIR", backupRoot)
	manualPath := filepath.Join(backupRoot, "ramseyer-finance-backup-2026-08-10.db")
	if err := os.WriteFile(manualPath, []byte("managed"), 0o600); err != nil {
		t.Fatalf("write managed backup: %v", err)
	}
	validatedPath, err := backupservice.ValidateManagedRestorePath(manualPath)
	if err != nil {
		t.Fatalf("validate managed backup: %v", err)
	}
	canonicalManualPath, err := filepath.EvalSymlinks(manualPath)
	if err != nil {
		t.Fatalf("resolve managed backup fixture: %v", err)
	}
	if validatedPath != canonicalManualPath {
		t.Fatalf("validated path = %q, want %q", validatedPath, canonicalManualPath)
	}

	externalDir := t.TempDir()
	externalPath := filepath.Join(externalDir, "external.sqlite")
	if err := os.WriteFile(externalPath, []byte("external"), 0o600); err != nil {
		t.Fatalf("write external backup: %v", err)
	}
	if _, err := backupservice.ValidateManagedRestorePath(externalPath); err == nil {
		t.Fatalf("arbitrary external path was accepted without history")
	}
	if err := backupservice.RecordBackupEvent("restore", "success", externalPath, "Previously restored backup"); err != nil {
		t.Fatalf("record successful restore history: %v", err)
	}
	if _, err := backupservice.ValidateManagedRestorePath(externalPath); err != nil {
		t.Fatalf("validate history-backed external path: %v", err)
	}
}

func TestFormatReleasePublishedAtUsesHumanReadableLabel(t *testing.T) {
	formatted := updatehandlers.FormatReleasePublishedAt("2026-05-03T10:15:00Z")
	if formatted != "3rd May, 2026 at 10:15 AM UTC" {
		t.Fatalf("formatted release label = %q, want %q", formatted, "3rd May, 2026 at 10:15 AM UTC")
	}
}

func TestTransactionsPageRendersEditForm(t *testing.T) {
	setupTestDB(t)

	insertTransaction(t, "2026-01-15", "income", "Offerings", categoryID(t, "income", "Offerings"), 25)

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

	insertTransaction(t, "2026-01-15", "income", "Offerings", categoryID(t, "income", "Offerings"), 25)

	updateForm := url.Values{
		"id":          {"1"},
		"date":        {"2026-01-20"},
		"type":        {"expenditure"},
		"category_id": {strconv.FormatInt(categoryID(t, "expenditure", "Printing & Stationery"), 10)},
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
	if categoryName != "Printing & Stationery" {
		t.Fatalf("updated category = %q, want Printing & Stationery", categoryName)
	}
	if noteRef != "20" {
		t.Fatalf("updated note_ref = %q, want 20", noteRef)
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

	offeringID := categoryID(t, "income", "Offerings")
	for index := 1; index <= 30; index++ {
		insertTransaction(
			t,
			fmt.Sprintf("2026-01-%02d", (index%28)+1),
			"income",
			"Offerings",
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

	insertTransaction(t, "2026-01-15", "income", "Offerings", categoryID(t, "income", "Offerings"), 25)

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

func TestTrialBalanceEditsDriveNotesAndRemainYearSpecific(t *testing.T) {
	setupTestDB(t)
	offeringsID := categoryID(t, "income", "Offerings")
	insertTransaction(t, "2026-02-01", "income", "Offerings", offeringsID, 100)
	insertTransaction(t, "2027-02-01", "income", "Offerings", offeringsID, 125)

	if _, err := buildTrialBalanceData(2026); err != nil {
		t.Fatalf("initialize 2026 Trial Balance: %v", err)
	}
	if _, err := db.DB.Exec(`
		UPDATE trial_balance_entries
		SET credit = 450, debit = 0
		WHERE year = 2026 AND account_type = 'income' AND account_name = 'Offerings'
	`); err != nil {
		t.Fatalf("edit authoritative Trial Balance row: %v", err)
	}

	notes, err := buildNotesData(2026)
	if err != nil {
		t.Fatalf("build notes from edited Trial Balance: %v", err)
	}
	if amountForNoteLine(noteSectionByNumber(t, notes.Notes, "4").Lines, "Offerings") != 450 {
		t.Fatalf("Note 4 did not receive the edited 2026 Trial Balance amount")
	}
	annual, err := buildAnnualData(2026)
	if err != nil {
		t.Fatalf("build annual statement from edited Trial Balance: %v", err)
	}
	if amountForAnnualLine(annual.IncomeLines, "4") != 450 {
		t.Fatalf("annual Note 4 amount = %v, want 450", amountForAnnualLine(annual.IncomeLines, "4"))
	}

	if _, err := buildTrialBalanceData(2027); err != nil {
		t.Fatalf("initialize independent 2027 Trial Balance: %v", err)
	}
	var credit2027 float64
	if err := db.DB.QueryRow(`
		SELECT credit FROM trial_balance_entries
		WHERE year = 2027 AND account_type = 'income' AND account_name = 'Offerings'
	`).Scan(&credit2027); err != nil {
		t.Fatalf("load 2027 offering row: %v", err)
	}
	if credit2027 != 125 {
		t.Fatalf("2027 offering = %v, want its independent 125 instead of the edited 2026 value", credit2027)
	}
}

func TestExportReportPDFReturnsARealDocument(t *testing.T) {
	setupTestDB(t)
	insertTransaction(t, "2026-02-01", "income", "Offerings", categoryID(t, "income", "Offerings"), 100)

	for _, report := range []string{"trial-balance", "annual", "balance-sheet", "cash-flow", "fixed-assets", "notes"} {
		t.Run(report, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/report/pdf?report="+report+"&year=2026", nil)
			recorder := httptest.NewRecorder()
			ExportReportPDF(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("report PDF status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if contentType := recorder.Header().Get("Content-Type"); contentType != "application/pdf" {
				t.Fatalf("report PDF content type = %q", contentType)
			}
			if !strings.HasPrefix(recorder.Body.String(), "%PDF-1.4") {
				t.Fatalf("report response is not a PDF document")
			}
		})
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

func amountForAnnualLine(lines []LineRow, note string) float64 {
	for _, line := range lines {
		if line.Note == note {
			return line.Amount
		}
	}
	return 0
}

func containsCategoryOption(options []CatOption, name string) bool {
	for _, option := range options {
		if option.Name == name {
			return true
		}
	}
	return false
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
