package sheet

import (
	"testing"

	"github.com/xuri/excelize/v2"
)

// clientLedgerWorkbook mirrors the client file: a bank book tab with date and amount
// headers, and an expenditure ledger with a blank first row, the description and amount
// labels on row two, and the account head in the unlabelled column after the amount.
func clientLedgerWorkbook(t *testing.T, rows [][]any) *excelize.File {
	t.Helper()

	workbook := excelize.NewFile()
	bankBook := workbook.GetSheetName(0)
	_ = workbook.SetSheetName(bankBook, "Sheet1")
	_ = workbook.SetCellValue(bankBook, "A1", "DATE")
	_ = workbook.SetCellValue(bankBook, "B1", "AMOUNT(GHC)")
	_ = workbook.SetCellValue(bankBook, "C1", "CHECQUE  NO. ")
	_ = workbook.SetCellValue(bankBook, "D1", "DESIGNATION ")
	_ = workbook.SetCellValue(bankBook, "A2", "closing bal.")

	ledger := "EXPENSES 2026"
	if _, err := workbook.NewSheet(ledger); err != nil {
		t.Fatalf("create ledger sheet: %v", err)
	}
	// Row 1 is left blank on purpose, matching the client file.
	_ = workbook.SetCellValue(ledger, "A2", "2026-01-04")
	_ = workbook.SetCellValue(ledger, "B2", "DESCRIP TION")
	_ = workbook.SetCellValue(ledger, "C2", "AMOUNT")

	for index, row := range rows {
		rowNumber := index + 3
		for column, value := range row {
			if value == nil {
				continue
			}
			cell, err := excelize.CoordinatesToCellName(column+1, rowNumber)
			if err != nil {
				t.Fatalf("coordinates for row %d: %v", rowNumber, err)
			}
			_ = workbook.SetCellValue(ledger, cell, value)
		}
	}

	return workbook
}

func TestChooseSheetPrefersTheExpenseLedger(t *testing.T) {
	workbook := clientLedgerWorkbook(t, [][]any{
		{"2026-01-04", "PLAN OF WORK", 456, "Printing & Stationery"},
		{"2026-01-04", "WATER", 400, "Utilities-Manse"},
	})
	defer workbook.Close()

	name, layout, _, err := ChooseSheet(workbook, "", "expenditure")
	if err != nil {
		t.Fatalf("ChooseSheet failed: %v", err)
	}
	if name != "EXPENSES 2026" {
		t.Fatalf("chosen sheet = %q, want EXPENSES 2026", name)
	}
	if layout.Date != 0 || layout.Description != 1 || layout.Amount != 2 || layout.Category != 3 {
		t.Fatalf("layout = %+v, want date/description/amount/category on A/B/C/D", layout)
	}
}

func TestChooseSheetRejectsTheBankBookForExpenditure(t *testing.T) {
	workbook := excelize.NewFile()
	_ = workbook.SetSheetName(workbook.GetSheetName(0), "Sheet1")
	_ = workbook.SetCellValue("Sheet1", "A1", "DATE")
	_ = workbook.SetCellValue("Sheet1", "B1", "AMOUNT(GHC)")
	_ = workbook.SetCellValue("Sheet1", "C1", "CHECQUE  NO. ")
	_ = workbook.SetCellValue("Sheet1", "D1", "DESIGNATION ")
	_ = workbook.SetCellValue("Sheet1", "A2", "2026-01-04")
	_ = workbook.SetCellValue("Sheet1", "B2", 4000)
	_ = workbook.SetCellValue("Sheet1", "C2", 3965)
	_ = workbook.SetCellValue("Sheet1", "D2", "Part payment")
	_ = workbook.SetCellValue("Sheet1", "E2", "END OF YR GET TOGETHER")
	defer workbook.Close()

	if _, _, _, err := ChooseSheet(workbook, "", "expenditure"); err == nil {
		t.Fatalf("bank book was accepted as an expenditure ledger")
	}
}

func TestChooseSheetUsesTheImportableRowsNotTheTabName(t *testing.T) {
	workbook := excelize.NewFile()
	_ = workbook.SetSheetName(workbook.GetSheetName(0), "Book")
	_ = workbook.SetCellValue("Book", "A1", "Date")
	_ = workbook.SetCellValue("Book", "B1", "Description")
	_ = workbook.SetCellValue("Book", "C1", "Amount")
	_ = workbook.SetCellValue("Book", "D1", "Category")
	for row := 2; row <= 20; row++ {
		for column, value := range []any{"2026-01-04", "BOOK ROW", 100, "Printing & Stationery"} {
			cell, _ := excelize.CoordinatesToCellName(column+1, row)
			_ = workbook.SetCellValue("Book", cell, value)
		}
	}
	if _, err := workbook.NewSheet("EXPENSES 2026"); err != nil {
		t.Fatalf("create ledger sheet: %v", err)
	}
	_ = workbook.SetCellValue("EXPENSES 2026", "A2", "2026-01-04")
	_ = workbook.SetCellValue("EXPENSES 2026", "B2", "DESCRIP TION")
	_ = workbook.SetCellValue("EXPENSES 2026", "C2", "AMOUNT")
	_ = workbook.SetCellValue("EXPENSES 2026", "A3", "2026-01-04")
	_ = workbook.SetCellValue("EXPENSES 2026", "B3", "PLAN OF WORK")
	_ = workbook.SetCellValue("EXPENSES 2026", "C3", 456)
	_ = workbook.SetCellValue("EXPENSES 2026", "D3", "Printing & Stationery")
	defer workbook.Close()

	// The tab name no longer decides. The sheet with the most importable rows wins.
	name, _, _, err := ChooseSheet(workbook, "", "expenditure")
	if err != nil {
		t.Fatalf("ChooseSheet failed: %v", err)
	}
	if name != "Book" {
		t.Fatalf("chosen sheet = %q, want Book", name)
	}
}

func TestChooseSheetAcceptsARenamedExpenseLedger(t *testing.T) {
	workbook := excelize.NewFile()
	bankBook := workbook.GetSheetName(0)
	_ = workbook.SetSheetName(bankBook, "Sheet1")
	_ = workbook.SetCellValue(bankBook, "A1", "DATE")
	_ = workbook.SetCellValue(bankBook, "B1", "AMOUNT(GHC)")
	_ = workbook.SetCellValue(bankBook, "C1", "CHECQUE  NO. ")
	_ = workbook.SetCellValue(bankBook, "D1", "DESIGNATION ")
	_ = workbook.SetCellValue(bankBook, "A2", "2026-01-04")
	_ = workbook.SetCellValue(bankBook, "B2", 4000)
	_ = workbook.SetCellValue(bankBook, "C2", 3965)
	_ = workbook.SetCellValue(bankBook, "D2", "Part payment")
	_ = workbook.SetCellValue(bankBook, "E2", "END OF YR GET TOGETHER")

	const ledger = "Office Book"
	if _, err := workbook.NewSheet(ledger); err != nil {
		t.Fatalf("create ledger sheet: %v", err)
	}
	_ = workbook.SetCellValue(ledger, "A2", "2026-01-04")
	_ = workbook.SetCellValue(ledger, "B2", "DESCRIP TION")
	_ = workbook.SetCellValue(ledger, "C2", "AMOUNT")
	for row := 3; row <= 12; row++ {
		for column, value := range []any{"2026-01-04", "PLAN OF WORK", 456, "Printing & Stationery"} {
			cell, _ := excelize.CoordinatesToCellName(column+1, row)
			_ = workbook.SetCellValue(ledger, cell, value)
		}
	}
	defer workbook.Close()

	name, layout, _, err := ChooseSheet(workbook, "", "expenditure")
	if err != nil {
		t.Fatalf("ChooseSheet failed: %v", err)
	}
	if name != ledger {
		t.Fatalf("chosen sheet = %q, want %s", name, ledger)
	}
	if layout.Category != 3 {
		t.Fatalf("category column = %d, want D", layout.Category)
	}
}

func TestChooseSheetAcceptsAGenericSheetWithACategoryHeader(t *testing.T) {
	workbook := excelize.NewFile()
	_ = workbook.SetCellValue("Sheet1", "A1", "Date")
	_ = workbook.SetCellValue("Sheet1", "B1", "Description")
	_ = workbook.SetCellValue("Sheet1", "C1", "Amount")
	_ = workbook.SetCellValue("Sheet1", "D1", "Category")
	_ = workbook.SetCellValue("Sheet1", "A2", "2026-01-04")
	_ = workbook.SetCellValue("Sheet1", "B2", "PLAN OF WORK")
	_ = workbook.SetCellValue("Sheet1", "C2", 456)
	_ = workbook.SetCellValue("Sheet1", "D2", "Printing & Stationery")
	defer workbook.Close()

	name, layout, _, err := ChooseSheet(workbook, "", "expenditure")
	if err != nil {
		t.Fatalf("ChooseSheet failed: %v", err)
	}
	if name != "Sheet1" {
		t.Fatalf("chosen sheet = %q, want Sheet1", name)
	}
	if !layout.CategoryLabeled || layout.Category != 3 {
		t.Fatalf("category layout = %+v, want the labelled column D", layout)
	}
}
