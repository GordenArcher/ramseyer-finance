package finance

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"ramseyer-finance/internal/db"
	"testing"

	"github.com/xuri/excelize/v2"
)

// expenseLedgerWorkbook mirrors the client's real workbook: two unrelated helper tabs plus
// a ledger that starts with a blank row, labels the amount after the description, spells
// the header "DESCRIP TION", and leaves the expense head unlabelled in the column after the
// amount. Building it here keeps the parser honest against the file it must accept.
func expenseLedgerWorkbook(t *testing.T, rows [][]any) *excelize.File {
	t.Helper()

	workbook := excelize.NewFile()
	bankBook := workbook.GetSheetName(0)
	_ = workbook.SetSheetName(bankBook, "Sheet1")
	_ = workbook.SetCellValue(bankBook, "A1", "DATE")
	_ = workbook.SetCellValue(bankBook, "B1", "AMOUNT(GHC)")
	_ = workbook.SetCellValue(bankBook, "C1", "CHECQUE  NO. ")
	_ = workbook.SetCellValue(bankBook, "D1", "DESIGNATION ")
	_ = workbook.SetCellValue(bankBook, "A2", "closing bal.")

	ledger := "EXPENSES 2026"
	if _, err := workbook.NewSheet(ledger); err != nil {
		t.Fatalf("create ledger sheet: %v", err)
	}
	// Row 1 is left blank on purpose, matching the client file.
	_ = workbook.SetCellValue(ledger, "A2", "2026-01-04")
	_ = workbook.SetCellValue(ledger, "B2", "DESCRIP TION")
	_ = workbook.SetCellValue(ledger, "C2", "AMOUNT")

	for index, row := range rows {
		rowNumber := index + 3
		for column, value := range row {
			cell, err := excelize.CoordinatesToCellName(column+1, rowNumber)
			if err != nil {
				t.Fatalf("coordinates for row %d: %v", rowNumber, err)
			}
			if value == nil {
				continue
			}
			_ = workbook.SetCellValue(ledger, cell, value)
		}
	}

	return workbook
}

func bankBookWorkbook(t *testing.T) *excelize.File {
	t.Helper()

	workbook := excelize.NewFile()
	_ = workbook.SetSheetName(workbook.GetSheetName(0), "Sheet1")
	_ = workbook.SetCellValue("Sheet1", "A1", "DATE")
	_ = workbook.SetCellValue("Sheet1", "B1", "AMOUNT(GHC)")
	_ = workbook.SetCellValue("Sheet1", "C1", "CHECQUE  NO. ")
	_ = workbook.SetCellValue("Sheet1", "D1", "DESIGNATION ")
	_ = workbook.SetCellValue("Sheet1", "A2", "2026-01-04")
	_ = workbook.SetCellValue("Sheet1", "B2", 4000)
	_ = workbook.SetCellValue("Sheet1", "C2", 3965)
	_ = workbook.SetCellValue("Sheet1", "D2", "Part payment of c/s")
	_ = workbook.SetCellValue("Sheet1", "E2", "END OF YR GET TOGETHER")
	return workbook
}

func uploadWorkbook(t *testing.T, workbook *excelize.File, transactionType string) *httptest.ResponseRecorder {
	t.Helper()

	var workbookBytes bytes.Buffer
	if err := workbook.Write(&workbookBytes); err != nil {
		t.Fatalf("write workbook: %v", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("type", transactionType); err != nil {
		t.Fatalf("write type field: %v", err)
	}
	part, err := writer.CreateFormFile("file", "EXPENDITURE FOR 2026.xlsx")
	if err != nil {
		t.Fatalf("create file part: %v", err)
	}
	if _, err := part.Write(workbookBytes.Bytes()); err != nil {
		t.Fatalf("write file part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/data-entry/bulk-import", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	BulkImportTransaction(recorder, request)
	return recorder
}

// combinedWorkbook builds the one-sheet all-types layout: Date, Type, Description, Amount,
// Category, Payment Method. The Type cell is what routes each row to the right namespace.
func combinedWorkbook(t *testing.T, rows [][]any) *excelize.File {
	t.Helper()

	workbook := excelize.NewFile()
	const sheet = "All Entries"
	_ = workbook.SetSheetName(workbook.GetSheetName(0), sheet)

	headers := []string{"Date", "Type", "Description", "Amount", "Category", "Payment Method"}
	for index, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(index+1, 1)
		_ = workbook.SetCellValue(sheet, cell, header)
	}

	for index, row := range rows {
		rowNumber := index + 2
		for column, value := range row {
			cell, err := excelize.CoordinatesToCellName(column+1, rowNumber)
			if err != nil {
				t.Fatalf("coordinates for row %d: %v", rowNumber, err)
			}
			if value == nil {
				continue
			}
			_ = workbook.SetCellValue(sheet, cell, value)
		}
	}

	return workbook
}

func writeWorkbookPart(t *testing.T, writer *multipart.Writer, field, filename string, workbook *excelize.File) {
	t.Helper()

	var workbookBytes bytes.Buffer
	if err := workbook.Write(&workbookBytes); err != nil {
		t.Fatalf("write workbook: %v", err)
	}
	part, err := writer.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("create %s part: %v", field, err)
	}
	if _, err := part.Write(workbookBytes.Bytes()); err != nil {
		t.Fatalf("write %s part: %v", field, err)
	}
}

func uploadCombinedWorkbook(t *testing.T, workbook *excelize.File) *httptest.ResponseRecorder {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	writeWorkbookPart(t, writer, "file", "all-entries.xlsx", workbook)
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/data-entry/bulk-import-all", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	BulkImportAllTransactions(recorder, request)
	return recorder
}

func TestBulkImportReadsTheClientLedgerSheet(t *testing.T) {
	setupTestDB(t)
	stationery := categoryID(t, "expenditure", "Printing & Stationery")

	workbook := expenseLedgerWorkbook(t, [][]any{
		{"2026-01-04", "PLAN OF WORK", 456, "Printing & Stationery"},
		{"2026-01-04", "WATER", 400, "Utilities-Manse"},
		// A subtotal row must never become a posting.
		{nil, nil, "=SUM(C3:C4)", nil},
	})
	defer workbook.Close()

	recorder := uploadWorkbook(t, workbook, "expenditure")
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("bulk import status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	if got := countTransactionsForCategory(t, stationery); got != 1 {
		t.Fatalf("stationery rows = %d, want 1", got)
	}

	summary, ok := bulkResultStore.Get(summaryTokenFromRedirect(t, recorder.Header().Get("Location")))
	if !ok {
		t.Fatalf("import summary was not stored")
	}
	if summary.Sheet != "EXPENSES 2026" {
		t.Fatalf("imported sheet = %q, want EXPENSES 2026", summary.Sheet)
	}
	if summary.Imported != 2 {
		t.Fatalf("imported rows = %d, want 2 (%v)", summary.Imported, summary.Errors)
	}
}

func TestBulkImportRejectsTheBankBookForExpenditure(t *testing.T) {
	setupTestDB(t)

	workbook := bankBookWorkbook(t)
	defer workbook.Close()

	recorder := uploadWorkbook(t, workbook, "expenditure")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("bank book upload status = %d, want 400", recorder.Code)
	}
}

func TestBulkImportReportsUnknownCategoryWithoutCreatingOne(t *testing.T) {
	setupTestDB(t)

	workbook := expenseLedgerWorkbook(t, [][]any{
		{"2026-01-04", "MYSTERY ITEM", 500, "Not A Real Account"},
	})
	defer workbook.Close()

	recorder := uploadWorkbook(t, workbook, "expenditure")
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("bulk import status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	summary, ok := bulkResultStore.Get(summaryTokenFromRedirect(t, recorder.Header().Get("Location")))
	if !ok {
		t.Fatalf("import summary was not stored")
	}
	if summary.Imported != 0 {
		t.Fatalf("imported rows = %d, want 0", summary.Imported)
	}
	if len(summary.Errors) == 0 {
		t.Fatalf("unknown category produced no explanation")
	}

	var created int
	if err := db.DB.QueryRow(
		`SELECT COUNT(*) FROM categories WHERE type = 'expenditure' AND name = 'Not A Real Account'`,
	).Scan(&created); err != nil {
		t.Fatalf("count categories: %v", err)
	}
	if created != 0 {
		t.Fatalf("bulk import created %d accounts, want 0", created)
	}
}

func TestBulkImportSkipsTheSecondUploadOfTheSameFile(t *testing.T) {
	setupTestDB(t)
	stationery := categoryID(t, "expenditure", "Printing & Stationery")

	rows := [][]any{
		{"2026-01-04", "PLAN OF WORK", 456, "Printing & Stationery"},
	}

	first := expenseLedgerWorkbook(t, rows)
	recorder := uploadWorkbook(t, first, "expenditure")
	first.Close()
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("first upload status = %d", recorder.Code)
	}

	second := expenseLedgerWorkbook(t, rows)
	recorder = uploadWorkbook(t, second, "expenditure")
	second.Close()
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("second upload status = %d", recorder.Code)
	}

	if got := countTransactionsForCategory(t, stationery); got != 1 {
		t.Fatalf("stationery rows after re-upload = %d, want 1", got)
	}
}

func TestBulkImportAllUploadsEveryTypeFromOneSheet(t *testing.T) {
	setupTestDB(t)
	tithes := categoryID(t, "income", "Tithes")
	stationery := categoryID(t, "expenditure", "Printing & Stationery")

	workbook := combinedWorkbook(t, [][]any{
		{"2026-01-04", "income", "Sunday offering", 1000, "Tithes", "cash"},
		{"2026-01-04", "expenditure", "Plan of work", 456, "Printing & Stationery", "bank"},
	})
	recorder := uploadCombinedWorkbook(t, workbook)
	workbook.Close()

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("all-types import status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := countTransactionsForCategory(t, tithes); got != 1 {
		t.Fatalf("income rows = %d, want 1", got)
	}
	if got := countTransactionsForCategory(t, stationery); got != 1 {
		t.Fatalf("expenditure rows = %d, want 1", got)
	}

	summary, ok := bulkResultStore.Get(summaryTokenFromRedirect(t, recorder.Header().Get("Location")))
	if !ok {
		t.Fatalf("all-types summary was not stored")
	}
	if summary.Imported != 2 {
		t.Fatalf("imported rows = %d, want 2", summary.Imported)
	}
	if len(summary.Sections) != 2 {
		t.Fatalf("sections = %d, want 2", len(summary.Sections))
	}
	if summary.Sections[0].Type != "income" || summary.Sections[1].Type != "expenditure" {
		t.Fatalf("sections = %+v, want income then expenditure", summary.Sections)
	}
}

func TestBulkImportAllRequiresAFile(t *testing.T) {
	setupTestDB(t)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/data-entry/bulk-import-all", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	BulkImportAllTransactions(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("empty all-types upload status = %d, want 400", recorder.Code)
	}
}

func TestBulkImportAllRefusesAnUnknownTypeAndKeepsTheRest(t *testing.T) {
	setupTestDB(t)
	stationery := categoryID(t, "expenditure", "Printing & Stationery")

	workbook := combinedWorkbook(t, [][]any{
		{"2026-01-04", "donation", "MYSTERY ITEM", 100, "Tithes", "cash"},
		{"2026-01-04", "expenditure", "Plan of work", 456, "Printing & Stationery", "cash"},
	})
	recorder := uploadCombinedWorkbook(t, workbook)
	workbook.Close()

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("all-types import status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := countTransactionsForCategory(t, stationery); got != 1 {
		t.Fatalf("expenditure rows = %d, want 1 after an unknown type", got)
	}

	summary, ok := bulkResultStore.Get(summaryTokenFromRedirect(t, recorder.Header().Get("Location")))
	if !ok {
		t.Fatalf("all-types summary was not stored")
	}
	if summary.Imported != 1 {
		t.Fatalf("imported rows = %d, want 1", summary.Imported)
	}
	if len(summary.Errors) == 0 {
		t.Fatalf("the unknown type produced no explanation")
	}
}

func TestDownloadDataEntryTemplateBuildsTheCombinedSheet(t *testing.T) {
	setupTestDB(t)

	recorder := httptest.NewRecorder()
	DownloadDataEntryTemplate(recorder, httptest.NewRequest(http.MethodGet, "/api/data-entry/template?type=all&year=2026", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("combined template status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	workbook, err := excelize.OpenReader(bytes.NewReader(recorder.Body.Bytes()))
	if err != nil {
		t.Fatalf("open combined template: %v", err)
	}
	defer workbook.Close()

	if names := workbook.GetSheetList(); len(names) == 0 || names[0] != "All Entries" {
		t.Fatalf("combined template sheets = %v, want All Entries first", names)
	}
	if header, _ := workbook.GetCellValue("All Entries", "B1"); header != "Type" {
		t.Fatalf("combined template type header = %q, want Type", header)
	}
}

func TestDownloadDataEntryTemplateUsesTheClientExpenseLayout(t *testing.T) {
	setupTestDB(t)

	recorder := httptest.NewRecorder()
	DownloadDataEntryTemplate(recorder, httptest.NewRequest(http.MethodGet, "/api/data-entry/template?type=expenditure&year=2026", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("template status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	workbook, err := excelize.OpenReader(bytes.NewReader(recorder.Body.Bytes()))
	if err != nil {
		t.Fatalf("open template: %v", err)
	}
	defer workbook.Close()

	sheets := workbook.GetSheetList()
	if len(sheets) == 0 || sheets[0] != "EXPENSES 2026" {
		t.Fatalf("template sheets = %v, want the expenses sheet first", sheets)
	}
	if header, _ := workbook.GetCellValue("EXPENSES 2026", "B2", excelize.Options{RawCellValue: true}); header != "DESCRIP TION" {
		t.Fatalf("template description header = %q, want DESCRIP TION", header)
	}
	if header, _ := workbook.GetCellValue("EXPENSES 2026", "C2", excelize.Options{RawCellValue: true}); header != "AMOUNT" {
		t.Fatalf("template amount header = %q, want AMOUNT", header)
	}
	if value, _ := workbook.GetCellValue("EXPENSES 2026", "A3"); value != "" {
		t.Fatalf("template wrote a data row: A3 = %q", value)
	}
}

func TestDownloadDataEntryTemplateKeepsTheGenericHeaderForOtherTypes(t *testing.T) {
	setupTestDB(t)

	recorder := httptest.NewRecorder()
	DownloadDataEntryTemplate(recorder, httptest.NewRequest(http.MethodGet, "/api/data-entry/template?type=income&year=2026", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("template status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	workbook, err := excelize.OpenReader(bytes.NewReader(recorder.Body.Bytes()))
	if err != nil {
		t.Fatalf("open template: %v", err)
	}
	defer workbook.Close()

	if names := workbook.GetSheetList(); len(names) == 0 || names[0] != "income" {
		t.Fatalf("template sheets = %v, want income first", names)
	}
	if header, _ := workbook.GetCellValue("income", "B1"); header != "Description" {
		t.Fatalf("template description header = %q, want Description", header)
	}
}

func TestExportDataEntryExcelWritesTheClientWorkbook(t *testing.T) {
	setupTestDB(t)
	stationery := categoryID(t, "expenditure", "Printing & Stationery")

	if _, err := db.DB.Exec(
		`INSERT INTO transactions (date, type, category, category_id, payment_method, description, amount, note_ref, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now','localtime'))`,
		"2026-01-04", "expenditure", "Printing & Stationery", stationery, "cash", "PLAN OF WORK", 456.0, "",
	); err != nil {
		t.Fatalf("insert expenditure: %v", err)
	}

	recorder := httptest.NewRecorder()
	ExportDataEntryExcel(recorder, httptest.NewRequest(http.MethodGet, "/api/data-entry/export?type=expenditure&year=2026", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("export status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	workbook, err := excelize.OpenReader(bytes.NewReader(recorder.Body.Bytes()))
	if err != nil {
		t.Fatalf("open export: %v", err)
	}
	defer workbook.Close()

	sheet := "EXPENSES 2026"
	if names := workbook.GetSheetList(); len(names) != 1 || names[0] != sheet {
		t.Fatalf("export sheets = %v, want only %s", names, sheet)
	}
	rows, err := workbook.GetRows(sheet, excelize.Options{RawCellValue: true})
	if err != nil {
		t.Fatalf("read export rows: %v", err)
	}
	if len(rows) < 3 {
		t.Fatalf("export has %d rows, want header and data", len(rows))
	}
	if rows[1][1] != "DESCRIP TION" || rows[1][2] != "AMOUNT" {
		t.Fatalf("export header row = %v", rows[1])
	}
	if rows[2][0] != "46026" || rows[2][1] != "PLAN OF WORK" || rows[2][2] != "456" || rows[2][3] != "Printing & Stationery" {
		t.Fatalf("export data row = %v", rows[2])
	}
}

func TestExportDataEntryExcelOnlyServesExpenditure(t *testing.T) {
	setupTestDB(t)

	recorder := httptest.NewRecorder()
	ExportDataEntryExcel(recorder, httptest.NewRequest(http.MethodGet, "/api/data-entry/export?type=income&year=2026", nil))
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("income export status = %d, want 400", recorder.Code)
	}
}

func countTransactionsForCategory(t *testing.T, category int64) int {
	t.Helper()

	var count int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM transactions WHERE category_id = ?`, category).Scan(&count); err != nil {
		t.Fatalf("count transactions: %v", err)
	}
	return count
}

func summaryTokenFromRedirect(t *testing.T, location string) string {
	t.Helper()

	if location == "" {
		t.Fatalf("redirect location is empty")
	}
	for _, pair := range bytes.Split([]byte(location), []byte("&")) {
		if bytes.HasPrefix(pair, []byte("/data-entry?bulk=")) {
			return string(bytes.TrimPrefix(pair, []byte("/data-entry?bulk=")))
		}
	}
	t.Fatalf("redirect location %q has no bulk token", location)
	return ""
}
