package bulk

import (
	"fmt"

	"github.com/xuri/excelize/v2"
)

const (
	// templateColumnWidths keeps the sample sheet readable when the operator opens it.
	// Anything wider forces horizontal scrolling on a laptop before the first example row.
	templateDateWidth        = 14
	templateDescriptionWidth = 42
	templateAmountWidth      = 14
	templateCategoryWidth    = 34
	templatePaymentWidth     = 18
	templateInstructionWidth = 90
	// templateAmountStyle is Excel's built in comma separated two decimal format. Reusing
	// the built in id avoids shipping a format string that changes with locale.
	templateAmountStyle = 4
)

// BuildEntryTemplate returns a workbook in the shape the importer reads for income, asset,
// and liability entries. Generating the template from the same package that parses uploads
// is the point: if the two drift apart the operator gets a file the importer rejects, and
// that is the most confusing failure this feature can have.
func BuildEntryTemplate(transactionType string, categories []Category) (*excelize.File, error) {
	workbook := excelize.NewFile()
	sheet := transactionType
	if err := workbook.SetSheetName(workbook.GetSheetName(0), sheet); err != nil {
		_ = workbook.Close()
		return nil, fmt.Errorf("name template sheet: %w", err)
	}

	headers := []string{"Date", "Description", "Amount", "Category", "Payment Method"}
	for index, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(index+1, 1)
		if err := workbook.SetCellValue(sheet, cell, header); err != nil {
			_ = workbook.Close()
			return nil, fmt.Errorf("write template header: %w", err)
		}
	}

	_ = workbook.SetColWidth(sheet, "A", "A", templateDateWidth)
	_ = workbook.SetColWidth(sheet, "B", "B", templateDescriptionWidth)
	_ = workbook.SetColWidth(sheet, "C", "C", templateAmountWidth)
	_ = workbook.SetColWidth(sheet, "D", "D", templateCategoryWidth)
	_ = workbook.SetColWidth(sheet, "E", "E", templatePaymentWidth)

	// The example uses a real account name from the database so the operator sees the
	// exact spelling the category matcher expects.
	exampleCategory := ""
	if len(categories) > 0 {
		exampleCategory = categories[0].Name
	}
	example := []string{"2026-01-04", "Example: Sunday offering", "100.00", exampleCategory, "cash"}
	for index, value := range example {
		cell, _ := excelize.CoordinatesToCellName(index+1, 2)
		if err := workbook.SetCellValue(sheet, cell, value); err != nil {
			_ = workbook.Close()
			return nil, fmt.Errorf("write template example: %w", err)
		}
	}

	if amountStyle, err := workbook.NewStyle(&excelize.Style{NumFmt: templateAmountStyle}); err == nil {
		_ = workbook.SetCellStyle(sheet, "C2", "C1000", amountStyle)
	}

	if err := AddEntryTemplateInstructions(workbook, transactionType, categories); err != nil {
		_ = workbook.Close()
		return nil, err
	}

	return workbook, nil
}

// AddEntryTemplateInstructions adds a second sheet. The operator prints or reads it beside
// the data sheet, and listing the valid account names there prevents the most common failed
// upload: a category that is close to an account name but not equal to it.
func AddEntryTemplateInstructions(workbook *excelize.File, transactionType string, categories []Category) error {
	const sheetName = "Instructions"
	if _, err := workbook.NewSheet(sheetName); err != nil {
		return fmt.Errorf("add template instructions: %w", err)
	}

	lines := [][]string{
		{"Bulk entry upload instructions"},
		{""},
		{"Date", "Required. Use YYYY-MM-DD, for example 2026-01-04, or a real Excel date cell."},
		{"Description", "Required for asset and liability entries, optional for income and expenditure."},
		{"Amount", "Required. Numbers only. Income and expenditure must be greater than zero; asset and liability accept negative movements."},
		{"Category", "Required. Must match an existing " + transactionType + " account name. Uploading never creates an account."},
	}
	if transactionType == "expenditure" {
		lines = append(lines, []string{"Layout", "Use the EXPENSES sheet. Row 1 is blank, row 2 has the labels, and data starts on row 3."})
		lines = append(lines, []string{"Payment Method", "Not read from the expenditure sheet. Entries are saved as cash."})
	} else {
		lines = append(lines, []string{"Payment Method", "Optional. One of cash, momo, cheque, or bank. Defaults to cash."})
	}
	lines = append(lines,
		[]string{""},
		[]string{"Ignored rows", "Blank rows, subtotals, and opening or closing balance lines are skipped."},
		[]string{""},
		[]string{"Existing " + transactionType + " accounts:", ""},
	)
	for _, category := range categories {
		lines = append(lines, []string{"", category.Name})
	}

	for rowIndex, line := range lines {
		for column, value := range line {
			if value == "" {
				continue
			}
			cell, _ := excelize.CoordinatesToCellName(column+1, rowIndex+1)
			if err := workbook.SetCellValue(sheetName, cell, value); err != nil {
				return fmt.Errorf("write template instructions: %w", err)
			}
		}
	}

	_ = workbook.SetColWidth(sheetName, "A", "A", templateCategoryWidth)
	_ = workbook.SetColWidth(sheetName, "B", "B", templateInstructionWidth)
	return nil
}
