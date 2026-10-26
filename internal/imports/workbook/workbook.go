// Package workbook reads the populated rows of an Excel workbook. The importer owns
// the meaning of those rows; this package only hides the spreadsheet library behind a
// small, testable surface so the parsing rules do not depend on excelize directly.
package workbook

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// Workbook is an open spreadsheet file. Call Close when the caller is done with it.
type Workbook struct {
	file *excelize.File
}

// Open reads an .xlsx file from memory. Reading from memory keeps the HTTP upload and
// the tests on the same path and avoids leaving temporary files behind after a preview.
func Open(data []byte) (*Workbook, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("workbook is empty")
	}

	file, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open workbook: %w", err)
	}
	return &Workbook{file: file}, nil
}

// SheetNames lists sheets in the order they appear in the workbook. The order matters
// for the import report because it lets the operator follow the workbook as they know it.
func (w *Workbook) SheetNames() []string {
	if w == nil || w.file == nil {
		return nil
	}
	return w.file.GetSheetList()
}

// Rows returns the populated rows of a sheet using raw stored values. Raw values keep
// dates as Excel serial numbers and amounts free of display formatting, so the planner
// can parse them without depending on the workbook's cell styles. excelize omits trailing
// empty columns, so callers must use Cell instead of indexing a row directly.
func (w *Workbook) Rows(sheet string) ([][]string, error) {
	if w == nil || w.file == nil {
		return nil, fmt.Errorf("workbook is not open")
	}

	rows, err := w.file.GetRows(sheet, excelize.Options{RawCellValue: true})
	if err != nil {
		return nil, fmt.Errorf("read sheet %q: %w", sheet, err)
	}
	return trimTrailingEmptyRows(rows), nil
}

// Close releases the workbook's internal resources.
func (w *Workbook) Close() error {
	if w == nil || w.file == nil {
		return nil
	}
	return w.file.Close()
}

// Cell returns the value at column index in row, or an empty string when the row is
// shorter than the requested column. Spreadsheet rows are sparse, and treating a missing
// cell as empty keeps label and amount parsing free of bounds checks.
func Cell(row []string, index int) string {
	if index < 0 || index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}

// dateLayouts cover workbooks that store a date as text. Excel normally stores dates as
// serial numbers, so these are a fallback rather than the main path.
var dateLayouts = []string{
	"2006-01-02",
	"2006-01-02 15:04:05",
	"2 Jan 2006",
	"2 January 2006",
	"Jan 2, 2006",
}

// Excel date serials are just numbers, so every amount in the workbook can be read as a
// date if the range is not bounded. The church's books are live, so a serial outside this
// range is a money cell or a corrupted value, not a date.
const (
	minDateYear = 2000
	maxDateYear = 2100
)

// ParseDate reads a raw cell value as a date. Raw mode returns Excel dates as serial
// numbers, and this converts them with the library's own date handling so the 1900 leap
// year quirk stays in one place. The planner deals in calendar dates only, so the time
// component is dropped.
func ParseDate(value string) (time.Time, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, false
	}

	if serial, err := strconv.ParseFloat(trimmed, 64); err == nil {
		parsed, err := excelize.ExcelDateToTime(serial, false)
		if err == nil && parsed.Year() >= minDateYear && parsed.Year() <= maxDateYear {
			return time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, time.UTC), true
		}
		return time.Time{}, false
	}

	for _, layout := range dateLayouts {
		if parsed, err := time.ParseInLocation(layout, trimmed, time.UTC); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// trimTrailingEmptyRows removes rows that are completely empty at the end of a sheet.
// excelize already trims empty trailing columns, but a sheet can carry styled empty rows
// that would otherwise look like data rows to the planner.
func trimTrailingEmptyRows(rows [][]string) [][]string {
	last := len(rows)
	for last > 0 && rowIsEmpty(rows[last-1]) {
		last--
	}
	return rows[:last]
}

func rowIsEmpty(row []string) bool {
	for _, value := range row {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}
