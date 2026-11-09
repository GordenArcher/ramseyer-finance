// Package sheet resolves an uploaded workbook into the worksheet and column layout the
// importer reads. It decides which tab is the ledger, how a header maps to columns, and
// which rows are real entries, keeping that logic testable without a request or database.
package sheet

import (
	"fmt"
	"strings"

	"ramseyer-finance/internal/finance/dataentry/row"
	"ramseyer-finance/internal/finance/dataentry/value"

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

// headerAliases maps a semantic column to the words real workbooks use for it.
// Substring matching is deliberate: the office file spells the description column
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
			if transactionType == "expenditure" && sheetLooksLikeBankBook(rows, layout) {
				return "", ColumnLayout{}, nil, fmt.Errorf("worksheet %q looks like a bank book, not an expense ledger", requested)
			}
			return name, layout, rows, nil
		}
		return "", ColumnLayout{}, nil, fmt.Errorf("worksheet %q was not found in the workbook", requested)
	}

	// The sheet is chosen by its contents, not its tab name. A bank book also has date,
	// description, and amount columns, so the cheque, deposit, and balance labels are what
	// keep it out of the candidate set.
	candidate, ok := bestScoringSheet(workbook, sheetNames, func(_ string, layout ColumnLayout, rows [][]string) bool {
		if transactionType == "expenditure" && sheetLooksLikeBankBook(rows, layout) {
			return false
		}
		return true
	})
	if !ok || candidate.Score < sheetScoreFloor {
		if transactionType == "expenditure" {
			if hasBankBookSheet(workbook, sheetNames) {
				return "", ColumnLayout{}, nil, fmt.Errorf("this workbook only contains a bank book. Upload the EXPENSES sheet, not the bank book")
			}
			return "", ColumnLayout{}, nil, fmt.Errorf("no expense sheet was found. The sheet needs a date column, a description column, an amount column, and an expense head column")
		}
		return "", ColumnLayout{}, nil, fmt.Errorf("no worksheet with a date, description, amount, and category column was found")
	}
	return candidate.Name, candidate.Layout, candidate.Rows, nil
}

// Candidate is one worksheet that scored well enough to be considered. The fields are
// exported because the all-types sheet chooser reuses the same scoring result.
type Candidate struct {
	Name   string
	Layout ColumnLayout
	Rows   [][]string
	Score  int
}

// bestScoringSheet inspects the sheets the caller accepts and returns the one with the most
// importable rows. The accept callback keeps the bank-book rule out of the scoring loop.
func bestScoringSheet(workbook *excelize.File, names []string, accept func(string, ColumnLayout, [][]string) bool) (Candidate, bool) {
	best := Candidate{Score: -1}
	found := false
	for _, name := range names {
		layout, rows, err := InspectSheet(workbook, name)
		if err != nil || layout.HeaderRow < 0 || layout.Category < 0 {
			continue
		}
		if !accept(name, layout, rows) {
			continue
		}
		score := ScoreSheet(rows, layout)
		if score > best.Score {
			best = Candidate{Name: name, Layout: layout, Rows: rows, Score: score}
			found = true
		}
	}
	return best, found
}

// hasBankBookSheet reports whether the workbook contains a sheet that looks like a bank
// book. It only runs on the no-candidate path so the operator gets a message that names the
// actual problem instead of a generic missing-column warning.
func hasBankBookSheet(workbook *excelize.File, names []string) bool {
	for _, name := range names {
		layout, rows, err := InspectSheet(workbook, name)
		if err != nil || layout.HeaderRow < 0 {
			continue
		}
		if sheetLooksLikeBankBook(rows, layout) {
			return true
		}
	}
	return false
}

// sheetLooksLikeBankBook rejects the bank book tabs that sit beside the expense ledger.
// Those tabs also have a date, a description, and an amount, so the cheque, deposit, and
// balance labels are what separate them from an expense ledger.
func sheetLooksLikeBankBook(rows [][]string, layout ColumnLayout) bool {
	if layout.HeaderRow < 0 || layout.HeaderRow >= len(rows) {
		return false
	}
	for _, value := range rows[layout.HeaderRow] {
		label := strings.ToLower(strings.TrimSpace(value))
		for _, marker := range []string{"cheque", "checque", "check", "balance", "deposit"} {
			if strings.Contains(label, marker) {
				return true
			}
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
		cells := rows[index]
		for column := range cells {
			if column == layout.Description || column == layout.Amount || column == layout.Category || column == layout.Payment || column == layout.TransactionType {
				continue
			}
			if _, ok := value.ParseDate(row.Cell(cells, column)); ok {
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
			cellValue := strings.TrimSpace(row.Cell(rows[index], column))
			if cellValue == "" {
				continue
			}
			if _, err := value.ParseAmount(cellValue); err != nil {
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
		cells := rows[index]
		if row.IsBlank(cells) || row.IsSummary(cells) {
			continue
		}
		if _, ok := value.DateValue(cells, layout.Date); !ok {
			continue
		}
		if _, ok := value.AmountValue(cells, layout.Amount); !ok {
			continue
		}
		score++
	}
	return score
}
