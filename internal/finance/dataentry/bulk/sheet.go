// Package bulk holds the pure workbook logic behind data-entry Excel uploads and exports.
// The HTTP handlers stay thin: they own request parsing, database writes, and audit records,
// while this package decides which sheet is the ledger, how a header maps to columns, and
// how a cell becomes a date or an amount. Keeping that split makes the parser testable
// without a request or a database.
package bulk

import (
	"fmt"
	"strings"

	"github.com/xuri/excelize/v2"
)

const (
	// HeaderScanRows is how far down a sheet the header is searched for. The client's
	// ledger starts with a blank row and repeats the day in column A beside the labels.
	HeaderScanRows = 15
	// categorySampleRows is how many data rows are sampled when the sheet has no
	// category header and the column has to be inferred.
	categorySampleRows = 16
	// categoryScanColumns bounds the inference search so a wide bank book sheet cannot
	// turn a stray text column into an account name.
	categoryScanColumns = 4
	// sheetScoreFloor is the row count below which a sheet is treated as unrelated to
	// entry data. It keeps summary tabs from winning sheet selection on a single lucky row.
	sheetScoreFloor = 1
)

// expenseSheetMarkers names the tabs the client uses for the expenditure ledger. The client
// file also carries bank book tabs named Sheet1 and Sheet3 with the same date and amount
// columns, so the name check is what keeps an expenditure upload off the bank book when the
// ledger tab is present.
var expenseSheetMarkers = []string{"expense", "expenditure"}

// headerAliases maps a semantic column to the words real workbooks use for it.
// Substring matching is deliberate: the client file spells the description column
// "DESCRIP TION" and the amount column "AMOUNT(GHC)", so exact header equality would
// miss both.
var headerAliases = map[string][]string{
	"date":        {"date", "day"},
	"type":        {"type", "transaction type", "entry type"},
	"description": {"descr", "particular", "designation", "narration", "details", "item"},
	"amount":      {"amount", "amt", "ghc", "value", "total"},
	"category":    {"category", "account", "head"},
	"payment":     {"payment", "method", "mode"},
}

// ColumnLayout records which sheet column carries each semantic field. A -1 means the
// field was not found, so the importer infers it from the data rows.
type ColumnLayout struct {
	Date            int
	TransactionType int
	Description     int
	Amount          int
	Category        int
	Payment         int
	HeaderRow       int
	CategoryLabeled bool
}

// NewColumnLayout returns a layout with every column unset.
func NewColumnLayout() ColumnLayout {
	return ColumnLayout{
		Date:            -1,
		TransactionType: -1,
		Description:     -1,
		Amount:          -1,
		Category:        -1,
		Payment:         -1,
		HeaderRow:       -1,
	}
}

// ChooseSheet picks the worksheet that actually contains dated entries. A workbook can
// carry a bank book, a summary tab, and the ledger. Scoring each sheet by how many rows
// yield a real date and amount selects the ledger without asking the operator to guess.
//
// Expenditure uploads are stricter: the bank book has date and amount headers that look
// importable, but no account head. Only a sheet named like the expenditure ledger, or one
// that explicitly labels a category column, can win. This is what makes "upload the
// EXPENSES sheet" a rule instead of a hope.
func ChooseSheet(workbook *excelize.File, requested, transactionType string) (string, ColumnLayout, [][]string, error) {
	sheetNames := workbook.GetSheetList()
	if len(sheetNames) == 0 {
		return "", ColumnLayout{}, nil, fmt.Errorf("the workbook has no worksheets")
	}

	if requested != "" {
		for _, name := range sheetNames {
			if !strings.EqualFold(name, requested) {
				continue
			}
			layout, rows, err := InspectSheet(workbook, name)
			if err != nil {
				return "", ColumnLayout{}, nil, err
			}
			if layout.HeaderRow < 0 {
				return "", ColumnLayout{}, nil, fmt.Errorf("worksheet %q has no description and amount header", requested)
			}
			if layout.Category < 0 {
				return "", ColumnLayout{}, nil, fmt.Errorf("worksheet %q has no category column", requested)
			}
			if !sheetFitsType(name, layout, transactionType) {
				return "", ColumnLayout{}, nil, fmt.Errorf("worksheet %q is not an expenditure ledger; upload the EXPENSES sheet", requested)
			}
			return name, layout, rows, nil
		}
		return "", ColumnLayout{}, nil, fmt.Errorf("worksheet %q was not found in the workbook", requested)
	}

	// Expenditure gets two passes. The client's bank book has date and amount headers that
	// score well, so the EXPENSES tab must win before any other tab is considered. A generic
	// workbook is still accepted in the second pass, but only when it labels a category
	// column, which is an explicit statement that the sheet carries account heads.
	if transactionType == "expenditure" {
		if candidate, ok := bestScoringSheet(workbook, sheetNames, func(name string, _ ColumnLayout) bool {
			return sheetNameMatchesExpense(name)
		}); ok && candidate.score >= sheetScoreFloor {
			return candidate.name, candidate.layout, candidate.rows, nil
		}
		if candidate, ok := bestScoringSheet(workbook, sheetNames, func(_ string, layout ColumnLayout) bool {
			return layout.CategoryLabeled
		}); ok && candidate.score >= sheetScoreFloor {
			return candidate.name, candidate.layout, candidate.rows, nil
		}
		return "", ColumnLayout{}, nil, fmt.Errorf("no worksheet named like an expenditure ledger with a date, description, amount, and category column was found")
	}

	candidate, ok := bestScoringSheet(workbook, sheetNames, func(string, ColumnLayout) bool { return true })
	if !ok || candidate.score < sheetScoreFloor {
		return "", ColumnLayout{}, nil, fmt.Errorf("no worksheet with a date, description, amount, and category column was found")
	}
	return candidate.name, candidate.layout, candidate.rows, nil
}

type sheetCandidate struct {
	name   string
	layout ColumnLayout
	rows   [][]string
	score  int
}

// bestScoringSheet inspects the sheets the caller accepts and returns the one with the most
// importable rows. The accept callback keeps the expenditure preference rule out of the
// scoring loop, so the same scan serves both passes.
func bestScoringSheet(workbook *excelize.File, names []string, accept func(string, ColumnLayout) bool) (sheetCandidate, bool) {
	best := sheetCandidate{score: -1}
	found := false
	for _, name := range names {
		layout, rows, err := InspectSheet(workbook, name)
		if err != nil || layout.HeaderRow < 0 || layout.Category < 0 {
			continue
		}
		if !accept(name, layout) {
			continue
		}
		score := ScoreSheet(rows, layout)
		if score > best.score {
			best = sheetCandidate{name: name, layout: layout, rows: rows, score: score}
			found = true
		}
	}
	return best, found
}

// sheetFitsType rejects the bank book tabs for expenditure. A sheet that labels a category
// column is always accepted because that label is an explicit statement of intent, even when
// the operator named the tab something generic.
func sheetFitsType(name string, layout ColumnLayout, transactionType string) bool {
	if transactionType != "expenditure" {
		return true
	}
	return layout.CategoryLabeled || sheetNameMatchesExpense(name)
}

func sheetNameMatchesExpense(name string) bool {
	lowered := strings.ToLower(name)
	for _, marker := range expenseSheetMarkers {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}

// InspectSheet reads raw cell values and locates the header row and column layout.
// Raw values are used so a date formatted cell arrives as its Excel serial number, which
// converts unambiguously instead of depending on the machine's regional date format.
func InspectSheet(workbook *excelize.File, sheet string) (ColumnLayout, [][]string, error) {
	rows, err := workbook.GetRows(sheet, excelize.Options{RawCellValue: true})
	if err != nil {
		return ColumnLayout{}, nil, err
	}

	layout := NewColumnLayout()

	limit := len(rows)
	if limit > HeaderScanRows {
		limit = HeaderScanRows
	}
	for index := 0; index < limit; index++ {
		candidate := DetectHeader(rows[index])
		// Description plus amount is the smallest header that can describe an entry.
		// Requiring both avoids mistaking a title row for a header.
		if candidate.Description >= 0 && candidate.Amount >= 0 {
			candidate.HeaderRow = index
			layout = candidate
			break
		}
	}
	if layout.HeaderRow < 0 {
		return layout, rows, nil
	}

	layout.CategoryLabeled = layout.Category >= 0
	layout.Date = inferDateColumn(rows, layout)
	layout.Category = inferCategoryColumn(rows, layout)
	return layout, rows, nil
}

// DetectHeader maps header labels onto column indexes. Only description and amount are
// required here. Date and category are inferred afterwards so sheets that label neither
// still import.
func DetectHeader(row []string) ColumnLayout {
	layout := NewColumnLayout()
	for index, raw := range row {
		label := strings.ToLower(strings.TrimSpace(raw))
		if label == "" {
			continue
		}
		for field, aliases := range headerAliases {
			if !labelMatches(label, aliases) {
				continue
			}
			switch field {
			case "date":
				if layout.Date < 0 {
					layout.Date = index
				}
			case "type":
				if layout.TransactionType < 0 {
					layout.TransactionType = index
				}
			case "description":
				if layout.Description < 0 {
					layout.Description = index
				}
			case "amount":
				if layout.Amount < 0 {
					layout.Amount = index
				}
			case "category":
				if layout.Category < 0 {
					layout.Category = index
				}
			case "payment":
				if layout.Payment < 0 {
					layout.Payment = index
				}
			}
		}
	}
	return layout
}

func labelMatches(label string, aliases []string) bool {
	for _, alias := range aliases {
		if strings.Contains(label, alias) {
			return true
		}
	}
	return false
}

// inferDateColumn looks at the data rows when the sheet has no date header. The first
// column holding a real date or Excel serial wins, which is how the ledger sheet is read
// even though it repeats the day in column A without labelling it.
func inferDateColumn(rows [][]string, layout ColumnLayout) int {
	if layout.Date >= 0 {
		return layout.Date
	}
	for index := layout.HeaderRow + 1; index < len(rows); index++ {
		row := rows[index]
		for column := range row {
			if column == layout.Description || column == layout.Amount || column == layout.Category || column == layout.Payment || column == layout.TransactionType {
				continue
			}
			if _, ok := ParseDate(Cell(row, column)); ok {
				return column
			}
		}
	}
	return -1
}

// inferCategoryColumn falls back to the first text column after the amount when the sheet
// has no category header. The client's ledger stores the expense head in exactly that
// position, one column to the right of the amount.
func inferCategoryColumn(rows [][]string, layout ColumnLayout) int {
	if layout.Category >= 0 {
		return layout.Category
	}
	if layout.Amount < 0 {
		return -1
	}

	start := layout.Amount + 1
	sampleLimit := layout.HeaderRow + categorySampleRows
	if sampleLimit > len(rows) {
		sampleLimit = len(rows)
	}

	// A column qualifies when it holds text and no numbers. Counting numeric hits as a
	// veto is what keeps the running balance column out of the category slot, and it also
	// lets a single row workbook resolve, which a minimum hit count would not.
	for column := start; column < start+categoryScanColumns; column++ {
		textHits := 0
		numericHits := 0
		for index := layout.HeaderRow + 1; index < sampleLimit; index++ {
			if column == layout.Description || column == layout.Payment || column == layout.Date || column == layout.TransactionType {
				continue
			}
			value := strings.TrimSpace(Cell(rows[index], column))
			if value == "" {
				continue
			}
			if _, err := ParseAmount(value); err != nil {
				textHits++
			} else {
				numericHits++
			}
		}
		if textHits > 0 && numericHits == 0 {
			return column
		}
	}
	return -1
}

// ScoreSheet counts how many data rows look importable, which is how the ledger sheet
// outranks a summary tab during sheet selection.
func ScoreSheet(rows [][]string, layout ColumnLayout) int {
	score := 0
	for index := layout.HeaderRow + 1; index < len(rows); index++ {
		row := rows[index]
		if RowIsBlank(row) || RowIsSummary(row) {
			continue
		}
		if _, ok := DateValue(row, layout.Date); !ok {
			continue
		}
		if _, ok := AmountValue(row, layout.Amount); !ok {
			continue
		}
		score++
	}
	return score
}

// Cell returns one column from a row without making every caller repeat the bounds check.
func Cell(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	return row[index]
}

// RowIsBlank reports whether every cell in the row is empty or whitespace.
func RowIsBlank(row []string) bool {
	for _, value := range row {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

// RowIsSummary recognises the subtotal and opening balance rows that appear between
// real entries. They carry a formula or a running total in the amount column, so they must
// never become postings.
func RowIsSummary(row []string) bool {
	for _, value := range row {
		trimmed := strings.ToLower(strings.TrimSpace(value))
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "=") ||
			strings.HasPrefix(trimmed, "closing bal") ||
			strings.HasPrefix(trimmed, "opening bal") {
			return true
		}
	}
	return false
}
