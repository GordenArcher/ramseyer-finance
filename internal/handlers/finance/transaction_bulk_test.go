package finance

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"ramseyer-finance/internal/db"
	"strings"
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

func TestBulkImportStillRefusesUnknownIncomeCategories(t *testing.T) {
	setupTestDB(t)

	workbook := expenseLedgerWorkbook(t, [][]any{
		{"2026-01-04", "MYSTERY INCOME", 500, "Not A Real Income Account"},
	})
	defer workbook.Close()

	recorder := uploadWorkbook(t, workbook, "income")
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
		t.Fatalf("unknown income category produced no explanation")
	}
	if len(summary.Created) != 0 {
		t.Fatalf("income upload created %v, want no accounts", summary.Created)
	}

	var created int
	if err := db.DB.QueryRow(
		`SELECT COUNT(*) FROM categories WHERE type = 'income' AND name = 'Not A Real Income Account'`,
	).Scan(&created); err != nil {
		t.Fatalf("count categories: %v", err)
	}
	if created != 0 {
		t.Fatalf("income upload created %d accounts, want 0", created)
	}
}

func TestBulkImportCreatesUnknownExpenditureHeadsUnderOtherExpenditure(t *testing.T) {
	setupTestDB(t)

	workbook := expenseLedgerWorkbook(t, [][]any{
		{"2026-01-04", "ASSESSMENT", 500, "Not A Real Account"},
		{"2026-01-05", "ASSESSMENT AGAIN", 250, "Not A Real Account"},
		{"2026-01-06", "MYSTERY", 100, "Another New Head"},
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
	if summary.Imported != 3 {
		t.Fatalf("imported rows = %d, want 3 (%v)", summary.Imported, summary.Errors)
	}
	if len(summary.Created) != 2 || summary.Created[0] != "Not A Real Account" || summary.Created[1] != "Another New Head" {
		t.Fatalf("created heads = %v, want the two new heads once each", summary.Created)
	}

	var (
		parentID int64
		noteRef  string
	)
	if err := db.DB.QueryRow(
		`SELECT id, note_ref FROM categories WHERE type = 'expenditure' AND name = 'Other Expenditure' AND parent_id = 0`,
	).Scan(&parentID, &noteRef); err != nil {
		t.Fatalf("load Other Expenditure parent: %v", err)
	}
	if noteRef != "20" {
		t.Fatalf("Other Expenditure note = %q, want 20", noteRef)
	}

	for _, name := range []string{"Not A Real Account", "Another New Head"} {
		var (
			headType   string
			headParent int64
			headNote   string
		)
		if err := db.DB.QueryRow(
			`SELECT type, parent_id, note_ref FROM categories WHERE name = ?`,
			name,
		).Scan(&headType, &headParent, &headNote); err != nil {
			t.Fatalf("load created head %q: %v", name, err)
		}
		if headType != "expenditure" || headParent != parentID || headNote != "20" {
			t.Fatalf("head %q type/parent/note = %s/%d/%s, want expenditure/%d/20", name, headType, headParent, headNote, parentID)
		}
	}
}

func TestBulkImportReusesExpenseHeadsOnASecondUpload(t *testing.T) {
	setupTestDB(t)

	rows := [][]any{
		{"2026-01-04", "MYSTERY ITEM", 500, "Not A Real Account"},
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

	summary, ok := bulkResultStore.Get(summaryTokenFromRedirect(t, recorder.Header().Get("Location")))
	if !ok {
		t.Fatalf("second import summary was not stored")
	}
	if summary.Imported != 0 {
		t.Fatalf("second upload imported %d rows, want 0", summary.Imported)
	}
	if len(summary.Created) != 0 {
		t.Fatalf("second upload created %v, want no new heads", summary.Created)
	}

	var heads int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM categories WHERE name = 'Not A Real Account'`).Scan(&heads); err != nil {
		t.Fatalf("count expense heads: %v", err)
	}
	if heads != 1 {
		t.Fatalf("expense head count = %d, want 1", heads)
	}

	var saved int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM transactions WHERE type = 'expenditure'`).Scan(&saved); err != nil {
		t.Fatalf("count expenditure rows: %v", err)
	}
	if saved != 1 {
		t.Fatalf("expenditure rows after re-upload = %d, want 1", saved)
	}
}

func TestBulkImportNormalisesExpenseHeadVariants(t *testing.T) {
	setupTestDB(t)

	workbook := expenseLedgerWorkbook(t, [][]any{
		{"2026-02-01", "TEA", 10, "T & T"},
		{"2026-02-01", "TEA AGAIN", 20, "T&T"},
		{"2026-02-02", "TIER", 30, "TIER2"},
		{"2026-02-02", "TIER AGAIN", 40, "TIER 2"},
		{"2026-02-03", "REFRESH", 50, "REFESHMENT"},
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
	if summary.Imported != 5 {
		t.Fatalf("imported rows = %d, want 5 (%v)", summary.Imported, summary.Errors)
	}
	if len(summary.Created) != 2 || summary.Created[0] != "T&T" || summary.Created[1] != "TIER 2" {
		t.Fatalf("created heads = %v, want T&T and TIER 2", summary.Created)
	}

	for _, head := range []struct {
		name string
		rows int
	}{
		{"T&T", 2},
		{"TIER 2", 2},
	} {
		var heads int
		if err := db.DB.QueryRow(
			`SELECT COUNT(*) FROM categories WHERE type = 'expenditure' AND name = ?`,
			head.name,
		).Scan(&heads); err != nil {
			t.Fatalf("count head %q: %v", head.name, err)
		}
		if heads != 1 {
			t.Fatalf("head %q count = %d, want 1", head.name, heads)
		}

		var rows int
		if err := db.DB.QueryRow(
			`SELECT COUNT(*) FROM transactions WHERE category = ?`,
			head.name,
		).Scan(&rows); err != nil {
			t.Fatalf("count rows for head %q: %v", head.name, err)
		}
		if rows != head.rows {
			t.Fatalf("rows for head %q = %d, want %d", head.name, rows, head.rows)
		}
	}

	// REFESHMENT is repaired to REFRESHMENT, and that spelling resolves to the chart's
	// existing refreshments account instead of adding another head.
	var refreshRows int
	if err := db.DB.QueryRow(
		`SELECT COUNT(*) FROM transactions WHERE category = 'General Refreshments/Hospitality'`,
	).Scan(&refreshRows); err != nil {
		t.Fatalf("count refreshment rows: %v", err)
	}
	if refreshRows != 1 {
		t.Fatalf("refreshment rows = %d, want the row matched to General Refreshments/Hospitality", refreshRows)
	}
	var refreshHeads int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM categories WHERE name = 'REFRESHMENT'`).Scan(&refreshHeads); err != nil {
		t.Fatalf("count REFRESHMENT head: %v", err)
	}
	if refreshHeads != 0 {
		t.Fatalf("REFRESHMENT head count = %d, want the existing chart account reused", refreshHeads)
	}
}

func TestBulkImportSkipsImprestRowsInTheExpenseSheet(t *testing.T) {
	setupTestDB(t)

	workbook := expenseLedgerWorkbook(t, [][]any{
		{"2026-01-04", "IMPREST TOP-UP", 1000, "IMPREST"},
		{"2026-01-05", "STATIONERY", 50, "STATIONERY"},
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
	if summary.Imported != 1 || summary.Skipped != 1 {
		t.Fatalf("imported/skipped = %d/%d, want 1/1 (%v)", summary.Imported, summary.Skipped, summary.Errors)
	}
	if !strings.Contains(strings.Join(summary.Errors, " "), "float top-ups") {
		t.Fatalf("skipped IMPREST row produced no float-top-up explanation: %v", summary.Errors)
	}

	var heads int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM categories WHERE name = 'IMPREST'`).Scan(&heads); err != nil {
		t.Fatalf("count IMPREST heads: %v", err)
	}
	if heads != 0 {
		t.Fatalf("IMPREST head count = %d, want 0", heads)
	}
}

func TestBulkImportAllCreatesExpenditureHeadsFromOneSheet(t *testing.T) {
	setupTestDB(t)
	tithes := categoryID(t, "income", "Tithes")

	workbook := combinedWorkbook(t, [][]any{
		{"2026-01-04", "income", "Sunday offering", 1000, "Tithes", "cash"},
		{"2026-01-04", "expenditure", "New office line", 50, "Office New Head", "cash"},
	})
	recorder := uploadCombinedWorkbook(t, workbook)
	workbook.Close()

	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("all-types import status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	summary, ok := bulkResultStore.Get(summaryTokenFromRedirect(t, recorder.Header().Get("Location")))
	if !ok {
		t.Fatalf("all-types summary was not stored")
	}
	if summary.Imported != 2 {
		t.Fatalf("imported rows = %d, want 2 (%v)", summary.Imported, summary.Errors)
	}
	if len(summary.Created) != 1 || summary.Created[0] != "Office New Head" {
		t.Fatalf("created heads = %v, want Office New Head", summary.Created)
	}
	if got := countTransactionsForCategory(t, tithes); got != 1 {
		t.Fatalf("income rows = %d, want 1", got)
	}

	var headID int64
	if err := db.DB.QueryRow(
		`SELECT id FROM categories WHERE type = 'expenditure' AND name = 'Office New Head'`,
	).Scan(&headID); err != nil {
		t.Fatalf("load created head: %v", err)
	}
	if got := countTransactionsForCategory(t, headID); got != 1 {
		t.Fatalf("expenditure rows for the new head = %d, want 1", got)
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

func TestBulkImportKeepsIdenticalRowsInOneSheetButRefusesAReload(t *testing.T) {
	setupTestDB(t)
	stationery := categoryID(t, "expenditure", "Printing & Stationery")

	rows := [][]any{
		{"2026-01-04", "PLAN OF WORK", 456, "Printing & Stationery"},
		{"2026-01-04", "PLAN OF WORK", 456, "Printing & Stationery"},
	}

	first := expenseLedgerWorkbook(t, rows)
	recorder := uploadWorkbook(t, first, "expenditure")
	first.Close()
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("first upload status = %d", recorder.Code)
	}

	summary, ok := bulkResultStore.Get(summaryTokenFromRedirect(t, recorder.Header().Get("Location")))
	if !ok {
		t.Fatalf("first import summary was not stored")
	}
	if summary.Imported != 2 {
		t.Fatalf("first upload imported = %d, want both identical rows (%v)", summary.Imported, summary.Errors)
	}
	if got := countTransactionsForCategory(t, stationery); got != 2 {
		t.Fatalf("stationery rows after first upload = %d, want 2", got)
	}

	second := expenseLedgerWorkbook(t, rows)
	recorder = uploadWorkbook(t, second, "expenditure")
	second.Close()
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("second upload status = %d", recorder.Code)
	}

	summary, ok = bulkResultStore.Get(summaryTokenFromRedirect(t, recorder.Header().Get("Location")))
	if !ok {
		t.Fatalf("second import summary was not stored")
	}
	if summary.Imported != 0 {
		t.Fatalf("second upload imported = %d, want the reload refused (%v)", summary.Imported, summary.Errors)
	}
	if got := countTransactionsForCategory(t, stationery); got != 2 {
		t.Fatalf("stationery rows after re-upload = %d, want 2", got)
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
