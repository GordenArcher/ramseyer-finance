package finance

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"ramseyer-finance/internal/db"
	"ramseyer-finance/internal/finance/dataentry/category"
	expenseheadparser "ramseyer-finance/internal/finance/dataentry/expense/head"
	"ramseyer-finance/internal/finance/dataentry/report"
	rowdata "ramseyer-finance/internal/finance/dataentry/row"
	"ramseyer-finance/internal/finance/dataentry/sheet"
	"ramseyer-finance/internal/finance/dataentry/value"
	"ramseyer-finance/internal/handlers/finance/expensehead"
	transactionsaudit "ramseyer-finance/internal/transactions/audit"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

const (
	// uploadMemoryLimit is the multipart ceiling for an uploaded workbook. A year of typed
	// entries is well under this, so anything larger is a mistake worth refusing early.
	uploadMemoryLimit = 32 << 20
)

// bulkResultStore is the process-wide store the import handler and page handler share.
var bulkResultStore = report.NewSummaryStore(report.ResultTTL, report.MaxResults, time.Now)

// BulkImportTransaction imports dated entries from an uploaded workbook.
//
// The parser is header driven rather than position driven. The client's ledger puts the
// description before the amount and carries helper columns for cheque numbers and running
// balances, so fixed column positions would read the wrong cell on every row. Reusing
// the same insert, duplicate check, and audit path as manual entry keeps an imported
// posting indistinguishable from one typed on screen.
func BulkImportTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(uploadMemoryLimit); err != nil {
		badRequest(w, "Invalid form submission")
		return
	}

	transactionType, ok := normalizeBulkTransactionType(r.FormValue("type"))
	if !ok {
		badRequest(w, "Invalid transaction type")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		badRequest(w, "Excel file required")
		return
	}
	defer file.Close()

	workbook, err := openUploadedWorkbook(file, header)
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	defer workbook.Close()

	sheet, err := parseBulkSheet(workbook, strings.TrimSpace(r.FormValue("sheet")), transactionType)
	if err != nil {
		badRequest(w, err.Error())
		return
	}

	summary, err := importParsedSheet(transactionType, sheet)
	if err != nil {
		serverError(w, err)
		return
	}

	token := bulkResultStore.Put(summary)
	http.Redirect(w, r, "/data-entry?bulk="+token+"&tab="+transactionType, http.StatusSeeOther)
}

// normalizeBulkTransactionType keeps the whitelist in one place. Both upload handlers and
// any future caller must agree on the four namespaces, because the category lookup and the
// business rules are keyed by them.
func normalizeBulkTransactionType(raw string) (string, bool) {
	transactionType := strings.TrimSpace(raw)
	switch transactionType {
	case "income", "expenditure", "asset", "liability":
		return transactionType, true
	default:
		return "", false
	}
}

// openUploadedWorkbook reads one multipart file into a workbook. Every failure here is
// caused by the file the operator chose, so the returned error is already phrased for the
// page instead of being wrapped for the log.
func openUploadedWorkbook(file multipart.File, header *multipart.FileHeader) (*excelize.File, error) {
	// excelize reads the Office Open XML formats only. Accepting a legacy .xls here would
	// pass the extension check and then fail as an unreadable workbook, so the message names
	// exactly what this parser can actually open.
	switch strings.ToLower(filepath.Ext(header.Filename)) {
	case ".xlsx", ".xlsm":
	default:
		return nil, fmt.Errorf("only .xlsx or .xlsm files are supported")
	}

	var buffer bytes.Buffer
	if _, err := buffer.ReadFrom(file); err != nil {
		return nil, fmt.Errorf("unable to read the uploaded file")
	}
	workbook, err := excelize.OpenReader(bytes.NewReader(buffer.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("unable to open the workbook")
	}
	return workbook, nil
}

// parsedSheet is one worksheet resolved into the layout the importer understands. Both the
// single-type upload and the all-types upload hand this to importParsedSheet, so the row
// rules stay identical no matter how many files the operator submitted at once.
type parsedSheet struct {
	Name   string
	Layout sheet.ColumnLayout
	Rows   [][]string
}

// parseBulkSheet resolves the worksheet and layout for one type. Errors returned here come
// from the uploaded workbook, so the handler can show them to the operator directly.
func parseBulkSheet(workbook *excelize.File, requestedSheet, transactionType string) (parsedSheet, error) {
	name, layout, rows, err := sheet.ChooseSheet(workbook, requestedSheet, transactionType)
	if err != nil {
		return parsedSheet{}, err
	}
	return parsedSheet{Name: name, Layout: layout, Rows: rows}, nil
}

// importParsedSheet writes the parsed rows through the same insert, duplicate, and audit
// path as manual entry. Errors returned here are internal failures, not row-level refusals;
// row-level refusals are recorded in the summary so one bad row never discards the rest.
func importParsedSheet(transactionType string, sheet parsedSheet) (report.ImportSummary, error) {
	categories, err := loadBulkCategories(transactionType)
	if err != nil {
		return report.ImportSummary{}, err
	}

	// Rows already in the ledger keep their ids below this boundary. The save path uses it
	// to refuse a repeated upload without refusing two identical lines that the workbook
	// itself contains.
	baselineID, err := lastTransactionID()
	if err != nil {
		return report.ImportSummary{}, err
	}

	summary := report.ImportSummary{Sheet: sheet.Name}
	for index := sheet.Layout.HeaderRow + 1; index < len(sheet.Rows); index++ {
		row := sheet.Rows[index]
		if rowdata.IsBlank(row) {
			continue
		}

		// The client's books interleave subtotal and opening balance rows with real
		// entries. Posting those would inflate every report, so they are counted as
		// skipped and never reach the insert below.
		if rowdata.IsSummary(row) {
			summary.Skipped++
			continue
		}

		dateValue, ok := value.DateValue(row, sheet.Layout.Date)
		if !ok {
			summary.Skipped++
			summary.AppendRowError(index, "date is missing or not a real date")
			continue
		}

		amount, ok := value.AmountValue(row, sheet.Layout.Amount)
		if !ok {
			summary.Skipped++
			summary.AppendRowError(index, "amount is missing or not a number")
			continue
		}
		// Income and expenditure read as positive receipts and payments. A negative value
		// there means either a credit note or a wrong column, and both need a human decision.
		if (transactionType == "income" || transactionType == "expenditure") && amount <= 0 {
			summary.Skipped++
			summary.AppendRowError(index, "income and expenditure amounts must be greater than zero")
			continue
		}
		if amount == 0 {
			summary.Skipped++
			summary.AppendRowError(index, "amount must not be zero")
			continue
		}

		description := strings.TrimSpace(rowdata.Cell(row, sheet.Layout.Description))
		categoryLabel := strings.TrimSpace(rowdata.Cell(row, sheet.Layout.Category))
		paymentMethod := strings.TrimSpace(rowdata.Cell(row, sheet.Layout.Payment))
		result := saveImportedRow(transactionType, dateValue, description, categoryLabel, paymentMethod, amount, categories, baselineID)
		if result.Reason != "" {
			summary.Skipped++
			summary.AppendRowError(index, result.Reason)
			continue
		}
		// A head that was not in the loaded slice is now a real account. Adding it here
		// keeps the next row with the same head on the in-memory match instead of asking
		// the database again for every line in the workbook.
		if result.ResolvedID != 0 {
			categories = append(categories, category.Category{ID: result.ResolvedID, Name: result.ResolvedName})
		}
		summary.AppendCreated(result.AddedHead)
		summary.Imported++
	}

	return summary, nil
}

// importedRowResult is the outcome of saving one parsed row. Reason is empty when the row
// was saved. AddedHead names an expense head this row created, while ResolvedID carries a
// head that already existed but was missing from the loaded category slice. Keeping the two
// apart lets the report list each new account once without confusing it with a cache miss.
type importedRowResult struct {
	Reason       string
	AddedHead    string
	ResolvedID   int64
	ResolvedName string
}

// saveImportedRow validates and inserts one parsed row. An empty Reason means the row was
// saved; anything else is the operator-facing reason it was refused. Keeping the insert,
// duplicate, and audit rules in one function is what stops the per-type upload and the
// all-types upload from posting different results for the same row.
func saveImportedRow(transactionType, dateValue, description, categoryLabel, paymentMethod string, amount float64, categories []category.Category, baselineID int64) importedRowResult {
	categoryLabel = strings.TrimSpace(categoryLabel)
	// Expenditure heads are the office's own shorthand, so they get the spelling rules and
	// the create-on-missing behaviour. The other three types keep the strict chart match
	// because their accounts are structural, not free text from a workbook.
	if transactionType == "expenditure" {
		categoryLabel = expenseheadparser.Normalize(categoryLabel)
	}

	// The client's main expenditure book carries 11 GHS 1,000 IMPREST lines. Those are float
	// top-ups, not payments: the imprest cash book already records what the float bought.
	// Refusing the label here stops the top-up from being counted as expenditure if someone
	// later adds an IMPREST account to the chart.
	if transactionType == "expenditure" && strings.EqualFold(categoryLabel, "imprest") {
		return importedRowResult{Reason: "IMPREST lines are float top-ups, not expenditure; upload the imprest cash book on the Imprest Expenses page"}
	}

	categoryID, matched := category.MatchCategory(categories, categoryLabel)
	var result importedRowResult
	if !matched {
		if categoryLabel == "" {
			return importedRowResult{Reason: "category is missing; add a category column or name the account"}
		}
		if transactionType != "expenditure" {
			// Other types must never create an account as a side effect. An unknown label is
			// reported so the chart of accounts stays an explicit operator decision.
			return importedRowResult{Reason: "category \"" + categoryLabel + "\" does not match an existing account"}
		}

		headID, created, err := expensehead.Ensure(categoryLabel)
		if err != nil {
			return importedRowResult{Reason: "the expense head \"" + categoryLabel + "\" could not be added"}
		}
		categoryID = headID
		result.ResolvedID = headID
		result.ResolvedName = categoryLabel
		if created {
			result.AddedHead = categoryLabel
		}
	}

	categoryMeta, err := lookupTransactionCategoryMeta(categoryID, transactionType)
	if err != nil {
		return importedRowResult{Reason: "matched account is no longer available"}
	}

	normalizedPayment, err := normalizePaymentMethod(strings.TrimSpace(paymentMethod))
	if err != nil {
		return importedRowResult{Reason: "payment method must be cash, momo, cheque, or bank"}
	}

	// Running the same rule check as manual entry means uploading one workbook twice
	// refuses the second pass instead of double posting every row. The baseline keeps two
	// identical lines inside the uploaded workbook from being treated as that second pass.
	if err := validateImportedRow(dateValue, transactionType, categoryID, description, amount, baselineID); err != nil {
		return importedRowResult{Reason: err.Error()}
	}

	inserted, err := db.DB.Exec(
		`INSERT INTO transactions (date, type, category, category_id, payment_method, description, amount, note_ref, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now','localtime'))`,
		dateValue,
		transactionType,
		categoryMeta.Name,
		categoryID,
		normalizedPayment,
		description,
		amount,
		categoryMeta.NoteRef,
	)
	if err != nil {
		return importedRowResult{Reason: "could not be saved"}
	}

	// Bulk rows share the audit trail with typed rows so the register can still explain
	// where a posting came from after an import.
	if transactionID, idErr := inserted.LastInsertId(); idErr == nil {
		if snapshot, snapshotErr := loadTransactionSnapshot(transactionID); snapshotErr == nil {
			_ = recordTransactionAudit(transactionsaudit.ActionCreated, snapshot)
		}
	}
	return result
}

// loadBulkCategories reads every active account of one type once, so a three hundred row
// sheet does not issue three hundred lookups against the same table.
func loadBulkCategories(transactionType string) ([]category.Category, error) {
	rows, err := db.DB.Query(
		`SELECT id, name FROM categories WHERE type = ? AND COALESCE(is_active, 1) = 1 ORDER BY id`,
		transactionType,
	)
	if err != nil {
		return nil, fmt.Errorf("load categories for bulk import: %w", err)
	}
	defer rows.Close()

	var categories []category.Category
	for rows.Next() {
		var category category.Category
		if err := rows.Scan(&category.ID, &category.Name); err != nil {
			return nil, fmt.Errorf("scan bulk import category: %w", err)
		}
		categories = append(categories, category)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bulk import categories: %w", err)
	}
	return categories, nil
}
