package finance

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"ramseyer-finance/internal/db"
	transactionsaudit "ramseyer-finance/internal/transactions/audit"
	"strconv"
	"strings"
	"time"
)

// CatOption represents a single selectable category in a dropdown or list. It carries the
// database ID (used as the form value), the display name, and an indentation level for
// visual hierarchy in the UI. Categories with Indent=1 are subcategories and should be
// rendered with a visual indent (e.g., a "--" prefix or left padding) to distinguish them
// from top-level parent categories. This simple two-level model matches the application's
// category hierarchy where subcategories have a parent_id referencing a top-level category.
type CatOption struct {
	ID     int64
	Name   string
	Indent int
}

// DataEntryPage serves the data entry page with a manual form and an Excel upload for each
// transaction type.
func DataEntryPage(w http.ResponseWriter, r *http.Request) {
	data, err := buildDashboardData(time.Now(), "data-entry", r.URL.Query().Get("msg"), "")
	if err != nil {
		serverError(w, err)
		return
	}
	// The bulk importer redirects with the type it just imported. Reconciling it here keeps
	// the page on the same tab after the round trip, which is where the operator needs to
	// read the import report.
	data.ActiveTab = dataEntryTab(r.URL.Query().Get("tab"))
	data.DataEntryExcel = dataEntryExcelControls(data.CurrentYear)
	// A rejected form redirects here with tone=warning so the message is not styled like a
	// saved entry. Anything else keeps the default success tone from the shared builder.
	if r.URL.Query().Get("tone") == "warning" {
		data.MessageTone = "warning"
	}
	if summary, ok := bulkResultStore.Get(r.URL.Query().Get("bulk")); ok {
		data.BulkImport = &summary
	}
	RenderTemplate(w, "data-entry", data)
}

// dataEntryExcelControls builds the upload, template, and export links for each type. Only
// expenditure has a saved-workbook export because that is the client workbook the church
// office shares; the other types export through the transaction register.
func dataEntryExcelControls(year int) map[string]DataEntryExcelControl {
	controls := make(map[string]DataEntryExcelControl, 4)
	for _, entry := range []struct {
		Type       string
		Label      string
		LowerLabel string
	}{
		{Type: "income", Label: "Income", LowerLabel: "income"},
		{Type: "expenditure", Label: "Expenditure", LowerLabel: "expenditure"},
		{Type: "asset", Label: "Asset", LowerLabel: "asset"},
		{Type: "liability", Label: "Liability", LowerLabel: "liability"},
	} {
		control := DataEntryExcelControl{
			Type:        entry.Type,
			Label:       entry.Label,
			LowerLabel:  entry.LowerLabel,
			Year:        year,
			TemplateURL: fmt.Sprintf("/api/data-entry/template?type=%s&year=%d", entry.Type, year),
		}
		if entry.Type == "expenditure" {
			control.ExportURL = fmt.Sprintf("/api/data-entry/export?type=expenditure&year=%d", year)
		}
		controls[entry.Type] = control
	}
	return controls
}

// dataEntryTab normalises the requested tab so an unknown query value cannot leave every
// panel hidden.
func dataEntryTab(requested string) string {
	tab := strings.TrimSpace(requested)
	switch tab {
	case "expenditure", "asset", "liability":
		return tab
	default:
		return "income"
	}
}

// AddTransaction handles POST requests to create a new transaction. It validates the form
// fields (type, date, category, amount) with the same rules applied to updates, resolves
// the canonical category metadata from the database (name and note_ref) so that the stored
// transaction always reflects the authoritative category data rather than whatever label
// the form submitted, enforces business rules (duplicate detection, description
// requirements for asset/liability entries), inserts the row with an initial updated_at
// timestamp, records an audit entry with the full inserted snapshot, and redirects back
// to the return-to URL with a success or error message. The return-to URL is sanitised to
// prevent open redirect attacks, falling back to the data entry page if the provided value
// is not a relative in-app path.
func AddTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		badRequest(w, "Invalid form submission")
		return
	}

	// I validate the transaction type first because it determines which category namespace,
	// business rules, and reporting path this posting belongs to.
	// The transaction type must be one of the four known types. This whitelist approach
	// prevents invalid types from reaching the database and ensures the category lookup
	// in the next step is scoped to the correct type namespace.
	transactionType := strings.TrimSpace(r.FormValue("type"))
	switch transactionType {
	case "income", "expenditure", "asset", "liability":
	default:
		badRequest(w, "Invalid transaction type")
		return
	}

	transactionDate, err := parseTransactionDate(r.FormValue("date"))
	if err != nil {
		badRequest(w, "Invalid transaction date")
		return
	}

	categoryID, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("category_id")), 10, 64)
	if err != nil || categoryID <= 0 {
		badRequest(w, "Invalid category")
		return
	}
	paymentMethod, err := normalizePaymentMethod(r.FormValue("payment_method"))
	if err != nil {
		badRequest(w, "Invalid payment method")
		return
	}

	amount, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue("amount")), 64)
	if err != nil || amount == 0 || ((transactionType == "income" || transactionType == "expenditure") && amount < 0) {
		badRequest(w, transactionAmountValidationMessage(transactionType))
		return
	}

	description := strings.TrimSpace(r.FormValue("description"))
	// The allow_duplicate flag is a hidden checkbox that the user can enable after the
	// frontend displays a duplicate warning. It is passed through from the confirmation
	// dialog and tells the business rules validator to skip duplicate detection.
	allowDuplicate := strings.TrimSpace(r.FormValue("allow_duplicate")) != ""
	returnTo := sanitizeReturnTo(r.FormValue("return_to"), "/data-entry?open=transaction-entry")

	// I resolve category metadata from the database instead of trusting form labels because the
	// stored transaction should inherit the canonical category name and note mapping.
	// Look up the category's canonical name, parent name, and note_ref from the database.
	// This ensures the denormalised category and note_ref columns in the transactions
	// table always match the categories table, even if the form submitted stale or
	// tampered data. If the category doesn't exist or doesn't match the transaction type,
	// we reject the submission rather than storing inconsistent data.
	categoryMeta, err := lookupTransactionCategoryMeta(categoryID, transactionType)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			badRequest(w, "Selected category does not match the transaction type")
			return
		}
		serverError(w, err)
		return
	}
	// Run the same business rule validation used for updates. The transactionID of 0
	// tells the validator this is a new transaction (so the duplicate check should
	// not exclude any existing row). If validation fails, the error message is
	// appended to the return-to URL as a query parameter so the frontend can display
	// it alongside the form for correction.
	if err := validateTransactionBusinessRules(0, transactionDate, transactionType, categoryID, description, amount, allowDuplicate); err != nil {
		separator := "?"
		if strings.Contains(returnTo, "?") {
			separator = "&"
		}
		http.Redirect(w, r, returnTo+separator+"msg="+queryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	// Insert the transaction with all fields, including the denormalised category name
	// and note_ref from the resolved metadata. The updated_at timestamp is set to the
	// current local time so that brand-new transactions have a meaningful value in that
	// column from the start, not just after their first edit.
	// The visible payment method is only a classification flag. The transaction category and
	// amount are saved once so no report can manufacture a second deduction the operator never entered.
	result, err := db.DB.Exec(
		`INSERT INTO transactions (date, type, category, category_id, payment_method, description, amount, note_ref, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now','localtime'))`,
		transactionDate,
		transactionType,
		categoryMeta.Name,
		categoryID,
		paymentMethod,
		description,
		amount,
		categoryMeta.NoteRef,
	)
	if err != nil {
		serverError(w, err)
		return
	}
	// I record a create audit after insert so the history reflects the final normalized stored row,
	// including category metadata supplied by the database.
	// After a successful insert, retrieve the new row's auto-incremented ID, load the
	// full snapshot (which includes the database-generated timestamps and the resolved
	// category data), and record an audit entry. If the snapshot load fails, the audit
	// is silently skipped—the transaction was still created successfully, and a missing
	// audit entry is preferable to rolling back or returning an error to the user.
	transactionID, err := result.LastInsertId()
	if err == nil {
		if snapshot, snapshotErr := loadTransactionSnapshot(transactionID); snapshotErr == nil {
			_ = recordTransactionAudit(transactionsaudit.ActionCreated, snapshot)
		}
	}

	separator := "?"
	if strings.Contains(returnTo, "?") {
		separator = "&"
	}
	http.Redirect(w, r, returnTo+separator+"msg=Entry+saved.+Trial+Balance+and+reports+recalculated", http.StatusSeeOther)
}
