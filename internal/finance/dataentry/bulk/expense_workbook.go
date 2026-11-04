package bulk

import (
	"fmt"
	"time"

	"github.com/xuri/excelize/v2"
)

const (
	// ExpenseSheetPrefix names the client's expenditure tab. Keeping the year in the name
	// matches the file the church already shares, so a downloaded copy opens on the same
	// tab the operator expects.
	ExpenseSheetPrefix = "EXPENSES "
	// expenseDescriptionHeader keeps the client's spelling. The header matcher looks for
	// "descr", so changing it to a clean "DESCRIPTION" would still import, but the shared
	// file should look like the one on the client's machine.
	expenseDescriptionHeader = "DESCRIP TION"
	expenseAmountHeader      = "AMOUNT"
	// The widths are copied from the client workbook. Matching them keeps the description
	// readable and the amount column narrow, which is how the shared sheet already looks.
	expenseDescriptionWidth = 37.0
	expenseAmountWidth      = 10.21875
	expenseCategoryWidth    = 19.33203125
	// expenseDateStyle is Excel's built in d-mmm format, the same one the client workbook
	// uses. Reusing the built in id avoids shipping a locale dependent format string.
	expenseDateStyle = 16
	// expenseTemplateRows is how far down a blank template carries the date and amount
	// formats. Pre-formatting the empty rows means the operator types into cells that
	// already look like the client workbook instead of applying formats by hand.
	expenseTemplateRows = 1000
)

// expenseAmountFormat is the accounting format on the client's amount column. It keeps
// two decimals, wraps negatives in brackets, and shows a dash for zero, which is what the
// church office reads all day.
const expenseAmountFormat = `_(* #,##0.00_);_(* \(#,##0.00\);_(* "-"??_);_(@_)`

// ExpenseRow is one saved expenditure line in the client's column order.
type ExpenseRow struct {
	Date        string
	Description string
	Amount      float64
	Category    string
}

// ExpenseSheetName returns the tab name for a year, for example EXPENSES 2026.
func ExpenseSheetName(year int) string {
	return fmt.Sprintf("%s%d", ExpenseSheetPrefix, year)
}

// BuildExpenseWorkbook writes the expenditure ledger in the client's shape: a blank first
// row, the description and amount headers on row two, and data from row three across
// date, description, amount, and account name. The template is the same builder with no
// rows, so a downloaded template and a saved workbook cannot drift apart.
func BuildExpenseWorkbook(year int, rows []ExpenseRow) (*excelize.File, error) {
	workbook := excelize.NewFile()
	sheet := ExpenseSheetName(year)
	if err := workbook.SetSheetName(workbook.GetSheetName(0), sheet); err != nil {
		_ = workbook.Close()
		return nil, fmt.Errorf("name expense sheet: %w", err)
	}

	_ = workbook.SetColWidth(sheet, "B", "B", expenseDescriptionWidth)
	_ = workbook.SetColWidth(sheet, "C", "C", expenseAmountWidth)
	_ = workbook.SetColWidth(sheet, "D", "D", expenseCategoryWidth)

	dateStyle, err := workbook.NewStyle(&excelize.Style{NumFmt: expenseDateStyle})
	if err != nil {
		_ = workbook.Close()
		return nil, fmt.Errorf("create expense date style: %w", err)
	}
	amountFormat := expenseAmountFormat
	amountStyle, err := workbook.NewStyle(&excelize.Style{CustomNumFmt: &amountFormat})
	if err != nil {
		_ = workbook.Close()
		return nil, fmt.Errorf("create expense amount style: %w", err)
	}

	if err := workbook.SetCellValue(sheet, "B2", expenseDescriptionHeader); err != nil {
		_ = workbook.Close()
		return nil, fmt.Errorf("write expense description header: %w", err)
	}
	if err := workbook.SetCellValue(sheet, "C2", expenseAmountHeader); err != nil {
		_ = workbook.Close()
		return nil, fmt.Errorf("write expense amount header: %w", err)
	}
	_ = workbook.SetCellStyle(sheet, "C2", "C2", amountStyle)

	for index, row := range rows {
		rowNumber := index + 3
		if err := setExpenseDate(workbook, sheet, rowNumber, row.Date); err != nil {
			_ = workbook.Close()
			return nil, err
		}
		if err := setExpenseText(workbook, sheet, rowNumber, 2, row.Description); err != nil {
			_ = workbook.Close()
			return nil, err
		}
		amountCell, _ := excelize.CoordinatesToCellName(3, rowNumber)
		if err := workbook.SetCellValue(sheet, amountCell, row.Amount); err != nil {
			_ = workbook.Close()
			return nil, fmt.Errorf("write expense amount: %w", err)
		}
		if err := setExpenseText(workbook, sheet, rowNumber, 4, row.Category); err != nil {
			_ = workbook.Close()
			return nil, err
		}
	}

	if len(rows) > 0 {
		lastRow := len(rows) + 2
		_ = workbook.SetCellStyle(sheet, "A3", fmt.Sprintf("A%d", lastRow), dateStyle)
		_ = workbook.SetCellStyle(sheet, "C3", fmt.Sprintf("C%d", lastRow), amountStyle)
	} else {
		_ = workbook.SetCellStyle(sheet, "A3", fmt.Sprintf("A%d", expenseTemplateRows), dateStyle)
		_ = workbook.SetCellStyle(sheet, "C3", fmt.Sprintf("C%d", expenseTemplateRows), amountStyle)
	}

	return workbook, nil
}

// setExpenseDate writes a real Excel date when the stored value parses, and falls back to
// the original text when it does not. A corrupt date must not stop the operator from
// sharing the rest of the ledger, but it also must not be silently rewritten.
func setExpenseDate(workbook *excelize.File, sheet string, rowNumber int, raw string) error {
	cell, _ := excelize.CoordinatesToCellName(1, rowNumber)
	canonical, ok := ParseDate(raw)
	if !ok {
		return setExpenseText(workbook, sheet, rowNumber, 1, raw)
	}
	parsed, err := time.Parse("2006-01-02", canonical)
	if err != nil {
		return setExpenseText(workbook, sheet, rowNumber, 1, raw)
	}
	if err := workbook.SetCellValue(sheet, cell, parsed); err != nil {
		return fmt.Errorf("write expense date: %w", err)
	}
	return nil
}

func setExpenseText(workbook *excelize.File, sheet string, rowNumber, column int, value string) error {
	cell, _ := excelize.CoordinatesToCellName(column, rowNumber)
	if err := workbook.SetCellValue(sheet, cell, value); err != nil {
		return fmt.Errorf("write expense cell: %w", err)
	}
	return nil
}
