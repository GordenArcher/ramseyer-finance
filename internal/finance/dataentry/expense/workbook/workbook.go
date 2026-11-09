package workbook

import (
	"fmt"
	"time"

	dataentrybulk "ramseyer-finance/internal/finance/dataentry/bulk"

	"github.com/xuri/excelize/v2"
)

const (
	// sheetPrefix names the expenditure tab. Keeping the year in the name matches the file
	// the church already shares, so a downloaded copy opens on the same tab the operator
	// expects.
	sheetPrefix = "EXPENSES "
	// descriptionHeader keeps the office spelling. The header matcher looks for "descr", so
	// changing it to a clean "DESCRIPTION" would still import, but the shared file should
	// look like the one on the office machine.
	descriptionHeader = "DESCRIP TION"
	amountHeader      = "AMOUNT"
	// The widths are copied from the office workbook. Matching them keeps the description
	// readable and the amount column narrow, which is how the shared sheet already looks.
	descriptionWidth = 37.0
	amountWidth      = 10.21875
	categoryWidth    = 19.33203125
	// dateStyle is Excel's built in d-mmm format, the same one the office workbook uses.
	// Reusing the built in id avoids shipping a locale dependent format string.
	dateStyle = 16
	// templateRows is how far down a blank template carries the date and amount formats.
	// Pre-formatting the empty rows means the operator types into cells that already look
	// like the office workbook instead of applying formats by hand.
	templateRows = 1000
)

// amountFormat is the accounting format on the office amount column. It keeps two decimals,
// wraps negatives in brackets, and shows a dash for zero, which is what the church office
// reads all day.
const amountFormat = `_(* #,##0.00_);_(* \(#,##0.00\);_(* "-"??_);_(@_)`

// Row is one saved expenditure line in the office column order.
type Row struct {
	Date        string
	Description string
	Amount      float64
	Category    string
}

// SheetName returns the tab name for a year, for example EXPENSES 2026.
func SheetName(year int) string {
	return fmt.Sprintf("%s%d", sheetPrefix, year)
}

// Build writes the expenditure ledger in the office shape: a blank first row, the
// description and amount headers on row two, and data from row three across date,
// description, amount, and account name. The template is the same builder with no rows, so
// a downloaded template and a saved workbook cannot drift apart.
func Build(year int, rows []Row) (*excelize.File, error) {
	workbook := excelize.NewFile()
	sheet := SheetName(year)
	if err := workbook.SetSheetName(workbook.GetSheetName(0), sheet); err != nil {
		_ = workbook.Close()
		return nil, fmt.Errorf("name expense sheet: %w", err)
	}

	_ = workbook.SetColWidth(sheet, "B", "B", descriptionWidth)
	_ = workbook.SetColWidth(sheet, "C", "C", amountWidth)
	_ = workbook.SetColWidth(sheet, "D", "D", categoryWidth)

	dateFormat, err := workbook.NewStyle(&excelize.Style{NumFmt: dateStyle})
	if err != nil {
		_ = workbook.Close()
		return nil, fmt.Errorf("create expense date style: %w", err)
	}
	amountNumFmt := amountFormat
	amountStyle, err := workbook.NewStyle(&excelize.Style{CustomNumFmt: &amountNumFmt})
	if err != nil {
		_ = workbook.Close()
		return nil, fmt.Errorf("create expense amount style: %w", err)
	}

	if err := workbook.SetCellValue(sheet, "B2", descriptionHeader); err != nil {
		_ = workbook.Close()
		return nil, fmt.Errorf("write expense description header: %w", err)
	}
	if err := workbook.SetCellValue(sheet, "C2", amountHeader); err != nil {
		_ = workbook.Close()
		return nil, fmt.Errorf("write expense amount header: %w", err)
	}
	_ = workbook.SetCellStyle(sheet, "C2", "C2", amountStyle)

	for index, row := range rows {
		rowNumber := index + 3
		if err := setDate(workbook, sheet, rowNumber, row.Date); err != nil {
			_ = workbook.Close()
			return nil, err
		}
		if err := setText(workbook, sheet, rowNumber, 2, row.Description); err != nil {
			_ = workbook.Close()
			return nil, err
		}
		amountCell, _ := excelize.CoordinatesToCellName(3, rowNumber)
		if err := workbook.SetCellValue(sheet, amountCell, row.Amount); err != nil {
			_ = workbook.Close()
			return nil, fmt.Errorf("write expense amount: %w", err)
		}
		if err := setText(workbook, sheet, rowNumber, 4, row.Category); err != nil {
			_ = workbook.Close()
			return nil, err
		}
	}

	if len(rows) > 0 {
		lastRow := len(rows) + 2
		_ = workbook.SetCellStyle(sheet, "A3", fmt.Sprintf("A%d", lastRow), dateFormat)
		_ = workbook.SetCellStyle(sheet, "C3", fmt.Sprintf("C%d", lastRow), amountStyle)
	} else {
		_ = workbook.SetCellStyle(sheet, "A3", fmt.Sprintf("A%d", templateRows), dateFormat)
		_ = workbook.SetCellStyle(sheet, "C3", fmt.Sprintf("C%d", templateRows), amountStyle)
	}

	return workbook, nil
}

// setDate writes a real Excel date when the stored value parses, and falls back to the
// original text when it does not. A corrupt date must not stop the operator from sharing
// the rest of the ledger, but it also must not be silently rewritten.
func setDate(workbook *excelize.File, sheet string, rowNumber int, raw string) error {
	cell, _ := excelize.CoordinatesToCellName(1, rowNumber)
	canonical, ok := dataentrybulk.ParseDate(raw)
	if !ok {
		return setText(workbook, sheet, rowNumber, 1, raw)
	}
	parsed, err := time.Parse("2006-01-02", canonical)
	if err != nil {
		return setText(workbook, sheet, rowNumber, 1, raw)
	}
	if err := workbook.SetCellValue(sheet, cell, parsed); err != nil {
		return fmt.Errorf("write expense date: %w", err)
	}
	return nil
}

func setText(workbook *excelize.File, sheet string, rowNumber, column int, value string) error {
	cell, _ := excelize.CoordinatesToCellName(column, rowNumber)
	if err := workbook.SetCellValue(sheet, cell, value); err != nil {
		return fmt.Errorf("write expense cell: %w", err)
	}
	return nil
}
