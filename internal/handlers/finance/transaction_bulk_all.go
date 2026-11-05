package finance

import (
	"net/http"
	"ramseyer-finance/internal/finance/dataentry/bulk"
	"strings"
)

// bulkImportType pairs a transaction type with the label its section shows in the report.
// The slice order is the order sections appear, so it follows the data-entry tabs.
type bulkImportType struct {
	Type  string
	Label string
}

var bulkImportTypes = []bulkImportType{
	{Type: "income", Label: "Income"},
	{Type: "expenditure", Label: "Expenditure"},
	{Type: "asset", Label: "Asset"},
	{Type: "liability", Label: "Liability"},
}

// BulkImportAllTransactions imports one sheet that carries every transaction type. The Type
// column decides which account namespace each row is matched against, so one upload can save
// income, expenditure, assets, and liabilities together. The per-type upload inside each tab
// stays available for the client's existing single-type workbooks.
//
// The insert, duplicate, and audit rules are shared with the per-type handler through
// saveImportedRow. This handler owns only the extra job of reading the Type cell and routing
// the row, which is why it cannot drift from the individual uploads.
func BulkImportAllTransactions(w http.ResponseWriter, r *http.Request) {
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

	sheetName, layout, rows, err := bulk.ChooseCombinedSheet(workbook, strings.TrimSpace(r.FormValue("sheet")))
	if err != nil {
		badRequest(w, err.Error())
		return
	}

	summary := bulk.ImportSummary{Sheet: sheetName}
	sections := make(map[string]*bulk.ImportSection, len(bulkImportTypes))
	categoryCache := make(map[string][]bulk.Category, len(bulkImportTypes))

	for index := layout.HeaderRow + 1; index < len(rows); index++ {
		row := rows[index]
		if bulk.RowIsBlank(row) {
			continue
		}

		// Type is read before every other cell because a row without a usable type cannot
		// be attributed to an account namespace. Refusing it here is what keeps the rest of
		// the row from being matched against the wrong chart of accounts.
		typeLabel := strings.TrimSpace(bulk.Cell(row, layout.TransactionType))
		transactionType, ok := bulk.NormalizeTransactionType(typeLabel)
		if !ok {
			summary.Skipped++
			if typeLabel == "" {
				summary.AppendRowError(index, "type is missing; use income, expenditure, asset, or liability")
			} else {
				summary.AppendRowError(index, "type \""+typeLabel+"\" is not income, expenditure, asset, or liability")
			}
			continue
		}
		section := ensureImportSection(sections, transactionType)

		if bulk.RowIsSummary(row) {
			section.Skipped++
			continue
		}

		dateValue, ok := bulk.DateValue(row, layout.Date)
		if !ok {
			section.Skipped++
			section.AppendRowError(index, "date is missing or not a real date")
			continue
		}

		amount, ok := bulk.AmountValue(row, layout.Amount)
		if !ok {
			section.Skipped++
			section.AppendRowError(index, "amount is missing or not a number")
			continue
		}
		if (transactionType == "income" || transactionType == "expenditure") && amount <= 0 {
			section.Skipped++
			section.AppendRowError(index, "income and expenditure amounts must be greater than zero")
			continue
		}
		if amount == 0 {
			section.Skipped++
			section.AppendRowError(index, "amount must not be zero")
			continue
		}

		categories, err := categorySet(categoryCache, transactionType)
		if err != nil {
			serverError(w, err)
			return
		}

		description := strings.TrimSpace(bulk.Cell(row, layout.Description))
		categoryLabel := strings.TrimSpace(bulk.Cell(row, layout.Category))
		paymentMethod := strings.TrimSpace(bulk.Cell(row, layout.Payment))
		if reason := saveImportedRow(transactionType, dateValue, description, categoryLabel, paymentMethod, amount, categories); reason != "" {
			section.Skipped++
			section.AppendRowError(index, reason)
			continue
		}
		section.Imported++
	}

	assembleCombinedSummary(&summary, sections)

	token := bulkResultStore.Put(summary)
	http.Redirect(w, r, "/data-entry?bulk="+token, http.StatusSeeOther)
}

// ensureImportSection creates the section the first time a type appears in the sheet, so the
// report shows only the types the operator actually uploaded.
func ensureImportSection(sections map[string]*bulk.ImportSection, transactionType string) *bulk.ImportSection {
	section, exists := sections[transactionType]
	if !exists {
		section = &bulk.ImportSection{Type: transactionType}
		sections[transactionType] = section
	}
	return section
}

// categorySet loads one type's accounts once per upload. Four small queries beat one query
// per row on a sheet that may carry hundreds of mixed entries.
func categorySet(cache map[string][]bulk.Category, transactionType string) ([]bulk.Category, error) {
	if cached, exists := cache[transactionType]; exists {
		return cached, nil
	}
	loaded, err := loadBulkCategories(transactionType)
	if err != nil {
		return nil, err
	}
	cache[transactionType] = loaded
	return loaded, nil
}

// assembleCombinedSummary folds the per-type sections into the totals and the flat error
// list the page renders. Prefixing each message with its type keeps "Row 4" unambiguous
// when one sheet carried several namespaces.
func assembleCombinedSummary(summary *bulk.ImportSummary, sections map[string]*bulk.ImportSection) {
	for _, entry := range bulkImportTypes {
		section, exists := sections[entry.Type]
		if !exists {
			continue
		}
		section.Label = entry.Label
		summary.Sections = append(summary.Sections, *section)
		summary.Imported += section.Imported
		summary.Skipped += section.Skipped
		for _, message := range section.Errors {
			summary.Errors = append(summary.Errors, entry.Label+": "+message)
		}
	}
}
