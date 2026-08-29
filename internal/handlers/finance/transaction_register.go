package finance

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"ramseyer-finance/internal/db"
	"strconv"
	"strings"
	"time"
)

// transactionsPageSize controls how many transaction rows appear on a single register
// page. The value is kept moderate (25) to balance between showing enough rows for a
// useful overview and keeping page load times and server memory usage low. Pagination
// happens in SQL via LIMIT/OFFSET, so only the visible rows are loaded into memory
// regardless of the total dataset size.
const transactionsPageSize = 25

// TransactionsData carries all the template variables for the transaction register page.
// It includes the current filter state (year, type, search query), pagination metadata
// (current page, total pages, previous/next URLs, page number links), the list of visible
// transaction rows, the recent audit trail entries for the activity sidebar, the full
// category choice list for the edit modal dropdown, and—when the "edit" query parameter is
// present—the pre-populated edit form data for the targeted transaction. The ReturnTo
// field preserves the current filtered view URL so that after an edit or delete, the user
// is redirected back to the same page and filter state they were viewing.
type TransactionsData struct {
	Active          string
	Message         string
	MessageTone     string
	Year            string
	Years           []int
	Page            int
	TotalPages      int
	TotalCount      int
	HasPrev         bool
	HasNext         bool
	PrevPageURL     string
	NextPageURL     string
	PageLinks       []PageLink
	FilterType      string
	Search          string
	ExportStartDate string
	ExportEndDate   string
	ReturnTo        string
	Transactions    []TransactionRow
	AuditEntries    []TransactionAuditEntry
	CategoryChoices []CategoryChoice
	EditForm        EditableTransaction
}

// TransactionRow represents a single row in the transaction register table. It includes
// the resolved top-level category name (via parent join) and the direct category name,
// plus a pre-computed DisplayLabel that shows the hierarchical path (e.g., "Offering /
// Children Service") for flat display. The NoteRef field carries the accounting reference
// code for category-based reporting. All text fields are COALESCE'd to empty strings at
// query time so the template never receives NULL values.
type TransactionRow struct {
	ID            int64
	Date          string
	Type          string
	TopCategory   string
	Category      string
	PaymentMethod string
	NoteRef       string
	Description   string
	Amount        float64
	DisplayLabel  string
}

// EditableTransaction holds the current values of a transaction being edited, loaded from
// the database when the "edit" query parameter is present. The Loaded flag distinguishes
// between an intentionally empty form (no edit requested) and a failed lookup, allowing
// the template to conditionally render the edit modal. All fields are the raw database
// values without category metadata resolution—the edit form's category dropdown is
// populated separately via the CategoryChoices list.
type EditableTransaction struct {
	Loaded        bool
	ID            int64
	Date          string
	Type          string
	CategoryID    int64
	PaymentMethod string
	Description   string
	Amount        float64
}

// PageLink describes a single numbered page button in the pagination control. Number is
// the page number for display, URL is the fully-constructed link to that page (preserving
// all current filters), and Active is true for the currently-viewed page (which should be
// styled differently, typically as a filled or highlighted button).
type PageLink struct {
	Number int
	URL    string
	Active bool
}

// TransactionsPage serves the main transaction register. It parses the year, type filter,
// search query, and page number from the request, loads the paginated transaction rows
// along with the shared category choices and audit trail, and renders the full register
// template. If the "edit" query parameter is present with a valid transaction ID, it also
// loads that transaction's current values and populates the edit form modal. This single
// handler consolidates the list view, filtering, pagination, and edit-in-place workflow
// into one page, avoiding modal popups that require separate AJAX endpoints.
func TransactionsPage(w http.ResponseWriter, r *http.Request) {
	// I treat the register as the operational source of truth for review and correction. The
	// dashboard is summary-first, but this page owns the detailed transaction workflow.
	year, err := parseReportYear(r.URL.Query().Get("year"))
	if err != nil {
		badRequest(w, err.Error())
		return
	}

	// Validate the type filter against the four known transaction types plus the empty
	// "all types" option. This whitelist approach prevents arbitrary SQL fragments from
	// reaching the query builder, even though the value is parameterised.
	filterType := strings.TrimSpace(r.URL.Query().Get("type"))
	switch filterType {
	case "", "income", "expenditure", "asset", "liability":
	default:
		badRequest(w, "invalid transaction type filter")
		return
	}

	search := strings.TrimSpace(r.URL.Query().Get("q"))
	page := parsePageNumber(r.URL.Query().Get("page"))
	years, err := reportYears(year)
	if err != nil {
		serverError(w, err)
		return
	}

	rows, totalCount, err := loadTransactionRows(year, filterType, search, page, transactionsPageSize)
	if err != nil {
		serverError(w, err)
		return
	}

	// I load category choices and audit entries alongside the rows so the page can support editing
	// and change visibility without forcing the user into another screen.
	// Category choices are loaded once per page render (not per row) because the edit
	// modal's dropdown needs the complete list, and the list is small enough (under 100
	// items) that caching or lazy-loading is unnecessary.
	choices, err := loadAllCategoryChoices()
	if err != nil {
		serverError(w, err)
		return
	}
	// Recent audit entries provide an activity sidebar showing the last 12 changes
	// (creates, updates, deletes) across all transactions, giving visibility into
	// who changed what and when.
	auditEntries, err := loadRecentTransactionAuditEntries(12)
	if err != nil {
		serverError(w, err)
		return
	}

	startDate, endDate := exportDateDefaults(year)
	totalPages := totalPagesForCount(totalCount, transactionsPageSize)
	// Build the return-to URL that encodes the current filter state. After an edit or
	// delete, the user is redirected back to this exact URL so they return to the same
	// page, filter, and search state they were viewing.
	returnTo := buildTransactionsURL(year, filterType, search, page)
	data := TransactionsData{
		Active:          "transactions",
		Message:         r.URL.Query().Get("msg"),
		MessageTone:     alertTone(r.URL.Query().Get("msg")),
		Year:            strconv.Itoa(year),
		Years:           years,
		Page:            page,
		TotalPages:      totalPages,
		TotalCount:      totalCount,
		HasPrev:         page > 1,
		HasNext:         page < totalPages,
		FilterType:      filterType,
		Search:          search,
		ExportStartDate: startDate,
		ExportEndDate:   endDate,
		ReturnTo:        returnTo,
		Transactions:    rows,
		AuditEntries:    auditEntries,
		CategoryChoices: choices,
	}
	// Previous and next page URLs are only populated when there is a valid page to
	// navigate to. This avoids generating links to page 0 or page beyond the last,
	// keeping the pagination controls clean.
	if data.HasPrev {
		data.PrevPageURL = buildTransactionsURL(year, filterType, search, page-1)
	}
	if data.HasNext {
		data.NextPageURL = buildTransactionsURL(year, filterType, search, page+1)
	}
	data.PageLinks = buildTransactionPageLinks(year, filterType, search, page, totalPages)

	// If an edit is requested, load the target transaction's current values. A missing
	// or invalid edit ID is treated as a client error with a redirect back to the
	// register (preserving filters) rather than a server error, because it typically
	// represents a stale or malformed link.
	if editID := strings.TrimSpace(r.URL.Query().Get("edit")); editID != "" {
		id, err := strconv.ParseInt(editID, 10, 64)
		if err != nil || id <= 0 {
			badRequest(w, "invalid transaction edit target")
			return
		}
		record, err := loadEditableTransaction(id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				redirectWithMessage(w, r, data.ReturnTo, "Transaction not found")
				return
			}
			serverError(w, err)
			return
		}
		data.EditForm = record
	}

	RenderTemplate(w, "transactions", data)
}

// UpdateTransaction handles POST requests to update an existing transaction. It validates
// all form fields (ID, type, date, category, amount) with the same business rules applied
// to new transaction creation, looks up the resolved category metadata to keep the
// denormalised category and note_ref columns in sync, executes the UPDATE, refreshes the
// updated_at timestamp, records an audit entry with the new state, and redirects back to
// the register with a success message. The function uses the "return_to" form field to
// preserve the user's filter state across the redirect.
func UpdateTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		badRequest(w, "Invalid form submission")
		return
	}

	transactionID, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("id")), 10, 64)
	if err != nil || transactionID <= 0 {
		badRequest(w, "Invalid transaction")
		return
	}

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

	// I run the same business-rule validation on edits as on creates because a transaction should
	// not become invalid simply because it was modified later.
	// The allow_duplicate flag is passed through from the edit form's hidden field,
	// allowing the user to explicitly override the duplicate detection warning from
	// the frontend confirmation dialog.
	allowDuplicate := strings.TrimSpace(r.FormValue("allow_duplicate")) != ""
	categoryMeta, err := lookupTransactionCategoryMeta(categoryID, transactionType)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			badRequest(w, "Selected category does not match the transaction type")
			return
		}
		serverError(w, err)
		return
	}
	if err := validateTransactionBusinessRules(transactionID, transactionDate, transactionType, categoryID, strings.TrimSpace(r.FormValue("description")), amount, allowDuplicate); err != nil {
		redirectWithMessage(w, r, sanitizeReturnTo(r.FormValue("return_to"), "/transactions"), err.Error())
		return
	}
	if _, err := lookupExistingTransaction(transactionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			redirectWithMessage(w, r, sanitizeReturnTo(r.FormValue("return_to"), "/transactions"), "Transaction not found")
			return
		}
		serverError(w, err)
		return
	}
	// Update both the foreign key (category_id) and the denormalised columns (category,
	// note_ref) in one statement. Keeping the denormalised columns in sync avoids JOINs
	// on every read query while the category_id maintains referential integrity and
	// enables the backfill/sync maintenance operations.
	result, err := db.DB.Exec(`
		UPDATE transactions
		SET date = ?, type = ?, category = ?, category_id = ?, payment_method = ?, note_ref = ?, description = ?, amount = ?, updated_at = datetime('now','localtime')
		WHERE id = ?
	`, transactionDate, transactionType, categoryMeta.Name, categoryID, paymentMethod, categoryMeta.NoteRef, strings.TrimSpace(r.FormValue("description")), amount, transactionID)
	if err != nil {
		serverError(w, err)
		return
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		serverError(w, err)
		return
	}
	if rowsAffected == 0 {
		redirectWithMessage(w, r, sanitizeReturnTo(r.FormValue("return_to"), "/transactions"), "Transaction not found")
		return
	}
	// Load the full updated row for the audit trail. The snapshot includes all columns
	// so the audit entry is a complete record of the transaction's state after the edit.
	if snapshot, err := loadTransactionSnapshot(transactionID); err == nil {
		_ = recordTransactionAudit("updated", snapshot)
	}

	redirectWithMessage(w, r, sanitizeReturnTo(r.FormValue("return_to"), "/transactions"), "Entry updated and reports recalculated")
}

// DeleteTransaction handles POST requests to delete a transaction. It loads the full
// transaction snapshot before deletion (for the audit trail), executes the DELETE, records
// an audit entry with the deleted row's data, and redirects back to the register. The
// pre-delete snapshot ensures the audit log preserves a complete record of what was
// removed, including all category metadata and amounts, even after the row is gone.
func DeleteTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		badRequest(w, "Invalid form submission")
		return
	}

	// I capture the existing row before delete so the audit trail still has a complete record of
	// what was removed.
	transactionID, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("id")), 10, 64)
	if err != nil || transactionID <= 0 {
		badRequest(w, "Invalid transaction")
		return
	}

	// Load the full row before deletion. If the row doesn't exist (perhaps because
	// another tab or user already deleted it), redirect with a message rather than
	// returning a 500 error.
	snapshot, err := lookupExistingTransaction(transactionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			redirectWithMessage(w, r, sanitizeReturnTo(r.FormValue("return_to"), "/transactions"), "Transaction not found")
			return
		}
		serverError(w, err)
		return
	}

	if _, err := db.DB.Exec(`DELETE FROM transactions WHERE id = ?`, transactionID); err != nil {
		serverError(w, err)
		return
	}
	_ = recordTransactionAudit("deleted", snapshot)

	redirectWithMessage(w, r, sanitizeReturnTo(r.FormValue("return_to"), "/transactions"), "Entry deleted and reports recalculated")
}

// loadTransactionRows queries the transactions table with the given filters and pagination
// parameters, returning the visible rows for the current page and the total matching row
// count. The total count is computed with a separate COUNT(*) query using the same WHERE
// clause as the data query, ensuring the pagination metadata is always consistent with the
// visible results—even when filters change. Category metadata is resolved via LEFT JOINs
// so that transactions with missing or deleted categories still appear with their original
// text values as fallbacks. The display label is pre-computed for each row so the template
// can render it without string concatenation logic.
func loadTransactionRows(year int, filterType, search string, page, pageSize int) ([]TransactionRow, int, error) {
	// I paginate in SQL because the register is the one list that can grow without bound in normal
	// use. Pulling everything into memory first would age badly.
	startDate, endDate := yearBounds(year)
	whereArgs := []interface{}{startDate, endDate}

	// Build the WHERE clause dynamically, accumulating both the SQL fragment and the
	// corresponding parameter values. The same WHERE clause is used for both the COUNT
	// and the data query, ensuring they operate on identical row sets.
	var where strings.Builder
	where.WriteString(` WHERE t.date >= ? AND t.date < ?`)

	// I reuse one filter predicate for both count and row queries so pagination math and visible
	// rows can never disagree about what the current filter means.
	if filterType != "" {
		where.WriteString(` AND t.type = ?`)
		whereArgs = append(whereArgs, filterType)
	}
	// Search includes the visible money-movement flag so operators can quickly isolate all
	// Cash, Momo, Cheque, or Bank transactions without understanding internal accounts.
	if search != "" {
		pattern := "%" + strings.ToLower(search) + "%"
		where.WriteString(`
			AND (
				LOWER(COALESCE(category.name, t.category)) LIKE ?
				OR LOWER(COALESCE(parent.name, '')) LIKE ?
				OR LOWER(COALESCE(t.payment_method, 'cash')) LIKE ?
				OR LOWER(COALESCE(t.description, '')) LIKE ?
			)
		`)
		whereArgs = append(whereArgs, pattern, pattern, pattern, pattern)
	}

	countQuery := `
		SELECT COUNT(*)
		FROM transactions t
		LEFT JOIN categories category ON category.id = t.category_id
		LEFT JOIN categories parent ON parent.id = category.parent_id
	` + where.String()

	var totalCount int
	if err := db.DB.QueryRow(countQuery, whereArgs...).Scan(&totalCount); err != nil {
		return nil, 0, fmt.Errorf("count transactions: %w", err)
	}

	// Calculate the OFFSET from the page number and page size. Page 1 starts at offset
	// 0. The LIMIT and OFFSET are appended as the last two parameters to avoid
	// interfering with the WHERE clause parameter ordering.
	offset := (page - 1) * pageSize
	args := append([]interface{}{}, whereArgs...)
	args = append(args, pageSize, offset)
	query := `
		SELECT
			t.id,
			t.date,
			t.type,
			COALESCE(parent.name, category.name, t.category) AS top_category,
			COALESCE(category.name, t.category) AS category_name,
			COALESCE(category.note_ref, t.note_ref, '') AS note_ref,
			COALESCE(NULLIF(t.payment_method, ''), 'cash') AS payment_method,
			COALESCE(t.description, ''),
			t.amount
		FROM transactions t
		LEFT JOIN categories category ON category.id = t.category_id
		LEFT JOIN categories parent ON parent.id = category.parent_id
	` + where.String() + `
		ORDER BY t.date DESC, t.id DESC
		LIMIT ? OFFSET ?
	`

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query transactions: %w", err)
	}
	defer rows.Close()

	var transactions []TransactionRow
	for rows.Next() {
		var row TransactionRow
		if err := rows.Scan(
			&row.ID,
			&row.Date,
			&row.Type,
			&row.TopCategory,
			&row.Category,
			&row.NoteRef,
			&row.PaymentMethod,
			&row.Description,
			&row.Amount,
		); err != nil {
			return nil, 0, fmt.Errorf("scan transaction row: %w", err)
		}
		// Pre-compute the display label so the template can use it directly. The
		// hierarchical format ("Parent / Child") is used when the category has a
		// parent that differs from itself; standalone categories use just their name.
		row.DisplayLabel = row.Category
		if row.TopCategory != "" && row.TopCategory != row.Category {
			row.DisplayLabel = row.TopCategory + " / " + row.Category
		}
		row.PaymentMethod = paymentMethodLabel(row.PaymentMethod)
		transactions = append(transactions, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate transaction rows: %w", err)
	}

	return transactions, totalCount, nil
}

// loadEditableTransaction retrieves the current values of a single transaction for the
// edit form. It returns only the fields that are directly editable (date, type, category
// ID, description, amount), omitting computed or denormalised columns that will be
// refreshed from the category metadata on save. The Loaded flag is set to true to
// distinguish a successfully loaded record from the zero-value struct.
func loadEditableTransaction(id int64) (EditableTransaction, error) {
	var record EditableTransaction
	err := db.DB.QueryRow(`
		SELECT id, date, type, COALESCE(category_id, 0), COALESCE(NULLIF(payment_method, ''), 'cash'), COALESCE(description, ''), amount
		FROM transactions
		WHERE id = ?
	`, id).Scan(&record.ID, &record.Date, &record.Type, &record.CategoryID, &record.PaymentMethod, &record.Description, &record.Amount)
	if err != nil {
		return EditableTransaction{}, err
	}
	record.Loaded = true
	return record, nil
}

// buildTransactionsURL constructs the full query-string-preserving URL for the transaction
// register with the given filter state. It includes the year, page number (only when > 1,
// to keep URLs clean for the default first page), type filter, and search query. This URL
// is used for pagination links, return-to redirects after edits, and the export form's
// hidden return-to field, ensuring consistent filter preservation across all navigation.
func buildTransactionsURL(year int, filterType, search string, page int) string {
	values := url.Values{}
	values.Set("year", strconv.Itoa(year))
	// Only include the page parameter when it's greater than 1. Omitting page=1 from
	// the URL keeps the default view cleaner and avoids duplicate URLs for the same
	// content (both "/transactions?year=2026" and "/transactions?year=2026&page=1"
	// would otherwise point to the same data).
	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	if filterType != "" {
		values.Set("type", filterType)
	}
	if search != "" {
		values.Set("q", search)
	}
	return "/transactions?" + values.Encode()
}

// buildTransactionPageLinks generates the numbered page link buttons for the pagination
// control. It produces a narrow window of up to 5 page links centred around the current
// page, clamped to the valid range [1, totalPages]. This compact window keeps the
// pagination bar from sprawling across the screen when there are hundreds of pages, while
// still giving the user enough context to navigate forward or backward by a few pages at a
// time. If there is only one page, nil is returned and the pagination bar is hidden.
func buildTransactionPageLinks(year int, filterType, search string, page, totalPages int) []PageLink {
	// I keep the paging window narrow on purpose so the register stays compact even when the year
	// has many pages of activity.
	if totalPages <= 1 {
		return nil
	}

	// Calculate a window of up to 5 pages: start at (page - 2) and end at (start + 4).
	// Both boundaries are clamped to [1, totalPages], and if the window is smaller than
	// 5 because of clamping, we shift the start backward to fill the window when
	// possible (e.g., when the user is on the last page).
	start := page - 2
	if start < 1 {
		start = 1
	}
	end := start + 4
	if end > totalPages {
		end = totalPages
	}
	if end-start < 4 {
		start = end - 4
		if start < 1 {
			start = 1
		}
	}

	links := make([]PageLink, 0, end-start+1)
	for number := start; number <= end; number++ {
		links = append(links, PageLink{
			Number: number,
			URL:    buildTransactionsURL(year, filterType, search, number),
			Active: number == page,
		})
	}
	return links
}

// redirectWithMessage appends a feedback message to the given return-to URL and issues an
// HTTP 303 See Other redirect. It intelligently chooses between "?" and "&" as the
// parameter separator based on whether the URL already contains a query string, avoiding
// double-"?" or malformed URLs. The message is URL-escaped via queryEscape to handle
// spaces and special characters safely.
func redirectWithMessage(w http.ResponseWriter, r *http.Request, returnTo, message string) {
	separator := "?"
	if strings.Contains(returnTo, "?") {
		separator = "&"
	}
	http.Redirect(w, r, returnTo+separator+"msg="+queryEscape(message), http.StatusSeeOther)
}

// parseTransactionDate validates that a raw string conforms to the "2006-01-02" date
// format used throughout the application. It uses time.Parse for strict format checking
// rather than a regex, which also catches impossible dates like February 30th. On success,
// the trimmed and validated date string is returned; on failure, an error is returned that
// the caller can translate into a user-facing message.
func parseTransactionDate(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return "", err
	}
	return value, nil
}

func transactionYear(validDate string) int {
	parsed, _ := time.Parse("2006-01-02", validDate)
	return parsed.Year()
}

// parsePageNumber converts a raw query parameter string into a positive page number.
// Invalid, negative, or zero values all default to page 1. This function is intentionally
// lenient—a malformed page parameter should show the first page, not an error, because
// it's typically the result of manual URL manipulation or a stale bookmark.
func parsePageNumber(raw string) int {
	page, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || page < 1 {
		return 1
	}
	return page
}

// totalPagesForCount computes the total number of pages needed to display a given number
// of items at a fixed page size, rounding up for any partial final page. A count of zero
// returns 1 page (not 0) so that the UI always shows a page frame with an "empty" message
// rather than disappearing entirely.
func totalPagesForCount(totalCount, pageSize int) int {
	if totalCount <= 0 {
		return 1
	}
	pages := totalCount / pageSize
	if totalCount%pageSize != 0 {
		pages++
	}
	return pages
}

// exportDateDefaults returns the inclusive start and end date strings for a full calendar
// year, formatted as "YYYY-01-01" and "YYYY-12-31". These are used to pre-populate the
// export form's date range inputs on the transaction register page, giving the user a
// sensible default that matches the currently viewed year.
func exportDateDefaults(year int) (string, string) {
	return fmt.Sprintf("%d-01-01", year), fmt.Sprintf("%d-12-31", year)
}
