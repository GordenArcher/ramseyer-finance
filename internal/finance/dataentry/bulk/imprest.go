package bulk

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

const (
	// imprestScanRows is how far down a sheet the imprest header is searched. The client's
	// cash book puts the money columns and the DETAILS label a few rows under the title.
	imprestScanRows = 15
	// imprestSampleRows bounds the column inference below the header so one stray value
	// cannot decide which column carries the description.
	imprestSampleRows = 20
)

// ImprestLayout records where the imprest cash book keeps each field. It is intentionally
// separate from ColumnLayout because this book has two money columns (income and expense)
// and the category sits to the right of the expense column, not beside the description.
type ImprestLayout struct {
	Date        int
	Description int
	Amount      int
	Category    int
	HeaderRow   int
}

// NewImprestLayout returns a layout with every column unset.
func NewImprestLayout() ImprestLayout {
	return ImprestLayout{
		Date:        -1,
		Description: -1,
		Amount:      -1,
		Category:    -1,
		HeaderRow:   -1,
	}
}

// ImprestEntry is one importable cash book line. RowNumber is 1 based so the operator can
// find it in Excel without translating from a slice index.
type ImprestEntry struct {
	RowNumber   int
	Date        string
	Category    string
	Description string
	Amount      float64
}

// ChooseImprestSheet picks the worksheet for the requested year. The client names the tab
// "2026", so that match wins. A differently named sheet is still accepted when it has the
// cash book columns, which keeps a renamed copy usable without asking the operator to fix
// the tab name first.
func ChooseImprestSheet(workbook *excelize.File, year int) (string, ImprestLayout, [][]string, error) {
	sheetNames := workbook.GetSheetList()
	if len(sheetNames) == 0 {
		return "", ImprestLayout{}, nil, fmt.Errorf("the workbook has no worksheets")
	}

	wanted := strconv.Itoa(year)
	for _, name := range sheetNames {
		if strings.TrimSpace(name) != wanted {
			continue
		}
		layout, rows, err := InspectImprestSheet(workbook, name)
		if err != nil {
			return "", ImprestLayout{}, nil, err
		}
		if layout.HeaderRow >= 0 {
			return name, layout, rows, nil
		}
	}

	type candidate struct {
		name   string
		layout ImprestLayout
		rows   [][]string
		score  int
	}
	best := candidate{score: -1}
	for _, name := range sheetNames {
		layout, rows, err := InspectImprestSheet(workbook, name)
		if err != nil || layout.HeaderRow < 0 {
			continue
		}
		score := scoreImprestRows(rows, layout)
		if score > best.score {
			best = candidate{name: name, layout: layout, rows: rows, score: score}
		}
	}
	if best.score < 1 {
		return "", ImprestLayout{}, nil, fmt.Errorf("no sheet with a Description, EXPENSE, and DETAILS column was found in the workbook")
	}
	return best.name, best.layout, best.rows, nil
}

// InspectImprestSheet finds the header row and the four columns the imprest book uses. The
// DETAILS label is the anchor: it is the only header the client writes for the category,
// and the expense column is located from the INCOME/EXPENSE labels above it.
func InspectImprestSheet(workbook *excelize.File, sheet string) (ImprestLayout, [][]string, error) {
	rows, err := workbook.GetRows(sheet, excelize.Options{RawCellValue: true})
	if err != nil {
		return ImprestLayout{}, nil, err
	}

	limit := len(rows)
	if limit > imprestScanRows {
		limit = imprestScanRows
	}
	for index := 0; index < limit; index++ {
		categoryColumn := findImprestCategoryColumn(rows[index])
		if categoryColumn < 0 {
			continue
		}

		layout := NewImprestLayout()
		layout.HeaderRow = index
		layout.Category = categoryColumn
		layout.Amount = findImprestExpenseColumn(rows, index, categoryColumn)
		layout.Description = inferImprestDescriptionColumn(rows, layout)
		layout.Date = inferImprestDateColumn(rows, layout)
		return layout, rows, nil
	}

	return NewImprestLayout(), rows, nil
}

func findImprestCategoryColumn(row []string) int {
	for index, raw := range row {
		if strings.EqualFold(strings.TrimSpace(raw), "details") {
			return index
		}
	}
	return -1
}

// findImprestExpenseColumn looks for the EXPENSE label in the header row or the two rows
// above it. Falling back to the column immediately left of DETAILS keeps a sheet that
// dropped the INCOME/EXPENSE captions importable.
func findImprestExpenseColumn(rows [][]string, headerRow, categoryColumn int) int {
	start := headerRow - 2
	if start < 0 {
		start = 0
	}
	for index := headerRow; index >= start; index-- {
		for column := categoryColumn - 1; column >= 0; column-- {
			label := strings.ToLower(strings.TrimSpace(Cell(rows[index], column)))
			if label == "expense" || label == "expenses" || label == "expenditure" {
				return column
			}
		}
	}
	if categoryColumn > 0 {
		return categoryColumn - 1
	}
	return -1
}

// inferImprestDescriptionColumn walks left from the amount and takes the first column that
// holds text without numbers. The income column between them is normally empty, so this
// skips it and lands on the description.
func inferImprestDescriptionColumn(rows [][]string, layout ImprestLayout) int {
	if layout.Amount <= 0 {
		return -1
	}

	sampleEnd := layout.HeaderRow + imprestSampleRows
	if sampleEnd > len(rows) {
		sampleEnd = len(rows)
	}

	for column := layout.Amount - 1; column >= 0; column-- {
		textHits := 0
		numericHits := 0
		for index := layout.HeaderRow + 1; index < sampleEnd; index++ {
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

// inferImprestDateColumn picks the leftmost column before the description that actually
// holds dates. The book writes the date only on the first row of a group, so this has to
// look at the data rather than rely on a header that does not exist.
func inferImprestDateColumn(rows [][]string, layout ImprestLayout) int {
	limit := layout.Description
	if limit < 0 {
		limit = layout.Amount
	}
	for column := 0; column < limit; column++ {
		for index := layout.HeaderRow + 1; index < len(rows); index++ {
			if _, ok := ParseDate(Cell(rows[index], column)); ok {
				return column
			}
		}
	}
	return -1
}

func scoreImprestRows(rows [][]string, layout ImprestLayout) int {
	score := 0
	for index := layout.HeaderRow + 1; index < len(rows); index++ {
		row := rows[index]
		if RowIsBlank(row) || RowIsSummary(row) {
			continue
		}
		if strings.TrimSpace(Cell(row, layout.Category)) == "" {
			continue
		}
		if _, ok := AmountValue(row, layout.Amount); !ok {
			continue
		}
		score++
	}
	return score
}

// ReadImprestRows turns the worksheet into entries and row-level refusals. The imprest book
// writes a date once and leaves the following rows blank, so the last seen date is carried
// down until the next date appears. That carry is why this cannot reuse the per-type row
// loop, which treats a missing date as a refusal.
func ReadImprestRows(rows [][]string, layout ImprestLayout) ([]ImprestEntry, []string, int) {
	var (
		entries []ImprestEntry
		errors  []string
		skipped int
	)

	// The client's book leaves its first group undated. Seeding the carry with the earliest
	// written date keeps those opening payments in the import instead of dropping them and
	// understating the book. A sheet with no date anywhere still refuses every row below.
	lastDate := firstImprestDate(rows, layout)

	for index := layout.HeaderRow + 1; index < len(rows); index++ {
		row := rows[index]
		if RowIsBlank(row) {
			continue
		}
		if RowIsSummary(row) {
			skipped++
			continue
		}

		rowNumber := index + 1
		category := strings.TrimSpace(Cell(row, layout.Category))
		description := strings.TrimSpace(Cell(row, layout.Description))

		rawDate := strings.TrimSpace(Cell(row, layout.Date))
		if rawDate != "" {
			parsed, ok := ParseDate(rawDate)
			if !ok {
				skipped++
				errors = append(errors, imprestRowError(rowNumber, "date is not a real date"))
				continue
			}
			lastDate = parsed
		}
		if lastDate == "" {
			skipped++
			errors = append(errors, imprestRowError(rowNumber, "date is missing and the sheet has no date to carry"))
			continue
		}

		amount, ok := AmountValue(row, layout.Amount)
		if !ok {
			skipped++
			errors = append(errors, imprestRowError(rowNumber, "expense amount is missing or not a number"))
			continue
		}
		if amount <= 0 {
			skipped++
			errors = append(errors, imprestRowError(rowNumber, "expense amount must be greater than zero"))
			continue
		}
		if category == "" {
			skipped++
			errors = append(errors, imprestRowError(rowNumber, "DETAILS category is missing"))
			continue
		}
		if description == "" {
			skipped++
			errors = append(errors, imprestRowError(rowNumber, "description is missing"))
			continue
		}

		entries = append(entries, ImprestEntry{
			RowNumber:   rowNumber,
			Date:        lastDate,
			Category:    category,
			Description: description,
			Amount:      amount,
		})
	}

	return entries, errors, skipped
}

// firstImprestDate returns the earliest readable date in the data rows. It exists for the
// one case the carry cannot solve: a book whose first group has no date at all. The earliest
// written date is the only defensible anchor for those rows.
func firstImprestDate(rows [][]string, layout ImprestLayout) string {
	for index := layout.HeaderRow + 1; index < len(rows); index++ {
		if parsed, ok := ParseDate(Cell(rows[index], layout.Date)); ok {
			return parsed
		}
	}
	return ""
}

func imprestRowError(rowNumber int, message string) string {
	return fmt.Sprintf("Row %d: %s", rowNumber, message)
}
