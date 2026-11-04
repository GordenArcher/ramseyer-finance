package bulk

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// dateLayouts covers the textual shapes an operator's workbook actually uses. The
// day-first layouts come first because the church books are written in local day/month
// order, so 04/01/2026 has to read as 4 January. Excel serial values are handled separately
// because a date formatted cell has no reliable text form once raw values are requested.
var dateLayouts = []string{
	"2006-01-02",
	"02/01/2006",
	"2/1/2006",
	"02-01-2006",
	"2-1-2006",
	"02/01/06",
	"2/1/06",
	"2-Jan-2006",
	"02-Jan-2006",
	"Jan 2, 2006",
	"January 2, 2006",
	"2006/01/02",
}

// amountDecoration is everything the operator's currency cells wrap around the number.
// Stripping it centrally keeps the parsers from each growing their own replacement list.
var amountDecoration = strings.NewReplacer(
	",", "",
	"GH¢", "",
	"GH₵", "",
	"₵", "",
	"GHS", "",
	"GHC", "",
	" ", "",
)

// DateValue reads one date column from a row and reports whether it was usable.
func DateValue(row []string, column int) (string, bool) {
	if column < 0 {
		return "", false
	}
	return ParseDate(Cell(row, column))
}

// ParseDate accepts Excel serial numbers and the textual date shapes an operator types.
// The returned value is the canonical storage format used everywhere else in the app, so an
// imported row cannot end up with a date the register cannot parse.
func ParseDate(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", false
	}

	// Excel stores a real date cell as days since 1899-12-30. Converting the serial is
	// unambiguous, which matters because a formatted "01/04/2026" could read as either
	// 1 April or 4 January depending on locale.
	if serial, err := strconv.ParseFloat(value, 64); err == nil && serial > 0 {
		if parsed, err := excelize.ExcelDateToTime(serial, false); err == nil {
			return parsed.Format("2006-01-02"), true
		}
	}

	cleaned := strings.TrimSuffix(value, " 00:00:00")
	for _, layout := range dateLayouts {
		if parsed, err := time.Parse(layout, cleaned); err == nil {
			return parsed.Format("2006-01-02"), true
		}
	}
	return "", false
}

// AmountValue reads one amount column from a row and reports whether it was usable.
func AmountValue(row []string, column int) (float64, bool) {
	if column < 0 {
		return 0, false
	}
	amount, err := ParseAmount(Cell(row, column))
	if err != nil {
		return 0, false
	}
	return amount, true
}

// ParseAmount strips the currency decoration found in real workbooks before parsing.
// Bracket negatives are honoured because accounting exports use them for reversals.
func ParseAmount(raw string) (float64, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, fmt.Errorf("empty amount")
	}

	negative := false
	if strings.HasPrefix(value, "(") && strings.HasSuffix(value, ")") {
		negative = true
		value = strings.TrimSuffix(strings.TrimPrefix(value, "("), ")")
	}

	value = amountDecoration.Replace(value)
	if value == "" {
		return 0, fmt.Errorf("empty amount")
	}

	amount, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, err
	}
	if negative {
		amount = -amount
	}
	return amount, nil
}
