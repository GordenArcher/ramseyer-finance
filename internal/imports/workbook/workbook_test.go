package workbook

import (
	"bytes"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func TestRowsReturnsRawValues(t *testing.T) {
	t.Parallel()

	file := excelize.NewFile()
	defer func() {
		_ = file.Close()
	}()
	sheet := file.GetSheetName(0)

	if err := file.SetCellValue(sheet, "A1", "Sunday Service Offering"); err != nil {
		t.Fatalf("set label: %v", err)
	}
	if err := file.SetCellValue(sheet, "B1", 1234.5); err != nil {
		t.Fatalf("set amount: %v", err)
	}
	if err := file.SetCellValue(sheet, "C1", time.Date(2026, time.January, 4, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("set date: %v", err)
	}

	var buffer bytes.Buffer
	if err := file.Write(&buffer); err != nil {
		t.Fatalf("write workbook: %v", err)
	}

	document, err := Open(buffer.Bytes())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		_ = document.Close()
	}()

	rows, err := document.Rows(sheet)
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if Cell(rows[0], 0) != "Sunday Service Offering" {
		t.Fatalf("label = %q", Cell(rows[0], 0))
	}
	if Cell(rows[0], 1) != "1234.5" {
		t.Fatalf("amount = %q, want raw 1234.5", Cell(rows[0], 1))
	}

	date, ok := ParseDate(Cell(rows[0], 2))
	if !ok || date.Format("2006-01-02") != "2026-01-04" {
		t.Fatalf("date = %s ok=%v, want 2026-01-04", date.Format("2006-01-02"), ok)
	}
}

func TestParseDateRejectsMoneySerials(t *testing.T) {
	t.Parallel()

	if _, ok := ParseDate("3112"); ok {
		t.Fatal("a 3112 amount must not be read as a date")
	}
	if date, ok := ParseDate("46026"); !ok || date.Format("2006-01-02") != "2026-01-04" {
		t.Fatalf("serial 46026 = %s ok=%v, want 2026-01-04", date.Format("2006-01-02"), ok)
	}
	if date, ok := ParseDate("2026-01-04"); !ok || date.Format("2006-01-02") != "2026-01-04" {
		t.Fatalf("text date = %s ok=%v, want 2026-01-04", date.Format("2006-01-02"), ok)
	}
}

func TestCellTreatsMissingColumnsAsEmpty(t *testing.T) {
	t.Parallel()

	row := []string{"label"}
	if Cell(row, 4) != "" {
		t.Fatal("a missing column should read as empty")
	}
	if Cell(row, -1) != "" {
		t.Fatal("a negative index should read as empty")
	}
}

func TestOpenRejectsEmptyInput(t *testing.T) {
	t.Parallel()

	if _, err := Open(nil); err == nil {
		t.Fatal("expected an error for empty input")
	}
}
