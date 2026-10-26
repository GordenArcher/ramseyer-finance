package plan

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"ramseyer-finance/internal/imports/workbook"
)

const (
	// weeklyHeaderScanRows covers the blank title rows that push the real header down in
	// some sheets. The 08MAR sheet has its AMOUNT header on row four.
	weeklyHeaderScanRows = 5
	// weeklyHeaderScanCols stops before the second, printed copy of the collection list
	// that sits to the right of the working columns in most weekly sheets.
	weeklyHeaderScanCols = 6
	amountTolerance      = 0.01
	centsFactor          = 100
)

// weeklyLayout records where a weekly sheet keeps its labels and amounts. Services is true
// for the common 1ST, 2ND, TOTAL layout and false for the single AMOUNT layout used by the
// first sheet in the source workbook.
type weeklyLayout struct {
	HeaderRow   int
	LabelCol    int
	FirstCol    int
	SecondCol   int
	TotalCol    int
	HasServices bool
}

// reconciliationLabels are lines below or beside the collection list that reconcile cash,
// bank deposits, and spending. They are not income rows, and a sheet without a TOTAL row
// would otherwise let them slip into the import.
var reconciliationLabels = map[string]bool{
	"ANALYSIS":              true,
	"BANK DEPOSITS":         true,
	"CAL BANK":              true,
	"CASH":                  true,
	"CAT BOADI -SEND OFF":   true,
	"CHEQ":                  true,
	"CHEQUES":               true,
	"CHEQUES/BANK TRANSFER": true,
	"COINS":                 true,
	"DIFF":                  true,
	"EXPENSES FOR THE WEEK": true,
	"FUEL":                  true,
	"GA RURAL BANK":         true,
	"GMF REMMITANCE":        true,
	"GMF SUNDAY":            true,
	"KENTE FOR MAMA":        true,
	"MOMO":                  true,
	"PAYMENTS FOR THE WEEK": true,
	"SUB TOTAL":             true,
	"TOTAL DEPOSITS":        true,
}

// detectWeeklyLayout recognises the two weekly layouts in the workbook. It deliberately
// refuses the pledge list even though that sheet also has an AMOUNT header, because the
// NAME header marks a person-by-person list rather than a service collection.
func detectWeeklyLayout(rows [][]string) (weeklyLayout, bool) {
	for rowIndex := 0; rowIndex < len(rows) && rowIndex < weeklyHeaderScanRows; rowIndex++ {
		row := rows[rowIndex]
		firstCol, secondCol, totalCol, amountCol := -1, -1, -1, -1
		hasNameHeader := false

		for colIndex := 0; colIndex < len(row) && colIndex < weeklyHeaderScanCols; colIndex++ {
			switch strings.ToUpper(workbook.Cell(row, colIndex)) {
			case "1ST":
				firstCol = colIndex
			case "2ND":
				secondCol = colIndex
			case "TOTAL":
				totalCol = colIndex
			case "AMOUNT":
				amountCol = colIndex
			case "NAME":
				hasNameHeader = true
			}
		}

		if !hasNameHeader && firstCol > 0 && totalCol > firstCol {
			return weeklyLayout{
				HeaderRow:   rowIndex,
				LabelCol:    firstCol - 1,
				FirstCol:    firstCol,
				SecondCol:   secondCol,
				TotalCol:    totalCol,
				HasServices: true,
			}, true
		}
		if !hasNameHeader && amountCol > 0 {
			return weeklyLayout{
				HeaderRow: rowIndex,
				LabelCol:  amountCol - 1,
				TotalCol:  amountCol,
			}, true
		}
	}
	return weeklyLayout{}, false
}

// parseWeeklySheet turns one dated collection sheet into rows. Zero amounts are dropped
// because they add nothing to the register, and a label that repeats within a sheet is
// skipped with a warning rather than imported twice.
func parseWeeklySheet(name string, date time.Time, rows [][]string, layout weeklyLayout, sheetOrder int) ([]Row, SheetReport, []string) {
	report := SheetReport{Name: name, Kind: KindWeekly}
	warnings := make([]string, 0)
	sourceRows := make([]Row, 0)
	seenLabels := make(map[string]int)
	hasTotal := false
	expectedTotal := 0.0

	for rowIndex := layout.HeaderRow + 1; rowIndex < len(rows); rowIndex++ {
		row := rows[rowIndex]
		line := rowIndex + 1
		label := CleanLabel(workbook.Cell(row, layout.LabelCol))

		if label == "" || isNumericLabel(label) {
			continue
		}
		if isTotalLabel(label) {
			hasTotal = true
			expectedTotal = weeklyTotal(row, layout)
			break
		}
		if reconciliationLabels[strings.ToUpper(label)] {
			continue
		}

		// Two different labels can map to the same account, such as Sunday Service Offering
		// and an additional Second Offering. Deduplicating on the workbook label keeps those
		// separate while still catching a line that was accidentally repeated.
		labelKey := strings.ToUpper(label)
		if previousLine, exists := seenLabels[labelKey]; exists {
			warnings = append(warnings, fmt.Sprintf(
				"%s row %d: duplicate label %q was skipped; the first occurrence is on row %d.",
				name, line, label, previousLine))
			continue
		}
		seenLabels[labelKey] = line

		firstAmount, firstOK := parseAmount(workbook.Cell(row, layout.FirstCol))
		secondAmount, secondOK := parseAmount(workbook.Cell(row, layout.SecondCol))
		totalAmount, totalOK := parseAmount(workbook.Cell(row, layout.TotalCol))

		if layout.HasServices && firstOK && secondOK && totalOK && firstAmount > 0 && secondAmount > 0 &&
			math.Abs(firstAmount+secondAmount-totalAmount) > amountTolerance {
			warnings = append(warnings, fmt.Sprintf(
				"%s row %d: first and second service amounts do not add up to the row total.", name, line))
		}

		key := Key(label)
		category, source := DefaultCategory(key)
		entries := weeklyAmounts(label, layout, firstAmount, secondAmount, totalAmount, firstOK, secondOK, totalOK)

		for _, entry := range entries {
			sourceRows = append(sourceRows, Row{
				Date:       date,
				Label:      label,
				Key:        key,
				Category:   category,
				Source:     source,
				Amount:     entry.amount,
				Service:    entry.service,
				Sheet:      name,
				Line:       line,
				SheetOrder: sheetOrder,
			})
			report.Total += entry.amount
		}
	}

	report.Rows = len(sourceRows)
	if hasTotal {
		report.Expected = expectedTotal
		report.Reconciles = math.Abs(report.Total-expectedTotal) <= amountTolerance
		if !report.Reconciles {
			warnings = append(warnings, fmt.Sprintf(
				"%s: the sheet total is %.2f but the imported rows add up to %.2f.",
				name, expectedTotal, report.Total))
		}
	} else {
		report.Note = "No total row was found, so the sheet total could not be checked."
	}
	return sourceRows, report, warnings
}

// serviceAmount is one amount and the service that produced it. A split row returns two
// of these, one per service, so the register can keep the distinction without a schema
// change.
type serviceAmount struct {
	service string
	amount  float64
}

// weeklyAmounts picks the amounts that should become transactions. Two filled service
// columns mean the row really was split across services. A single amount is one
// transaction unless the label itself names the service, because most rows (Tithe, VTO,
// Friday offering) are entered in the first column without being a first-service item.
func weeklyAmounts(label string, layout weeklyLayout, first, second, total float64, firstOK, secondOK, totalOK bool) []serviceAmount {
	entries := make([]serviceAmount, 0, 2)

	if layout.HasServices && firstOK && secondOK && first > 0 && second > 0 {
		return []serviceAmount{
			{"1st", first},
			{"2nd", second},
		}
	}

	if layout.HasServices {
		amount := first
		hasAmount := firstOK && first > 0
		if !hasAmount && secondOK && second > 0 {
			amount = second
			hasAmount = true
		}
		if hasAmount {
			return []serviceAmount{{serviceFromLabel(label), amount}}
		}
	}

	if totalOK && total > 0 {
		return []serviceAmount{{serviceFromLabel(label), total}}
	}
	return entries
}

// serviceFromLabel reads the service from a label that names it. The columns take over
// when both services have an amount, so this is only used for single-amount rows.
func serviceFromLabel(label string) string {
	upper := strings.ToUpper(label)
	switch {
	case strings.Contains(upper, "SECOND"):
		return "2nd"
	case strings.Contains(upper, "FIRST"):
		return "1st"
	}
	return ""
}

// weeklyTotal reads the workbook's own total from the TOTAL row. When the split total cell
// is blank, as it is in one sheet, the two service columns are the fallback.
func weeklyTotal(row []string, layout weeklyLayout) float64 {
	total, ok := parseAmount(workbook.Cell(row, layout.TotalCol))
	if ok && total != 0 {
		return total
	}
	if layout.HasServices {
		first, firstOK := parseAmount(workbook.Cell(row, layout.FirstCol))
		second, secondOK := parseAmount(workbook.Cell(row, layout.SecondCol))
		if firstOK && secondOK {
			return first + second
		}
	}
	return total
}

// parseAmount reads a money cell. Currency symbols, thousands separators, and the common
// accounting parenthesis notation for negatives are handled here so the planner never
// carries display formatting into the database.
func parseAmount(value string) (float64, bool) {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return 0, false
	}

	negative := false
	if strings.HasPrefix(cleaned, "(") && strings.HasSuffix(cleaned, ")") {
		negative = true
		cleaned = strings.TrimSuffix(strings.TrimPrefix(cleaned, "("), ")")
	}

	replacer := strings.NewReplacer(",", "", " ", "", "GH\u20b5", "", "GHS", "", "GHC", "", "\u20b5", "")
	cleaned = replacer.Replace(cleaned)
	parsed, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return 0, false
	}
	if negative {
		parsed = -parsed
	}
	return math.Round(parsed*centsFactor) / centsFactor, true
}

// isTotalLabel marks the end of the collection list. Everything below it is reconciliation
// or spending, not another collection.
func isTotalLabel(label string) bool {
	normalised := strings.ToUpper(strings.TrimSpace(label))
	return normalised == "TOTAL" || normalised == "GRAND TOTAL"
}

// isNumericLabel catches the stray 0 rows that appear between collection lines in some
// sheets. They carry no name, so they cannot become a meaningful transaction.
func isNumericLabel(label string) bool {
	_, ok := parseAmount(label)
	return ok
}
