package finance

import (
	"database/sql"
	"errors"
	"net/http"
	"ramseyer-finance/internal/finance/dataentry/bulk"
	"strconv"
	"strings"
	"time"
)

// imprestEntryLimit bounds the working list on the imprest page. The page is a place to
// review recent payments, not a full register, so a fixed window keeps the render cheap
// while the year total still covers every saved row.
const imprestEntryLimit = 100

// ImprestData carries the imprest page's totals, category list, and working entries. The
// page is intentionally independent of the main data-entry page: the imprest book has its
// own heads and its own total, and mixing the two would hide which book a number came from.
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
	BulkImport       *bulk.ImportSummary
}

// ImprestPage serves the imprest cash book page. It shows the month and year imprest totals,
// the main expenditure they sit beside, and the combined expenditure the statements use.
func ImprestPage(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	year := now.Year()

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

	data.Entries, err = loadImprestEntries(year, imprestEntryLimit)
	if err != nil {
		serverError(w, err)
		return
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

	date, err := parseTransactionDate(r.FormValue("date"))
	if err != nil {
		redirectImprest(w, r, "Enter a valid date.", true)
		return
	}

	categoryID, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("category_id")), 10, 64)
	if err != nil || categoryID <= 0 {
		redirectImprest(w, r, "Choose an imprest category.", true)
		return
	}
	category, err := lookupImprestCategory(categoryID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			redirectImprest(w, r, "That imprest category is not available.", true)
			return
		}
		serverError(w, err)
		return
	}

	description := strings.TrimSpace(r.FormValue("description"))
	if description == "" {
		redirectImprest(w, r, "Describe what the imprest paid for.", true)
		return
	}

	amount, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue("amount")), 64)
	if err != nil || amount <= 0 {
		redirectImprest(w, r, "Enter an amount greater than zero.", true)
		return
	}

	if err := insertImprestExpense(date, category, description, amount); err != nil {
		serverError(w, err)
		return
	}

	redirectImprest(w, r, "Imprest expense saved.", false)
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

// redirectImprest returns to the imprest page with the operator-facing message. Warning
// tone is carried in the query string so the page can style a refusal differently from a
// save without a second handler.
func redirectImprest(w http.ResponseWriter, r *http.Request, message string, warning bool) {
	target := "/imprest?msg=" + queryEscape(message)
	if warning {
		target += "&tone=warning"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
