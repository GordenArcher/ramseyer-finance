package plan

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ramseyer-finance/internal/imports/workbook"
)

// A workbook year is accepted only inside this range. The guard catches a misread serial
// number or a sheet name from an unrelated document before it reaches the database.
const (
	minWorkbookYear = 2000
	maxWorkbookYear = 2100
)

// summaryDateThreshold is the number of dated columns that makes a row look like a
// week-by-week summary header. Five dated cells is far beyond the two or three amounts a
// weekly collection row carries, so a data row cannot be mistaken for a schedule.
const summaryDateThreshold = 5

var monthNumbers = map[string]time.Month{
	"JAN": time.January, "JANUARY": time.January,
	"FEB": time.February, "FEBRUARY": time.February,
	"MAR": time.March, "MARCH": time.March,
	"APR": time.April, "APRIL": time.April,
	"MAY": time.May,
	"JUN": time.June, "JUNE": time.June,
	"JUL": time.July, "JULY": time.July,
	"AUG": time.August, "AUGUST": time.August,
	"SEP": time.September, "SEPT": time.September, "SEPTEMBER": time.September,
	"OCT": time.October, "OCTOBER": time.October,
	"NOV": time.November, "NOVEMBER": time.November,
	"DEC": time.December, "DECEMBER": time.December,
}

// sheetDatePattern matches the weekly sheet names in the source workbook: 08MAR, 29 MAR,
// 3APRIL, 06 SEPT and similar. The optional separator keeps a stray space or dot harmless.
var sheetDatePattern = regexp.MustCompile(`^(\d{1,2})[\s._-]*([A-Za-z]+)$`)

// ParseSheetDate extracts a calendar date from a weekly sheet name. The workbook does not
// carry a year on each sheet, so the caller supplies the inferred year. The boolean is
// false when the name is simply not date-shaped, which is normal for summary or fund
// sheets. A date-shaped name with a bad month or day returns an error so the report can
// explain why the sheet was skipped instead of guessing.
func ParseSheetDate(name string, year int) (time.Time, bool, error) {
	cleaned := strings.ToUpper(strings.TrimSpace(name))
	match := sheetDatePattern.FindStringSubmatch(cleaned)
	if match == nil {
		return time.Time{}, false, nil
	}

	day, err := strconv.Atoi(match[1])
	if err != nil {
		return time.Time{}, true, fmt.Errorf("read day from sheet name %q: %w", name, err)
	}
	month, exists := monthNumbers[match[2]]
	if !exists {
		return time.Time{}, true, fmt.Errorf("unknown month %q in sheet name %q", match[2], name)
	}

	date := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	// time.Date normalises an impossible day into the next month, so a round trip is the
	// cheapest way to reject names such as 31FEB without a month-length table.
	if date.Day() != day || date.Month() != month {
		return time.Time{}, true, fmt.Errorf("invalid date %q in sheet name %q", cleaned, name)
	}
	return date, true, nil
}

// InferYear looks for the dated header of a summary sheet and returns its year. Weekly
// sheet names intentionally omit the year, so the summary is the only part of the workbook
// that can anchor the book to a calendar year without asking the operator.
func InferYear(rows [][]string) (int, bool) {
	for _, row := range rows {
		if len(row) < summaryDateThreshold {
			continue
		}

		dates := make([]time.Time, 0, len(row))
		for index := 0; index < len(row); index++ {
			if date, ok := workbook.ParseDate(workbook.Cell(row, index)); ok {
				dates = append(dates, date)
			}
		}
		if len(dates) >= summaryDateThreshold {
			return dates[0].Year(), true
		}
	}
	return 0, false
}

// validWorkbookYear keeps an inferred or operator-supplied year inside the range the
// reports and the SQLite date comparisons expect.
func validWorkbookYear(year int) bool {
	return year >= minWorkbookYear && year <= maxWorkbookYear
}
