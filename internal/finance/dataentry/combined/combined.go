package combined

import (
	"fmt"
	"strings"

	"ramseyer-finance/internal/finance/dataentry/sheet"
	"ramseyer-finance/internal/finance/dataentry/template"

	"github.com/xuri/excelize/v2"
)

const (
	// combinedSheetName is the worksheet the all-types template uses. The name is not part of
	// the upload contract, so an operator can rename it; sheet selection is header driven.
	combinedSheetName = "All Entries"
)

// combinedHeaders is the one row the all-types upload needs. Type sits beside Date because
// it decides which account namespace the rest of the row is matched against.
var combinedHeaders = []string{"Date", "Type", "Description", "Amount", "Category", "Payment Method"}

// NormalizeTransactionType maps the Type cell onto the four namespaces the rest of the app
// uses. Plurals and the common "expense" spelling are accepted because those are the words
// that appear in real workbooks, but nothing is guessed: anything else is refused.
func NormalizeTransactionType(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "income", "receipt", "receipts":
		return "income", true
	case "expenditure", "expense", "expenses":
		return "expenditure", true
	case "asset", "assets":
		return "asset", true
	case "liability", "liabilities":
		return "liability", true
	default:
		return "", false
	}
}

// ChooseCombinedSheet picks the worksheet that carries the six all-types columns. Unlike
// ChooseSheet it cannot fall back to an inferred category column, because a wrong inference
// would send a row to the wrong type's chart of accounts.
func ChooseCombinedSheet(workbook *excelize.File, requested string) (string, sheet.ColumnLayout, [][]string, error) {
	sheetNames := workbook.GetSheetList()
	if len(sheetNames) == 0 {
		return "", sheet.ColumnLayout{}, nil, fmt.Errorf("the workbook has no worksheets")
	}

	if requested != "" {
		for _, name := range sheetNames {
			if !strings.EqualFold(name, requested) {
				continue
			}
			layout, rows, err := sheet.InspectSheet(workbook, name)
			if err != nil {
				return "", sheet.ColumnLayout{}, nil, err
			}
			if !combinedLayoutReady(layout) {
				return "", sheet.ColumnLayout{}, nil, fmt.Errorf("worksheet %q needs Date, Type, Description, Amount, and Category columns", requested)
			}
			return name, layout, rows, nil
		}
		return "", sheet.ColumnLayout{}, nil, fmt.Errorf("worksheet %q was not found in the workbook", requested)
	}

	best := sheet.Candidate{Score: -1}
	for _, name := range sheetNames {
		layout, rows, err := sheet.InspectSheet(workbook, name)
		if err != nil || !combinedLayoutReady(layout) {
			continue
		}
		score := sheet.ScoreSheet(rows, layout)
		if score > best.Score {
			best = sheet.Candidate{Name: name, Layout: layout, Rows: rows, Score: score}
		}
	}
	if best.Score < 0 {
		return "", sheet.ColumnLayout{}, nil, fmt.Errorf("no worksheet with Date, Type, Description, Amount, and Category columns was found")
	}
	return best.Name, best.Layout, best.Rows, nil
}

// combinedLayoutReady is deliberately stricter than the per-type check. Type and Category
// must be labelled in the sheet, because the upload has no other reliable way to know which
// account namespace a row belongs to.
func combinedLayoutReady(layout sheet.ColumnLayout) bool {
	return layout.HeaderRow >= 0 &&
		layout.Date >= 0 &&
		layout.TransactionType >= 0 &&
		layout.Amount >= 0 &&
		layout.CategoryLabeled
}

// BuildCombinedTemplate returns the one-sheet workbook the all-types upload reads. The
// example rows deliberately use different types so the operator sees that one row per entry
// is the whole format.
func BuildCombinedTemplate() (*excelize.File, error) {
	workbook := excelize.NewFile()
	if err := workbook.SetSheetName(workbook.GetSheetName(0), combinedSheetName); err != nil {
		_ = workbook.Close()
		return nil, fmt.Errorf("name combined template sheet: %w", err)
	}

	for index, header := range combinedHeaders {
		cell, _ := excelize.CoordinatesToCellName(index+1, 1)
		if err := workbook.SetCellValue(combinedSheetName, cell, header); err != nil {
			_ = workbook.Close()
			return nil, fmt.Errorf("write combined template header: %w", err)
		}
	}

	examples := [][]string{
		{"2026-01-04", "income", "Sunday offering", "1000.00", "Tithes", "cash"},
		{"2026-01-04", "expenditure", "Plan of work", "456.00", "Printing & Stationery", "cash"},
	}
	for rowIndex, example := range examples {
		for column, value := range example {
			cell, _ := excelize.CoordinatesToCellName(column+1, rowIndex+2)
			if err := workbook.SetCellValue(combinedSheetName, cell, value); err != nil {
				_ = workbook.Close()
				return nil, fmt.Errorf("write combined template example: %w", err)
			}
		}
	}

	_ = workbook.SetColWidth(combinedSheetName, "A", "A", template.DateWidth)
	_ = workbook.SetColWidth(combinedSheetName, "B", "B", 18)
	_ = workbook.SetColWidth(combinedSheetName, "C", "C", template.DescriptionWidth)
	_ = workbook.SetColWidth(combinedSheetName, "D", "D", template.AmountWidth)
	_ = workbook.SetColWidth(combinedSheetName, "E", "E", template.CategoryWidth)
	_ = workbook.SetColWidth(combinedSheetName, "F", "F", template.PaymentWidth)

	if err := addCombinedInstructions(workbook); err != nil {
		_ = workbook.Close()
		return nil, err
	}
	return workbook, nil
}

// addCombinedInstructions keeps the rules beside the data so the operator does not have to
// remember that Type controls which accounts are valid.
func addCombinedInstructions(workbook *excelize.File) error {
	const sheetName = "Instructions"
	if _, err := workbook.NewSheet(sheetName); err != nil {
		return fmt.Errorf("add combined template instructions: %w", err)
	}

	lines := [][]string{
		{"All-types upload instructions"},
		{""},
		{"Date", "Required. Use YYYY-MM-DD, for example 2026-01-04, or a real Excel date cell."},
		{"Type", "Required. One of income, expenditure, asset, or liability."},
		{"Description", "Required for asset and liability entries, optional for income and expenditure."},
		{"Amount", "Required. Numbers only. Income and expenditure must be greater than zero; asset and liability accept negative movements."},
		{"Category", "Required. Must match an existing account under the row's Type. An expenditure head missing from the chart is added under Other Expenditure."},
		{"Payment Method", "Optional. One of cash, momo, cheque, or bank. Defaults to cash."},
		{""},
		{"Ignored rows", "Blank rows, subtotals, and opening or closing balance lines are skipped."},
	}

	for rowIndex, line := range lines {
		for column, value := range line {
			if value == "" {
				continue
			}
			cell, _ := excelize.CoordinatesToCellName(column+1, rowIndex+1)
			if err := workbook.SetCellValue(sheetName, cell, value); err != nil {
				return fmt.Errorf("write combined template instructions: %w", err)
			}
		}
	}

	_ = workbook.SetColWidth(sheetName, "A", "A", template.CategoryWidth)
	_ = workbook.SetColWidth(sheetName, "B", "B", template.InstructionWidth)
	return nil
}
