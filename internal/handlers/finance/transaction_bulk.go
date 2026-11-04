package finance

import (
	"bytes"
	"fmt"
	"net/http"
	"path/filepath"
	"ramseyer-finance/internal/db"
	dataentrybulk "ramseyer-finance/internal/finance/dataentry/bulk"
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
var bulkResultStore = dataentrybulk.NewSummaryStore(dataentrybulk.ResultTTL, dataentrybulk.MaxResults, time.Now)

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

	transactionType := strings.TrimSpace(r.FormValue("type"))
	switch transactionType {
	case "income", "expenditure", "asset", "liability":
	default:
		badRequest(w, "Invalid transaction type")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		badRequest(w, "Excel file required")
		return
	}
	defer file.Close()

	// excelize reads the Office Open XML formats only. Accepting a legacy .xls here would
	// pass the extension check and then fail as an unreadable workbook, so the message names
	// exactly what this parser can actually open.
	switch strings.ToLower(filepath.Ext(header.Filename)) {
	case ".xlsx", ".xlsm":
	default:
		badRequest(w, "Only .xlsx or .xlsm files are supported")
		return
	}

	var buffer bytes.Buffer
	if _, err := buffer.ReadFrom(file); err != nil {
		badRequest(w, "Unable to read the uploaded file")
		return
	}
	workbook, err := excelize.OpenReader(bytes.NewReader(buffer.Bytes()))
	if err != nil {
		badRequest(w, "Unable to open the workbook")
		return
	}
	defer workbook.Close()

	sheetName, layout, rows, err := dataentrybulk.ChooseSheet(workbook, strings.TrimSpace(r.FormValue("sheet")), transactionType)
	if err != nil {
		badRequest(w, err.Error())
		return
	}

	categories, err := loadBulkCategories(transactionType)
	if err != nil {
		serverError(w, err)
		return
	}

	summary := dataentrybulk.ImportSummary{Sheet: sheetName}
	for index := layout.HeaderRow + 1; index < len(rows); index++ {
		row := rows[index]
		if dataentrybulk.RowIsBlank(row) {
			continue
		}

		// The client's books interleave subtotal and opening balance rows with real
		// entries. Posting those would inflate every report, so they are counted as
		// skipped and never reach the insert below.
		if dataentrybulk.RowIsSummary(row) {
			summary.Skipped++
			continue
		}

		dateValue, ok := dataentrybulk.DateValue(row, layout.Date)
		if !ok {
			summary.Skipped++
			summary.AppendRowError(index, "date is missing or not a real date")
			continue
		}

		amount, ok := dataentrybulk.AmountValue(row, layout.Amount)
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

		description := strings.TrimSpace(dataentrybulk.Cell(row, layout.Description))
		categoryLabel := strings.TrimSpace(dataentrybulk.Cell(row, layout.Category))
		categoryID, matched := dataentrybulk.MatchCategory(categories, categoryLabel)
		if !matched {
			// Uploading must never create an account as a side effect. An unknown label is
			// reported so the chart of accounts stays an explicit operator decision.
			summary.Skipped++
			if categoryLabel == "" {
				summary.AppendRowError(index, "category is missing; add a category column or name the account")
			} else {
				summary.AppendRowError(index, "category \""+categoryLabel+"\" does not match an existing account")
			}
			continue
		}

		categoryMeta, err := lookupTransactionCategoryMeta(categoryID, transactionType)
		if err != nil {
			summary.Skipped++
			summary.AppendRowError(index, "matched account is no longer available")
			continue
		}

		paymentMethod, err := normalizePaymentMethod(strings.TrimSpace(dataentrybulk.Cell(row, layout.Payment)))
		if err != nil {
			summary.Skipped++
			summary.AppendRowError(index, "payment method must be cash, momo, cheque, or bank")
			continue
		}

		// Running the same rule check as manual entry means uploading one workbook twice
		// refuses the second pass instead of double posting every row.
		if err := validateTransactionBusinessRules(0, dateValue, transactionType, categoryID, description, amount, false); err != nil {
			summary.Skipped++
			summary.AppendRowError(index, err.Error())
			continue
		}

		result, err := db.DB.Exec(
			`INSERT INTO transactions (date, type, category, category_id, payment_method, description, amount, note_ref, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now','localtime'))`,
			dateValue,
			transactionType,
			categoryMeta.Name,
			categoryID,
			paymentMethod,
			description,
			amount,
			categoryMeta.NoteRef,
		)
		if err != nil {
			summary.Skipped++
			summary.AppendRowError(index, "could not be saved")
			continue
		}

		// Bulk rows share the audit trail with typed rows so the register can still explain
		// where a posting came from after an import.
		if transactionID, idErr := result.LastInsertId(); idErr == nil {
			if snapshot, snapshotErr := loadTransactionSnapshot(transactionID); snapshotErr == nil {
				_ = recordTransactionAudit(transactionsaudit.ActionCreated, snapshot)
			}
		}
		summary.Imported++
	}

	token := bulkResultStore.Put(summary)
	http.Redirect(w, r, "/data-entry?bulk="+token+"&tab="+transactionType, http.StatusSeeOther)
}

// loadBulkCategories reads every active account of one type once, so a three hundred row
// sheet does not issue three hundred lookups against the same table.
func loadBulkCategories(transactionType string) ([]dataentrybulk.Category, error) {
	rows, err := db.DB.Query(
		`SELECT id, name FROM categories WHERE type = ? AND COALESCE(is_active, 1) = 1 ORDER BY id`,
		transactionType,
	)
	if err != nil {
		return nil, fmt.Errorf("load categories for bulk import: %w", err)
	}
	defer rows.Close()

	var categories []dataentrybulk.Category
	for rows.Next() {
		var category dataentrybulk.Category
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
