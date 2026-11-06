package finance

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"ramseyer-finance/internal/db"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func imprestCategoryByName(t *testing.T, name string) ImprestCategory {
	t.Helper()

	categories, err := loadImprestCategories()
	if err != nil {
		t.Fatalf("load imprest categories: %v", err)
	}
	for _, category := range categories {
		if category.Name == name {
			return category
		}
	}
	t.Fatalf("imprest category %q not found", name)
	return ImprestCategory{}
}

func insertImprestFixture(t *testing.T, date, category, description string, amount float64) {
	t.Helper()

	if err := insertImprestExpense(date, imprestCategoryByName(t, category), description, amount); err != nil {
		t.Fatalf("insert imprest fixture: %v", err)
	}
}

func postImprestForm(t *testing.T, path string, form url.Values, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	handler(recorder, request)
	return recorder
}

func TestImprestCategoriesSeedWithTheCashBookHeads(t *testing.T) {
	setupTestDB(t)

	categories, err := loadImprestCategories()
	if err != nil {
		t.Fatalf("load imprest categories: %v", err)
	}
	expectedHeads := []string{
		"T&T", "REFRESHMENT", "CLEANING", "REPAIRS & MTCE", "STATIONERY",
		"OTHERS", "FUEL", "WELFARE", "SSNIT", "DATA", "SECURITY",
	}
	if len(categories) != len(expectedHeads) {
		t.Fatalf("category count = %d, want %d", len(categories), len(expectedHeads))
	}
	for _, expected := range expectedHeads {
		if _, matched := matchImprestCategory(categories, expected); !matched {
			t.Fatalf("default imprest head %q was not seeded", expected)
		}
	}
	// The imprest heads are deliberately separate from the expenditure chart. If a
	// short cash-book name appeared there, the two books would start agreeing by accident.
	if _, err := db.DB.Exec(
		`INSERT INTO categories (type, name, parent_id, note_ref, report_section, is_active)
		VALUES ('expenditure', 'T&T', 0, '', '', 1)`,
	); err != nil {
		t.Fatalf("insert accidental expenditure category: %v", err)
	}
	var linked int
	if err := db.DB.QueryRow(
		`SELECT COUNT(*) FROM categories WHERE type = 'expenditure' AND name IN ('T&T', 'REFRESHMENT', 'DETAILS')`,
	).Scan(&linked); err != nil {
		t.Fatalf("count expenditure matches: %v", err)
	}
	// The test inserted one row itself, so only that row should be present.
	if linked != 1 {
		t.Fatalf("expenditure categories matching imprest heads = %d, want the single test row", linked)
	}
}

func TestReportYearsIncludeAnImprestOnlyYear(t *testing.T) {
	setupTestDB(t)

	insertImprestFixture(t, "2020-06-01", "T&T", "Old imprest payment", 25)

	// The selected year's window is 2024 to 2028, so 2020 can only appear when the known
	// years query reads the imprest table as well as the main register.
	years, err := reportYears(2026)
	if err != nil {
		t.Fatalf("load report years: %v", err)
	}
	for _, year := range years {
		if year == 2020 {
			return
		}
	}
	t.Fatalf("report years = %v, want the imprest-only 2020 year included", years)
}

func TestMatchImprestCategoryIsExactButCaseInsensitive(t *testing.T) {
	setupTestDB(t)

	categories, err := loadImprestCategories()
	if err != nil {
		t.Fatalf("load imprest categories: %v", err)
	}
	if category, matched := matchImprestCategory(categories, "  t&t  "); !matched || category.Name != "T&T" {
		t.Fatalf("lowercase padded T&T matched %+v, want the T&T head", category)
	}
	if _, matched := matchImprestCategory(categories, "TT"); matched {
		t.Fatalf("TT matched a head; a misspelling must be reported, not guessed")
	}
	if _, matched := matchImprestCategory(categories, ""); matched {
		t.Fatalf("an empty head matched a category")
	}
}

func TestAddImprestExpenseSavesAValidPayment(t *testing.T) {
	setupTestDB(t)

	category := imprestCategoryByName(t, "T&T")
	form := url.Values{
		"date":        {"2026-03-04"},
		"category_id": {strconv.FormatInt(category.ID, 10)},
		"description": {"TNT to Pay tier 2 and documentation"},
		"amount":      {"35.50"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/imprest/add", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()

	AddImprestExpense(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", recorder.Code)
	}
	if location := recorder.Header().Get("Location"); strings.Contains(location, "tone=warning") {
		t.Fatalf("valid save redirected as a warning: %q", location)
	}

	var (
		date         string
		categoryName string
		description  string
		amount       float64
	)
	if err := db.DB.QueryRow(
		`SELECT date, category, description, amount FROM imprest_expenses`,
	).Scan(&date, &categoryName, &description, &amount); err != nil {
		t.Fatalf("load saved imprest payment: %v", err)
	}
	if date != "2026-03-04" || categoryName != "T&T" || amount != 35.50 {
		t.Fatalf("saved imprest payment = %s/%s/%.2f", date, categoryName, amount)
	}
	if description != "TNT to Pay tier 2 and documentation" {
		t.Fatalf("saved description = %q", description)
	}

	// An imprest payment must not leak into the main register.
	var ledgerRows int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM transactions`).Scan(&ledgerRows); err != nil {
		t.Fatalf("count main transactions: %v", err)
	}
	if ledgerRows != 0 {
		t.Fatalf("main transaction count = %d, want 0 after an imprest save", ledgerRows)
	}
}

func TestAddImprestExpenseRejectsBadForms(t *testing.T) {
	setupTestDB(t)

	category := imprestCategoryByName(t, "T&T")
	valid := func() url.Values {
		return url.Values{
			"date":        {"2026-03-04"},
			"category_id": {strconv.FormatInt(category.ID, 10)},
			"description": {"TNT to Pay tier 2"},
			"amount":      {"35.50"},
		}
	}

	scenarios := []struct {
		name   string
		mutate func(url.Values)
	}{
		{name: "missing date", mutate: func(form url.Values) { form.Set("date", "") }},
		{name: "impossible date", mutate: func(form url.Values) { form.Set("date", "2026-02-30") }},
		{name: "missing category", mutate: func(form url.Values) { form.Set("category_id", "") }},
		{name: "unknown category", mutate: func(form url.Values) { form.Set("category_id", "999999") }},
		{name: "missing description", mutate: func(form url.Values) { form.Set("description", "  ") }},
		{name: "zero amount", mutate: func(form url.Values) { form.Set("amount", "0") }},
		{name: "text amount", mutate: func(form url.Values) { form.Set("amount", "thirty") }},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			form := valid()
			scenario.mutate(form)
			request := httptest.NewRequest(http.MethodPost, "/api/imprest/add", strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			recorder := httptest.NewRecorder()

			AddImprestExpense(recorder, request)
			if recorder.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want a redirect", recorder.Code)
			}
			if location := recorder.Header().Get("Location"); !strings.Contains(location, "tone=warning") {
				t.Fatalf("redirect = %q, want a warning tone", location)
			}
		})
	}

	var rows int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM imprest_expenses`).Scan(&rows); err != nil {
		t.Fatalf("count imprest rows: %v", err)
	}
	if rows != 0 {
		t.Fatalf("imprest rows = %d, want none after refused forms", rows)
	}
}

func TestMainExpenditureImportRefusesImprestTopUps(t *testing.T) {
	setupTestDB(t)

	reason := saveImportedRow("expenditure", "2026-01-04", "IMPREST", "IMPREST", "", 1000, nil)
	if !strings.Contains(reason, "float top-ups") {
		t.Fatalf("IMPREST expenditure reason = %q, want a float-top-up refusal", reason)
	}

	// The refusal is specific to expenditure. An income label with the same word is not a
	// float top-up and must still go through the normal category lookup.
	reason = saveImportedRow("income", "2026-01-04", "IMPREST refund", "IMPREST", "", 1000, nil)
	if strings.Contains(reason, "float top-ups") {
		t.Fatalf("income row was refused as a float top-up: %q", reason)
	}

	var rows int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM transactions`).Scan(&rows); err != nil {
		t.Fatalf("count transactions after refused top-up: %v", err)
	}
	if rows != 0 {
		t.Fatalf("transaction count = %d, want the top-up kept out of the ledger", rows)
	}
}

func TestImprestTotalsCombineWithMainExpenditure(t *testing.T) {
	setupTestDB(t)

	insertTransaction(t, "2026-03-01", "income", "Offerings", categoryID(t, "income", "Offerings"), 500)
	insertTransaction(t, "2026-03-02", "expenditure", "Printing & Stationery", categoryID(t, "expenditure", "Printing & Stationery"), 120)
	insertImprestFixture(t, "2026-03-03", "T&T", "TNT to Pay tier 2", 30)
	insertImprestFixture(t, "2026-03-20", "REFRESHMENT", "Satchet water", 20)
	// A prior-year payment proves the year boundary is respected.
	insertImprestFixture(t, "2025-12-31", "OTHERS", "Old year purchase", 999)

	dashboard, err := buildDashboardData(
		time.Date(2026, time.March, 20, 12, 0, 0, 0, time.UTC),
		"dashboard",
		"",
		"",
	)
	if err != nil {
		t.Fatalf("build dashboard data: %v", err)
	}
	if dashboard.MonthImprest != 50 {
		t.Fatalf("month imprest = %.2f, want 50", dashboard.MonthImprest)
	}
	if dashboard.MonthTotalExpense != 170 {
		t.Fatalf("month total expense = %.2f, want 170", dashboard.MonthTotalExpense)
	}
	if dashboard.MonthSurplus != 330 {
		t.Fatalf("month surplus = %.2f, want 330", dashboard.MonthSurplus)
	}
	if dashboard.YearImprest != 50 {
		t.Fatalf("year imprest = %.2f, want 50", dashboard.YearImprest)
	}
	if dashboard.YearTotalExpense != 170 {
		t.Fatalf("year total expense = %.2f, want 170", dashboard.YearTotalExpense)
	}
	if dashboard.DailyImprest != 20 || dashboard.DailyTotalExpense != 20 {
		t.Fatalf("daily imprest = %.2f total = %.2f, want 20 and 20", dashboard.DailyImprest, dashboard.DailyTotalExpense)
	}
	if dashboard.DailyNetIncome != -20 {
		t.Fatalf("daily net income = %.2f, want -20 after the imprest payment", dashboard.DailyNetIncome)
	}

	monthly, err := buildMonthlyData(2026)
	if err != nil {
		t.Fatalf("build monthly data: %v", err)
	}
	if monthly.YTDImprest != 50 || monthly.YTDTotalExpense != 170 {
		t.Fatalf("monthly YTD imprest = %.2f total = %.2f, want 50 and 170", monthly.YTDImprest, monthly.YTDTotalExpense)
	}
	if monthly.YTDMainExpense != 120 {
		t.Fatalf("monthly YTD main expense = %.2f, want 120", monthly.YTDMainExpense)
	}
	if monthly.YTDSurplus != 330 {
		t.Fatalf("monthly YTD surplus = %.2f, want 330", monthly.YTDSurplus)
	}
	march := monthly.Months[time.March-1]
	if march.MainExpense != 120 || march.Imprest != 50 || march.TotalExpense != 170 || march.Surplus != 330 {
		t.Fatalf("march row = %+v, want main 120 imprest 50 total 170 surplus 330", march)
	}

	quarterly, err := buildQuarterlyData(2026)
	if err != nil {
		t.Fatalf("build quarterly data: %v", err)
	}
	if quarterly.TotalMainExpense != 120 || quarterly.TotalImprest != 50 || quarterly.TotalExpense != 170 {
		t.Fatalf(
			"quarterly main = %.2f imprest = %.2f total = %.2f, want 120/50/170",
			quarterly.TotalMainExpense,
			quarterly.TotalImprest,
			quarterly.TotalExpense,
		)
	}
	if quarterly.TotalSurplus != 330 {
		t.Fatalf("quarterly surplus = %.2f, want 330", quarterly.TotalSurplus)
	}
	q1 := quarterly.Quarters[0]
	if q1.MainExpense != 120 || q1.Imprest != 50 || q1.TotalExpense != 170 || q1.Surplus != 330 {
		t.Fatalf("Q1 row = %+v, want main 120 imprest 50 total 170 surplus 330", q1)
	}

	annual, err := buildAnnualData(2026)
	if err != nil {
		t.Fatalf("build annual data: %v", err)
	}
	if annual.TotalMainExpense != 120 || annual.TotalImprest != 50 || annual.TotalExpense != 170 {
		t.Fatalf("annual main = %.2f imprest = %.2f total = %.2f, want 120/50/170", annual.TotalMainExpense, annual.TotalImprest, annual.TotalExpense)
	}
	if annual.Surplus != 330 {
		t.Fatalf("annual surplus = %.2f, want 330", annual.Surplus)
	}
	imprestLine := annualLineByName(t, annual.ExpenseLines, "Imprest Expenses")
	if imprestLine.Amount != 50 {
		t.Fatalf("annual imprest line = %.2f, want 50", imprestLine.Amount)
	}
	totalLine := annualLineByName(t, annual.ExpenseLines, "TOTAL EXPENDITURE")
	if totalLine.Amount != 170 {
		t.Fatalf("annual total expenditure line = %.2f, want 170", totalLine.Amount)
	}

	balance, err := buildBalanceData(2026)
	if err != nil {
		t.Fatalf("build balance data: %v", err)
	}
	if balance.IncomeSurplus != 330 {
		t.Fatalf("balance income surplus = %.2f, want 330", balance.IncomeSurplus)
	}

	cashFlow, err := buildCashFlowData(2026)
	if err != nil {
		t.Fatalf("build cash flow data: %v", err)
	}
	if cashFlow.ExpenditureEntries != 120 || cashFlow.ImprestEntries != 50 || cashFlow.TotalExpenditure != 170 {
		t.Fatalf(
			"cash flow main = %.2f imprest = %.2f total = %.2f, want 120/50/170",
			cashFlow.ExpenditureEntries,
			cashFlow.ImprestEntries,
			cashFlow.TotalExpenditure,
		)
	}
	if cashFlow.IncomeLessExpenditure != 330 {
		t.Fatalf("cash flow income less expenditure = %.2f, want 330", cashFlow.IncomeLessExpenditure)
	}
}

func TestImprestPageRendersSeparateTotalsAndEntries(t *testing.T) {
	setupTestDB(t)

	insertTransaction(t, "2026-03-02", "expenditure", "Printing & Stationery", categoryID(t, "expenditure", "Printing & Stationery"), 120)
	insertImprestFixture(t, "2026-03-03", "T&T", "TNT to Pay tier 2", 30)

	recorder := httptest.NewRecorder()
	ImprestPage(recorder, httptest.NewRequest(http.MethodGet, "/imprest", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, marker := range []string{
		"Imprest Expenses",
		"Main Expenditure",
		"Total Expenditure",
		"TNT to Pay tier 2",
		"/api/imprest/bulk-import",
		"/api/imprest/category/save",
		"/api/imprest/category/delete",
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("imprest page missing %q", marker)
		}
	}
	// Imprest entries belong to their own page, not the main register.
	if strings.Contains(body, `data-page-tab-target="register-entries-panel"`) {
		t.Fatalf("imprest page rendered the main transaction register")
	}
}

func imprestUploadWorkbook(t *testing.T, rows [][]any) *excelize.File {
	t.Helper()

	workbook := excelize.NewFile()
	sheet := "2026"
	_ = workbook.SetSheetName(workbook.GetSheetName(0), sheet)
	_ = workbook.SetCellValue(sheet, "A1", "CASH BOOK # 1")
	_ = workbook.SetCellValue(sheet, "C1", "Imprest")
	_ = workbook.SetCellValue(sheet, "C2", "INCOME")
	_ = workbook.SetCellValue(sheet, "D2", "EXPENSE")
	_ = workbook.SetCellValue(sheet, "E2", "DETAILS")

	for index, row := range rows {
		rowNumber := index + 3
		for column, value := range row {
			if value == nil {
				continue
			}
			cell, err := excelize.CoordinatesToCellName(column+1, rowNumber)
			if err != nil {
				t.Fatalf("coordinates for row %d: %v", rowNumber, err)
			}
			_ = workbook.SetCellValue(sheet, cell, value)
		}
	}
	return workbook
}

func TestBulkImportImprestSavesMatchesAndReportsUnknownHeads(t *testing.T) {
	setupTestDB(t)

	workbook := imprestUploadWorkbook(t, [][]any{
		{"2026-01-20", "TNT to Pay tier 2 and documentation", nil, 35, "T&T"},
		{nil, "Bags of satchet water @6*10", nil, 60, "REFRESHMENT"},
		// The upload never creates a head; this row must be reported and skipped.
		{nil, "Mystery purchase", nil, 15, "UNKNOWN HEAD"},
		{nil, "Mowing of lawns", nil, 150, "CLEANING"},
	})
	defer workbook.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "imprest-2026.xlsx")
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if err := workbook.Write(part); err != nil {
		t.Fatalf("write workbook to upload: %v", err)
	}
	if err := writer.WriteField("year", "2026"); err != nil {
		t.Fatalf("write year field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/imprest/bulk-import", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()

	BulkImportImprest(recorder, request)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303: %s", recorder.Code, recorder.Body.String())
	}

	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse import redirect: %v", err)
	}
	summary, ok := bulkResultStore.Get(location.Query().Get("bulk"))
	if !ok {
		t.Fatalf("import summary was not stored for %q", location.String())
	}
	if summary.Sheet != "2026" {
		t.Fatalf("summary sheet = %q, want 2026", summary.Sheet)
	}
	if summary.Imported != 3 || summary.Skipped != 1 {
		t.Fatalf("summary imported = %d skipped = %d, want 3 and 1", summary.Imported, summary.Skipped)
	}
	if len(summary.Errors) != 1 || !strings.Contains(summary.Errors[0], "is not one of the imprest heads") {
		t.Fatalf("summary errors = %v, want the unknown head refusal", summary.Errors)
	}
	// The refusal must point at the Excel row, not the slice index.
	if !strings.Contains(summary.Errors[0], "Row 5:") {
		t.Fatalf("summary error = %q, want the spreadsheet row number", summary.Errors[0])
	}

	var (
		rows  int
		total float64
	)
	if err := db.DB.QueryRow(`SELECT COUNT(*), COALESCE(SUM(amount), 0) FROM imprest_expenses`).Scan(&rows, &total); err != nil {
		t.Fatalf("summarise imported imprest rows: %v", err)
	}
	if rows != 3 || total != 245 {
		t.Fatalf("imported rows = %d total = %.2f, want 3 and 245", rows, total)
	}
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM transactions`).Scan(&rows); err != nil {
		t.Fatalf("count main transactions after import: %v", err)
	}
	if rows != 0 {
		t.Fatalf("main transaction count = %d, want imprest kept out of the register", rows)
	}
}

func TestUpdateImprestExpenseChangesTheSavedPayment(t *testing.T) {
	setupTestDB(t)

	insertImprestFixture(t, "2026-03-03", "T&T", "Old description", 30)

	var entryID int64
	if err := db.DB.QueryRow(`SELECT id FROM imprest_expenses`).Scan(&entryID); err != nil {
		t.Fatalf("load imprest fixture id: %v", err)
	}
	category := imprestCategoryByName(t, "REFRESHMENT")
	recorder := postImprestForm(t, "/api/imprest/update", url.Values{
		"id":          {strconv.FormatInt(entryID, 10)},
		"date":        {"2026-04-02"},
		"category_id": {strconv.FormatInt(category.ID, 10)},
		"description": {"Bags of satchet water"},
		"amount":      {"60.25"},
		"return_to":   {"/imprest?page=2"},
	}, UpdateImprestExpense)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", recorder.Code)
	}
	location := recorder.Header().Get("Location")
	if !strings.Contains(location, "/imprest?page=2") || strings.Contains(location, "tone=warning") {
		t.Fatalf("update redirect = %q, want the preserved page without a warning", location)
	}

	var (
		date         string
		categoryName string
		description  string
		amount       float64
	)
	if err := db.DB.QueryRow(
		`SELECT date, category, description, amount FROM imprest_expenses WHERE id = ?`,
		entryID,
	).Scan(&date, &categoryName, &description, &amount); err != nil {
		t.Fatalf("load updated imprest payment: %v", err)
	}
	if date != "2026-04-02" || categoryName != "REFRESHMENT" || description != "Bags of satchet water" || amount != 60.25 {
		t.Fatalf("updated imprest payment = %s/%s/%s/%.2f", date, categoryName, description, amount)
	}
}

func TestDeleteImprestExpenseRemovesTheSavedPayment(t *testing.T) {
	setupTestDB(t)

	insertImprestFixture(t, "2026-03-03", "T&T", "TNT to Pay tier 2", 30)

	var entryID int64
	if err := db.DB.QueryRow(`SELECT id FROM imprest_expenses`).Scan(&entryID); err != nil {
		t.Fatalf("load imprest fixture id: %v", err)
	}
	recorder := postImprestForm(t, "/api/imprest/delete", url.Values{
		"id":        {strconv.FormatInt(entryID, 10)},
		"return_to": {"/imprest"},
	}, DeleteImprestExpense)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", recorder.Code)
	}
	if location := recorder.Header().Get("Location"); strings.Contains(location, "tone=warning") {
		t.Fatalf("valid delete redirected as a warning: %q", location)
	}

	var rows int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM imprest_expenses`).Scan(&rows); err != nil {
		t.Fatalf("count imprest rows after delete: %v", err)
	}
	if rows != 0 {
		t.Fatalf("imprest rows = %d, want the payment removed", rows)
	}
}

func TestRenameImprestHeadUpdatesSavedPayments(t *testing.T) {
	setupTestDB(t)

	insertImprestFixture(t, "2026-03-03", "T&T", "TNT to Pay tier 2", 30)
	category := imprestCategoryByName(t, "T&T")

	recorder := postImprestForm(t, "/api/imprest/category/save", url.Values{
		"id":        {strconv.FormatInt(category.ID, 10)},
		"name":      {"TRANSPORT"},
		"return_to": {"/imprest"},
	}, SaveImprestCategory)
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", recorder.Code)
	}
	if location := recorder.Header().Get("Location"); strings.Contains(location, "tone=warning") {
		t.Fatalf("valid rename redirected as a warning: %q", location)
	}

	var (
		headName     string
		paymentHead  string
		paymentCount int
	)
	if err := db.DB.QueryRow(`SELECT name FROM imprest_categories WHERE id = ?`, category.ID).Scan(&headName); err != nil {
		t.Fatalf("load renamed head: %v", err)
	}
	if err := db.DB.QueryRow(`SELECT category, COUNT(*) FROM imprest_expenses GROUP BY category`).Scan(&paymentHead, &paymentCount); err != nil {
		t.Fatalf("load renamed payment head: %v", err)
	}
	if headName != "TRANSPORT" || paymentHead != "TRANSPORT" || paymentCount != 1 {
		t.Fatalf("rename result = head %q payment %q count %d, want both TRANSPORT and one payment", headName, paymentHead, paymentCount)
	}
}

func TestAddImprestHeadRejectsDuplicatesAndSavesNewNames(t *testing.T) {
	setupTestDB(t)

	var before int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM imprest_categories`).Scan(&before); err != nil {
		t.Fatalf("count imprest heads before add: %v", err)
	}

	duplicate := postImprestForm(t, "/api/imprest/category/save", url.Values{
		"name":      {"  t&t  "},
		"return_to": {"/imprest"},
	}, SaveImprestCategory)
	if duplicate.Code != http.StatusSeeOther {
		t.Fatalf("duplicate add status = %d, want 303", duplicate.Code)
	}
	if location := duplicate.Header().Get("Location"); !strings.Contains(location, "tone=warning") {
		t.Fatalf("duplicate head redirect = %q, want a warning", location)
	}
	var after int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM imprest_categories`).Scan(&after); err != nil {
		t.Fatalf("count imprest heads after duplicate: %v", err)
	}
	if after != before {
		t.Fatalf("imprest head count = %d, want the duplicate refused at %d", after, before)
	}

	added := postImprestForm(t, "/api/imprest/category/save", url.Values{
		"name":      {"TRANSPORT"},
		"return_to": {"/imprest"},
	}, SaveImprestCategory)
	if added.Code != http.StatusSeeOther {
		t.Fatalf("new head status = %d, want 303", added.Code)
	}
	if location := added.Header().Get("Location"); strings.Contains(location, "tone=warning") {
		t.Fatalf("new head redirect = %q, want a save", location)
	}
	var addedID int64
	if err := db.DB.QueryRow(`SELECT id FROM imprest_categories WHERE name = 'TRANSPORT'`).Scan(&addedID); err != nil {
		t.Fatalf("load added head: %v", err)
	}
}

func TestDeleteImprestHeadRefusesWhilePaymentsUseIt(t *testing.T) {
	setupTestDB(t)

	insertImprestFixture(t, "2026-03-03", "T&T", "TNT to Pay tier 2", 30)
	used := imprestCategoryByName(t, "T&T")

	refused := postImprestForm(t, "/api/imprest/category/delete", url.Values{
		"id":        {strconv.FormatInt(used.ID, 10)},
		"return_to": {"/imprest"},
	}, DeleteImprestCategory)
	if refused.Code != http.StatusSeeOther {
		t.Fatalf("used head delete status = %d, want 303", refused.Code)
	}
	if location := refused.Header().Get("Location"); !strings.Contains(location, "tone=warning") {
		t.Fatalf("used head delete redirect = %q, want a warning", location)
	}
	var stillThere int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM imprest_categories WHERE id = ?`, used.ID).Scan(&stillThere); err != nil {
		t.Fatalf("count used head after refusal: %v", err)
	}
	if stillThere != 1 {
		t.Fatalf("used head count = %d, want the head kept", stillThere)
	}

	added := postImprestForm(t, "/api/imprest/category/save", url.Values{
		"name":      {"TRANSPORT"},
		"return_to": {"/imprest"},
	}, SaveImprestCategory)
	if location := added.Header().Get("Location"); strings.Contains(location, "tone=warning") {
		t.Fatalf("unused head setup = %q, want a save", location)
	}
	unused := imprestCategoryByName(t, "TRANSPORT")

	deleted := postImprestForm(t, "/api/imprest/category/delete", url.Values{
		"id":        {strconv.FormatInt(unused.ID, 10)},
		"return_to": {"/imprest"},
	}, DeleteImprestCategory)
	if location := deleted.Header().Get("Location"); strings.Contains(location, "tone=warning") {
		t.Fatalf("unused head delete = %q, want a save", location)
	}
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM imprest_categories WHERE id = ?`, unused.ID).Scan(&stillThere); err != nil {
		t.Fatalf("count unused head after delete: %v", err)
	}
	if stillThere != 0 {
		t.Fatalf("unused head count = %d, want the head removed", stillThere)
	}
}

func TestImprestEntriesPaginateBeyondTheOldHundredRowLimit(t *testing.T) {
	setupTestDB(t)

	category := imprestCategoryByName(t, "T&T")
	rowCount := imprestPageSize + 5
	for index := 1; index <= rowCount; index++ {
		if err := insertImprestExpense(
			"2026-03-03",
			category,
			fmt.Sprintf("Imprest row %03d", index),
			1,
		); err != nil {
			t.Fatalf("insert imprest row %d: %v", index, err)
		}
	}

	firstPage, total, err := loadImprestEntries(2026, 1, imprestPageSize)
	if err != nil {
		t.Fatalf("load first imprest page: %v", err)
	}
	if total != rowCount || len(firstPage) != imprestPageSize {
		t.Fatalf("first page total = %d rows = %d, want %d and %d", total, len(firstPage), rowCount, imprestPageSize)
	}
	secondPage, _, err := loadImprestEntries(2026, 2, imprestPageSize)
	if err != nil {
		t.Fatalf("load second imprest page: %v", err)
	}
	if len(secondPage) != 5 {
		t.Fatalf("second page rows = %d, want the remaining 5", len(secondPage))
	}
	if firstPage[0].Description != fmt.Sprintf("Imprest row %03d", rowCount) {
		t.Fatalf("newest row = %q, want row %03d first", firstPage[0].Description, rowCount)
	}
	if secondPage[len(secondPage)-1].Description != "Imprest row 001" {
		t.Fatalf("oldest row = %q, want row 001 last", secondPage[len(secondPage)-1].Description)
	}

	recorder := httptest.NewRecorder()
	ImprestPage(recorder, httptest.NewRequest(http.MethodGet, "/imprest", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("first page status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, fmt.Sprintf("Imprest row %03d", rowCount)) {
		t.Fatalf("first page missing the newest row")
	}
	if strings.Contains(body, "Imprest row 001") {
		t.Fatalf("first page rendered a row that belongs on the second page")
	}
	if !strings.Contains(body, "/imprest?page=2") {
		t.Fatalf("first page missing the next-page link")
	}

	recorder = httptest.NewRecorder()
	ImprestPage(recorder, httptest.NewRequest(http.MethodGet, "/imprest?page=2", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("second page status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "Imprest row 001") {
		t.Fatalf("second page missing the oldest row")
	}
}

func TestImprestPageOpensTheEditModalForATargetEntry(t *testing.T) {
	setupTestDB(t)

	insertImprestFixture(t, "2026-03-03", "T&T", "TNT to Pay tier 2", 30)

	var entryID int64
	if err := db.DB.QueryRow(`SELECT id FROM imprest_expenses`).Scan(&entryID); err != nil {
		t.Fatalf("load imprest fixture id: %v", err)
	}
	recorder := httptest.NewRecorder()
	ImprestPage(recorder, httptest.NewRequest(
		http.MethodGet,
		"/imprest?edit="+strconv.FormatInt(entryID, 10),
		nil,
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	for _, marker := range []string{
		`id="edit-imprest-modal"`,
		"/api/imprest/update",
		"TNT to Pay tier 2",
	} {
		if !strings.Contains(body, marker) {
			t.Fatalf("edit page missing %q", marker)
		}
	}
}
