package finance

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"ramseyer-finance/internal/finance/dataentry/bulk"
	"strconv"
	"strings"
	"time"
)

// imprestPageSize controls how many imprest payments appear on one page. The cash book can
// hold hundreds of rows for a year, so the list pages in SQL instead of truncating at a
// fixed window or rendering every row at once.
const imprestPageSize = 50

// ImprestData carries the imprest page's totals, category list, working entries, and the
// state needed to paginate and edit them. The page is intentionally independent of the main
// data-entry page: the imprest book has its own heads and its own total, and mixing the two
// would hide which book a number came from.
type ImprestData struct {
	Active           string
	Message          string
	MessageTone      string
	CurrentDate      string
	CurrentYear      int
	MonthTotal       float64
	YearTotal        float64
	YearMainExpense  float64
	YearTotalExpense float64
	Categories       []ImprestCategory
	Entries          []ImprestEntry
	Page             int
	TotalPages       int
	TotalCount       int
	HasPrev          bool
	HasNext          bool
	PrevPageURL      string
	NextPageURL      string
	PageLinks        []PageLink
	ReturnTo         string
	EditForm         EditableImprestEntry
	BulkImport       *bulk.ImportSummary
}

// imprestEntryInput is the validated set of values shared by the add and update handlers.
type imprestEntryInput struct {
	Date        string
	Category    ImprestCategory
	Description string
	Amount      float64
}

// ImprestPage serves the imprest cash book page. It shows the month and year imprest totals,
// the main expenditure they sit beside, the combined expenditure the statements use, and a
// paginated list of the year's saved payments.
func ImprestPage(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	year := now.Year()
	page := parsePageNumber(r.URL.Query().Get("page"))

	data := ImprestData{
		Active:      "imprest",
		Message:     r.URL.Query().Get("msg"),
		MessageTone: "success",
		CurrentDate: now.Format("2006-01-02"),
		CurrentYear: year,
	}
	// A rejected form redirects with tone=warning so an error is not dressed up as a save.
	if r.URL.Query().Get("tone") == "warning" {
		data.MessageTone = "warning"
	}

	categories, err := loadImprestCategories()
	if err != nil {
		serverError(w, err)
		return
	}
	data.Categories = categories

	monthStart, monthEnd := monthBounds(now)
	data.MonthTotal, err = loadImprestTotal(monthStart, monthEnd)
	if err != nil {
		serverError(w, err)
		return
	}

	yearStart, yearEnd := yearBounds(year)
	data.YearTotal, err = loadImprestTotal(yearStart, yearEnd)
	if err != nil {
		serverError(w, err)
		return
	}

	var yearIncome float64
	if err := loadIncomeExpenseTotals(yearStart, yearEnd, &yearIncome, &data.YearMainExpense); err != nil {
		serverError(w, err)
		return
	}
	data.YearTotalExpense = data.YearMainExpense + data.YearTotal

	entries, totalCount, err := loadImprestEntries(year, page, imprestPageSize)
	if err != nil {
		serverError(w, err)
		return
	}
	totalPages := totalPagesForCount(totalCount, imprestPageSize)
	// A stale page number (for example after deleting the last row on a page) should land on
	// the last real page instead of showing an empty table.
	if page > totalPages {
		http.Redirect(w, r, buildImprestURL(totalPages), http.StatusSeeOther)
		return
	}

	data.Entries = entries
	data.Page = page
	data.TotalPages = totalPages
	data.TotalCount = totalCount
	data.HasPrev = page > 1
	data.HasNext = page < totalPages
	data.ReturnTo = buildImprestURL(page)
	if data.HasPrev {
		data.PrevPageURL = buildImprestURL(page - 1)
	}
	if data.HasNext {
		data.NextPageURL = buildImprestURL(page + 1)
	}
	data.PageLinks = buildImprestPageLinks(page, totalPages)

	if editID := strings.TrimSpace(r.URL.Query().Get("edit")); editID != "" {
		id, err := strconv.ParseInt(editID, 10, 64)
		if err != nil || id <= 0 {
			redirectImprestTo(w, r, data.ReturnTo, "Invalid imprest entry.", true)
			return
		}
		editForm, err := loadEditableImprestEntry(id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				redirectImprestTo(w, r, data.ReturnTo, "Imprest entry not found.", true)
				return
			}
			serverError(w, err)
			return
		}
		data.EditForm = editForm
	}

	if summary, ok := bulkResultStore.Get(r.URL.Query().Get("bulk")); ok {
		data.BulkImport = &summary
	}

	RenderTemplate(w, "imprest", data)
}

// AddImprestExpense saves one manual imprest payment. The date, category, description, and
// amount are validated here before insert so a bad form never creates a row the page then
// has to explain.
func AddImprestExpense(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		badRequest(w, "Invalid form submission")
		return
	}
	returnTo := sanitizeReturnTo(r.FormValue("return_to"), "/imprest")

	input, message, err := parseImprestEntryForm(r)
	if err != nil {
		serverError(w, err)
		return
	}
	if message != "" {
		redirectImprestTo(w, r, returnTo, message, true)
		return
	}

	if err := insertImprestExpense(input.Date, input.Category, input.Description, input.Amount); err != nil {
		serverError(w, err)
		return
	}

	redirectImprestTo(w, r, returnTo, "Imprest expense saved.", false)
}

// UpdateImprestExpense changes one saved payment. It applies the same validation as a new
// entry, then refreshes the denormalised head name from the selected category.
func UpdateImprestExpense(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		badRequest(w, "Invalid form submission")
		return
	}
	returnTo := sanitizeReturnTo(r.FormValue("return_to"), "/imprest")

	entryID, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("id")), 10, 64)
	if err != nil || entryID <= 0 {
		redirectImprestTo(w, r, returnTo, "Invalid imprest entry.", true)
		return
	}

	input, message, err := parseImprestEntryForm(r)
	if err != nil {
		serverError(w, err)
		return
	}
	if message != "" {
		redirectImprestTo(w, r, returnTo, message, true)
		return
	}

	if err := updateImprestExpense(entryID, input.Date, input.Category, input.Description, input.Amount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			redirectImprestTo(w, r, returnTo, "Imprest entry not found.", true)
			return
		}
		serverError(w, err)
		return
	}

	redirectImprestTo(w, r, returnTo, "Imprest expense updated.", false)
}

// DeleteImprestExpense removes one saved payment from the imprest book.
func DeleteImprestExpense(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		badRequest(w, "Invalid form submission")
		return
	}
	returnTo := sanitizeReturnTo(r.FormValue("return_to"), "/imprest")

	entryID, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("id")), 10, 64)
	if err != nil || entryID <= 0 {
		redirectImprestTo(w, r, returnTo, "Invalid imprest entry.", true)
		return
	}

	if err := deleteImprestExpense(entryID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			redirectImprestTo(w, r, returnTo, "Imprest entry not found.", true)
			return
		}
		serverError(w, err)
		return
	}

	redirectImprestTo(w, r, returnTo, "Imprest expense deleted.", false)
}

// SaveImprestCategory adds a head, or renames one when the form carries its ID. Renaming
// updates the denormalised name on saved payments so the head list and history agree.
func SaveImprestCategory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		badRequest(w, "Invalid form submission")
		return
	}
	returnTo := sanitizeReturnTo(r.FormValue("return_to"), "/imprest")

	categoryID, err := parseOptionalInt64(r.FormValue("id"))
	if err != nil || categoryID < 0 {
		redirectImprestTo(w, r, returnTo, "Invalid imprest head.", true)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		redirectImprestTo(w, r, returnTo, "Enter an imprest head name.", true)
		return
	}

	taken, err := imprestCategoryNameTaken(name, categoryID)
	if err != nil {
		serverError(w, err)
		return
	}
	if taken {
		redirectImprestTo(w, r, returnTo, "An imprest head with that name already exists.", true)
		return
	}

	if categoryID == 0 {
		if err := createImprestCategory(name); err != nil {
			serverError(w, err)
			return
		}
		redirectImprestTo(w, r, returnTo, "Imprest head added.", false)
		return
	}

	if err := renameImprestCategory(categoryID, name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			redirectImprestTo(w, r, returnTo, "Imprest head not found.", true)
			return
		}
		serverError(w, err)
		return
	}

	redirectImprestTo(w, r, returnTo, "Imprest head renamed. Saved payments now use the new name.", false)
}

// DeleteImprestCategory removes a head that no saved payment uses. A used head must be
// renamed instead so historical payments keep a valid category.
func DeleteImprestCategory(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		badRequest(w, "Invalid form submission")
		return
	}
	returnTo := sanitizeReturnTo(r.FormValue("return_to"), "/imprest")

	categoryID, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("id")), 10, 64)
	if err != nil || categoryID <= 0 {
		redirectImprestTo(w, r, returnTo, "Invalid imprest head.", true)
		return
	}

	usage, err := imprestCategoryUsage(categoryID)
	if err != nil {
		serverError(w, err)
		return
	}
	if usage > 0 {
		redirectImprestTo(
			w,
			r,
			returnTo,
			fmt.Sprintf(
				"This head is used by %d saved payment%s. Rename it instead, or delete those payments first.",
				usage,
				pluralSuffix(usage),
			),
			true,
		)
		return
	}

	if err := deleteImprestCategory(categoryID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			redirectImprestTo(w, r, returnTo, "Imprest head not found.", true)
			return
		}
		serverError(w, err)
		return
	}

	redirectImprestTo(w, r, returnTo, "Imprest head deleted.", false)
}

// BulkImportImprest reads the client's imprest cash book. It uses the year-named sheet,
// carries the date down each group, and stores the DETAILS head exactly as written. Unknown
// heads are reported instead of creating a new category, so the head list stays an explicit
// operator decision.
func BulkImportImprest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(uploadMemoryLimit); err != nil {
		badRequest(w, "Invalid form submission")
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

	year, err := parseReportYear(r.FormValue("year"))
	if err != nil {
		badRequest(w, err.Error())
		return
	}

	sheetName, layout, rows, err := bulk.ChooseImprestSheet(workbook, year)
	if err != nil {
		badRequest(w, err.Error())
		return
	}

	entries, rowErrors, skipped := bulk.ReadImprestRows(rows, layout)
	summary := bulk.ImportSummary{Sheet: sheetName, Skipped: skipped}
	// ReadImprestRows returns every refusal so tests can see the whole sheet. The page only
	// needs the first few, and the shared limit is what keeps a structurally wrong sheet from
	// filling the report with hundreds of identical messages.
	for _, rowError := range rowErrors {
		if len(summary.Errors) >= bulk.RowErrorLimit {
			break
		}
		summary.Errors = append(summary.Errors, rowError)
	}

	categories, err := loadImprestCategories()
	if err != nil {
		serverError(w, err)
		return
	}

	for _, entry := range entries {
		category, matched := matchImprestCategory(categories, entry.Category)
		if !matched {
			summary.Skipped++
			summary.AppendRowError(entry.RowNumber-1, "category \""+entry.Category+"\" is not one of the imprest heads")
			continue
		}
		if err := insertImprestExpense(entry.Date, category, entry.Description, entry.Amount); err != nil {
			summary.Skipped++
			summary.AppendRowError(entry.RowNumber-1, "could not be saved")
			continue
		}
		summary.Imported++
	}

	token := bulkResultStore.Put(summary)
	http.Redirect(w, r, "/imprest?bulk="+token, http.StatusSeeOther)
}

// parseImprestEntryForm validates the shared add/update fields. A non-empty message is an
// operator-facing refusal; a returned error is an unexpected server-side failure.
func parseImprestEntryForm(r *http.Request) (imprestEntryInput, string, error) {
	date, err := parseTransactionDate(r.FormValue("date"))
	if err != nil {
		return imprestEntryInput{}, "Enter a valid date.", nil
	}

	categoryID, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("category_id")), 10, 64)
	if err != nil || categoryID <= 0 {
		return imprestEntryInput{}, "Choose an imprest category.", nil
	}
	category, err := lookupImprestCategory(categoryID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return imprestEntryInput{}, "That imprest category is not available.", nil
		}
		return imprestEntryInput{}, "", err
	}

	description := strings.TrimSpace(r.FormValue("description"))
	if description == "" {
		return imprestEntryInput{}, "Describe what the imprest paid for.", nil
	}

	amount, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue("amount")), 64)
	if err != nil || amount <= 0 {
		return imprestEntryInput{}, "Enter an amount greater than zero.", nil
	}

	return imprestEntryInput{
		Date:        date,
		Category:    category,
		Description: description,
		Amount:      amount,
	}, "", nil
}

// redirectImprestTo returns to the imprest page with the operator-facing message. Warning
// tone is carried in the query string so the page can style a refusal differently from a
// save without a second handler, and returnTo preserves the current page.
func redirectImprestTo(w http.ResponseWriter, r *http.Request, returnTo, message string, warning bool) {
	target := returnTo
	separator := "?"
	if strings.Contains(target, "?") {
		separator = "&"
	}
	target += separator + "msg=" + queryEscape(message)
	if warning {
		target += "&tone=warning"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// buildImprestURL returns the paginated imprest page address, omitting page=1 so the
// default view has one canonical URL.
func buildImprestURL(page int) string {
	if page <= 1 {
		return "/imprest"
	}
	return "/imprest?page=" + strconv.Itoa(page)
}

// buildImprestPageLinks returns a compact window of numbered page links centred on the
// current page. It mirrors the transaction register so pagination feels consistent.
func buildImprestPageLinks(page, totalPages int) []PageLink {
	if totalPages <= 1 {
		return nil
	}

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
			URL:    buildImprestURL(number),
			Active: number == page,
		})
	}
	return links
}
