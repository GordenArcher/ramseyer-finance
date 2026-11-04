package bulk

import (
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestBuildExpenseWorkbookMatchesTheClientLayout(t *testing.T) {
	workbook, err := BuildExpenseWorkbook(2026, []ExpenseRow{
		{Date: "2026-01-04", Description: "PLAN OF WORK", Amount: 456, Category: "Printing & Stationery"},
	})
	if err != nil {
		t.Fatalf("BuildExpenseWorkbook failed: %v", err)
	}
	defer workbook.Close()

	sheet := ExpenseSheetName(2026)
	if sheet != "EXPENSES 2026" {
		t.Fatalf("sheet name = %q, want EXPENSES 2026", sheet)
	}
	if names := workbook.GetSheetList(); len(names) != 1 || names[0] != sheet {
		t.Fatalf("sheets = %v, want only %s", names, sheet)
	}

	rows, err := workbook.GetRows(sheet, excelize.Options{RawCellValue: true})
	if err != nil {
		t.Fatalf("read workbook rows: %v", err)
	}
	if len(rows) < 3 {
		t.Fatalf("workbook has %d rows, want header and data", len(rows))
	}
	// The client file leaves row 1 empty and puts the two labels on row 2.
	for column, value := range rows[0] {
		if value != "" {
			t.Fatalf("row 1 column %d = %q, want blank", column+1, value)
		}
	}
	if rows[1][1] != "DESCRIP TION" || rows[1][2] != "AMOUNT" {
		t.Fatalf("header row = %v, want DESCRIP TION and AMOUNT on B and C", rows[1])
	}
	if rows[2][0] != "46026" {
		t.Fatalf("date cell = %q, want the Excel serial for 2026-01-04", rows[2][0])
	}
	if rows[2][1] != "PLAN OF WORK" || rows[2][2] != "456" || rows[2][3] != "Printing & Stationery" {
		t.Fatalf("data row = %v, want description, amount, and category", rows[2])
	}
}

func TestBuildExpenseWorkbookLeavesABlankTemplateWithTheSameHeaders(t *testing.T) {
	workbook, err := BuildExpenseWorkbook(2026, nil)
	if err != nil {
		t.Fatalf("BuildExpenseWorkbook failed: %v", err)
	}
	defer workbook.Close()

	sheet := ExpenseSheetName(2026)
	if value, _ := workbook.GetCellValue(sheet, "B2"); value != "DESCRIP TION" {
		t.Fatalf("template description header = %q", value)
	}
	if value, _ := workbook.GetCellValue(sheet, "A3"); value != "" {
		t.Fatalf("blank template wrote a data row: A3 = %q", value)
	}
}

func TestBuildEntryTemplateStartsWithTheHeaderRow(t *testing.T) {
	workbook, err := BuildEntryTemplate("income", []Category{{ID: 1, Name: "Offerings"}})
	if err != nil {
		t.Fatalf("BuildEntryTemplate failed: %v", err)
	}
	defer workbook.Close()

	if names := workbook.GetSheetList(); len(names) == 0 || names[0] != "income" {
		t.Fatalf("template sheets = %v, want income first", names)
	}
	if value, _ := workbook.GetCellValue("income", "B1"); value != "Description" {
		t.Fatalf("template description header = %q", value)
	}
}
