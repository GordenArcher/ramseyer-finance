package imprest

import (
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

// imprestWorkbook mirrors the client cash book: a title row, the money column labels, and
// the DETAILS head on the right. Dates appear only on the first row of a group.
func imprestWorkbook(t *testing.T, sheet string, rows [][]any) *excelize.File {
	t.Helper()

	workbook := excelize.NewFile()
	_ = workbook.SetSheetName(workbook.GetSheetName(0), sheet)

	_ = workbook.SetCellValue(sheet, "A1", "IMPREST CASH BOOK")
	_ = workbook.SetCellValue(sheet, "A2", "DATE")
	_ = workbook.SetCellValue(sheet, "B2", "DESCRIPTION")
	_ = workbook.SetCellValue(sheet, "C2", "INCOME")
	_ = workbook.SetCellValue(sheet, "D2", "EXPENSE")
	_ = workbook.SetCellValue(sheet, "E2", "DETAILS")

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
			_ = workbook.SetCellValue(sheet, cell, value)
		}
	}

	return workbook
}

func TestChooseImprestSheetUsesTheYearTab(t *testing.T) {
	workbook := imprestWorkbook(t, "2026", [][]any{
		{"2026-01-20", "TNT to Pay tier 2 and documentation", nil, 35, "T&T"},
		{nil, "Bags of satchet water @6*10", nil, 60, "REFRESHMENT"},
	})
	defer workbook.Close()

	if _, err := workbook.NewSheet("2025"); err != nil {
		t.Fatalf("create prior sheet: %v", err)
	}
	_ = workbook.SetCellValue("2025", "A1", "OLD BOOK")
	_ = workbook.SetCellValue("2025", "E1", "DETAILS")

	name, layout, _, err := ChooseImprestSheet(workbook, 2026)
	if err != nil {
		t.Fatalf("ChooseImprestSheet failed: %v", err)
	}
	if name != "2026" {
		t.Fatalf("chosen sheet = %q, want 2026", name)
	}
	if layout.Date != 0 || layout.Description != 1 || layout.Amount != 3 || layout.Category != 4 {
		t.Fatalf("layout = %+v, want date A, description B, amount D, category E", layout)
	}
}

func TestChooseImprestSheetFallsBackToTheOnlyCashBookTab(t *testing.T) {
	workbook := imprestWorkbook(t, "Cash Book", [][]any{
		{"2026-01-20", "TNT to Pay tier 2 and documentation", nil, 35, "T&T"},
	})
	defer workbook.Close()

	name, layout, _, err := ChooseImprestSheet(workbook, 2026)
	if err != nil {
		t.Fatalf("ChooseImprestSheet failed: %v", err)
	}
	if name != "Cash Book" {
		t.Fatalf("chosen sheet = %q, want the renamed cash book tab", name)
	}
	if layout.Amount != 3 || layout.Category != 4 {
		t.Fatalf("layout = %+v, want the expense and details columns", layout)
	}
}

func TestReadImprestRowsCarriesTheDateDown(t *testing.T) {
	workbook := imprestWorkbook(t, "2026", [][]any{
		{"2026-01-20", "TNT to Pay tier 2 and documentation", nil, 35, "T&T"},
		{nil, "Bags of satchet water @6*10", nil, 60, "REFRESHMENT"},
		{"2026-02-03", "TNT to bank", nil, 36, "T&T"},
	})
	defer workbook.Close()

	_, layout, rows, err := ChooseImprestSheet(workbook, 2026)
	if err != nil {
		t.Fatalf("ChooseImprestSheet failed: %v", err)
	}
	entries, rowErrors, skipped := ReadImprestRows(rows, layout)
	if len(rowErrors) != 0 || skipped != 0 {
		t.Fatalf("row errors = %v, skipped = %d, want a clean read", rowErrors, skipped)
	}
	if len(entries) != 3 {
		t.Fatalf("entry count = %d, want 3", len(entries))
	}
	if entries[0].Date != "2026-01-20" || entries[1].Date != "2026-01-20" {
		t.Fatalf("carried dates = %q and %q, want both on 2026-01-20", entries[0].Date, entries[1].Date)
	}
	if entries[2].Date != "2026-02-03" {
		t.Fatalf("third date = %q, want the new group date", entries[2].Date)
	}
	if entries[1].Amount != 60 || entries[1].Category != "REFRESHMENT" {
		t.Fatalf("carried row = %+v, want the expense amount and DETAILS head", entries[1])
	}
}

func TestReadImprestRowsAnchorsTheLeadingUndatedGroup(t *testing.T) {
	workbook := imprestWorkbook(t, "2026", [][]any{
		// The client's book starts with payments but no date on the first group.
		{nil, "Tnt to Osu for children service manuals", nil, 40, "T&T"},
		{nil, "Bags of satchet water @6*18", nil, 108, "REFRESHMENT"},
		{"2026-01-20", "TNT to Pay tier 2 and documentation", nil, 35, "T&T"},
	})
	defer workbook.Close()

	_, layout, rows, err := ChooseImprestSheet(workbook, 2026)
	if err != nil {
		t.Fatalf("ChooseImprestSheet failed: %v", err)
	}
	entries, rowErrors, skipped := ReadImprestRows(rows, layout)
	if len(rowErrors) != 0 || skipped != 0 {
		t.Fatalf("row errors = %v, skipped = %d, want the leading group anchored cleanly", rowErrors, skipped)
	}
	if len(entries) != 3 {
		t.Fatalf("entry count = %d, want 3", len(entries))
	}
	for index := 0; index < 2; index++ {
		if entries[index].Date != "2026-01-20" {
			t.Fatalf("leading entry %d date = %q, want the first written date", index, entries[index].Date)
		}
	}
}

func TestReadImprestRowsReportsEveryRefusal(t *testing.T) {
	workbook := imprestWorkbook(t, "2026", [][]any{
		{"2026-01-20", "TNT to Pay tier 2", nil, 35, "T&T"},
		// Missing amount.
		{nil, "Cleaning soap", nil, nil, "CLEANING"},
		// Missing DETAILS head.
		{nil, "Fuel", nil, 80, nil},
		// Missing description.
		{nil, nil, nil, 20, "OTHERS"},
		// Summary row must be skipped without an error.
		{"CLOSING BAL", nil, nil, 477.03, nil},
	})
	defer workbook.Close()

	_, layout, rows, err := ChooseImprestSheet(workbook, 2026)
	if err != nil {
		t.Fatalf("ChooseImprestSheet failed: %v", err)
	}
	entries, rowErrors, skipped := ReadImprestRows(rows, layout)
	if len(entries) != 1 {
		t.Fatalf("entry count = %d, want only the valid row", len(entries))
	}
	if entries[0].Description != "TNT to Pay tier 2" {
		t.Fatalf("valid entry = %+v", entries[0])
	}
	if len(rowErrors) != 3 {
		t.Fatalf("row errors = %v, want three refusals", rowErrors)
	}
	if skipped != 4 {
		t.Fatalf("skipped = %d, want three refusals plus the summary row", skipped)
	}
	for _, expected := range []string{
		"expense amount is missing",
		"DETAILS category is missing",
		"description is missing",
	} {
		if !containsImprestMessage(rowErrors, expected) {
			t.Fatalf("row errors %v did not include %q", rowErrors, expected)
		}
	}
}

func TestReadImprestRowsRefusesASheetWithNoDateAnywhere(t *testing.T) {
	workbook := imprestWorkbook(t, "2026", [][]any{
		{nil, "Water", nil, 60, "REFRESHMENT"},
		{nil, "Soap", nil, 20, "CLEANING"},
	})
	defer workbook.Close()

	_, layout, rows, err := ChooseImprestSheet(workbook, 2026)
	if err != nil {
		t.Fatalf("ChooseImprestSheet failed: %v", err)
	}
	entries, rowErrors, skipped := ReadImprestRows(rows, layout)
	if len(entries) != 0 || skipped != 2 || len(rowErrors) != 2 {
		t.Fatalf("entries = %d errors = %v skipped = %d, want every row refused", len(entries), rowErrors, skipped)
	}
	if !containsImprestMessage(rowErrors, "the sheet has no date to carry") {
		t.Fatalf("row errors = %v, want a missing-date refusal", rowErrors)
	}
}

func TestReadImprestRowsRejectsAnUnreadableGroupDate(t *testing.T) {
	workbook := imprestWorkbook(t, "2026", [][]any{
		{"not a date", "Water", nil, 60, "REFRESHMENT"},
		{"2026-01-20", "TNT to Pay tier 2", nil, 35, "T&T"},
	})
	defer workbook.Close()

	_, layout, rows, err := ChooseImprestSheet(workbook, 2026)
	if err != nil {
		t.Fatalf("ChooseImprestSheet failed: %v", err)
	}
	entries, rowErrors, skipped := ReadImprestRows(rows, layout)
	if len(entries) != 1 || entries[0].Date != "2026-01-20" {
		t.Fatalf("entries = %+v, want only the readable row", entries)
	}
	if len(rowErrors) != 1 || skipped != 1 {
		t.Fatalf("row errors = %v skipped = %d, want one refusal", rowErrors, skipped)
	}
	if !containsImprestMessage(rowErrors, "date is not a real date") {
		t.Fatalf("row errors = %v, want a date refusal", rowErrors)
	}
}

func containsImprestMessage(messages []string, needle string) bool {
	for _, message := range messages {
		if strings.Contains(message, needle) {
			return true
		}
	}
	return false
}
