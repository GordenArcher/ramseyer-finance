package plan

import (
	"fmt"
	"strings"
	"time"

	"ramseyer-finance/internal/imports/workbook"
)

// summaryLayout is the dated, week-by-week grid. The header carries one date per week and
// the first column carries the collection label. This is the only layout in the workbook
// that can fill months before the weekly sheets start.
type summaryLayout struct {
	HeaderRow int
	LabelCol  int
	Dates     map[int]time.Time
	Columns   []int
}

// detectSummaryLayout finds a row with several dated columns. Three dates is enough to
// distinguish a real schedule from a fund ledger that happens to carry one maturity date.
func detectSummaryLayout(rows [][]string) (summaryLayout, bool) {
	for rowIndex := 0; rowIndex < len(rows) && rowIndex < weeklyHeaderScanRows; rowIndex++ {
		dates := make(map[int]time.Time)
		columns := make([]int, 0)

		for colIndex := 1; colIndex < len(rows[rowIndex]); colIndex++ {
			date, ok := workbook.ParseDate(workbook.Cell(rows[rowIndex], colIndex))
			if !ok {
				continue
			}
			dates[colIndex] = date
			columns = append(columns, colIndex)
		}

		if len(columns) >= summaryDateThreshold {
			return summaryLayout{
				HeaderRow: rowIndex,
				LabelCol:  0,
				Dates:     dates,
				Columns:   columns,
			}, true
		}
	}
	return summaryLayout{}, false
}

// parseSummarySheet reads the dated summary but imports only the columns that have no
// dedicated weekly sheet. That rule fills January and February and any missing Sunday
// without ever posting the same week twice.
func parseSummarySheet(name string, rows [][]string, layout summaryLayout, weeklyDates map[string]bool, sheetOrder int) ([]Row, SheetReport, []string) {
	report := SheetReport{Name: name, Kind: KindSummary}
	warnings := make([]string, 0)
	sourceRows := make([]Row, 0)
	selectedColumns := make([]int, 0)
	skippedWeeks := 0

	for _, colIndex := range layout.Columns {
		date := layout.Dates[colIndex]
		if weeklyDates[date.Format(DateLayout)] {
			skippedWeeks++
			continue
		}
		selectedColumns = append(selectedColumns, colIndex)
	}

	if len(selectedColumns) == 0 {
		report.Note = "Every dated week already has its own weekly sheet, so the summary added nothing."
		return sourceRows, report, warnings
	}

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
			for _, colIndex := range selectedColumns {
				if amount, ok := parseAmount(workbook.Cell(row, colIndex)); ok {
					expectedTotal += amount
				}
			}
			break
		}
		if reconciliationLabels[strings.ToUpper(label)] {
			continue
		}

		key := Key(label)
		category, source := DefaultCategory(key)
		service := summaryService(label)

		for _, colIndex := range selectedColumns {
			amount, ok := parseAmount(workbook.Cell(row, colIndex))
			if !ok || amount == 0 {
				continue
			}
			if amount < 0 {
				warnings = append(warnings, fmt.Sprintf(
					"%s row %d: a negative amount for %q was skipped because income cannot be negative.",
					name, line, label))
				continue
			}

			sourceRows = append(sourceRows, Row{
				Date:       layout.Dates[colIndex],
				Label:      label,
				Key:        key,
				Category:   category,
				Source:     source,
				Amount:     amount,
				Service:    service,
				Sheet:      name,
				Line:       line,
				SheetOrder: sheetOrder,
			})
			report.Total += amount
		}
	}

	report.Rows = len(sourceRows)
	if skippedWeeks > 0 {
		report.Note = fmt.Sprintf(
			"Imported %d of %d dated weeks. %d weeks already have a weekly sheet and were left to that sheet.",
			len(selectedColumns), len(layout.Columns), skippedWeeks)
	} else {
		report.Note = fmt.Sprintf("Imported all %d dated weeks.", len(selectedColumns))
	}

	if hasTotal {
		report.Expected = expectedTotal
		report.Reconciles = absDifference(report.Total, expectedTotal) <= amountTolerance
		if !report.Reconciles {
			warnings = append(warnings, fmt.Sprintf(
				"%s: the summary totals for the imported weeks add up to %.2f but the rows add up to %.2f.",
				name, expectedTotal, report.Total))
		}
	} else {
		report.Note += " No TOTAL row was found, so the summary total could not be checked."
	}

	return sourceRows, report, warnings
}

// summaryService keeps the separate second offering distinct in the description. The
// summary has no per-service columns, so this label is the only hint the import has.
func summaryService(label string) string {
	if strings.Contains(strings.ToUpper(label), "SECOND") {
		return "2nd"
	}
	return ""
}

func absDifference(left, right float64) float64 {
	if left > right {
		return left - right
	}
	return right - left
}
