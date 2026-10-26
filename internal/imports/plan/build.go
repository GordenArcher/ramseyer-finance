package plan

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"ramseyer-finance/internal/imports/workbook"
)

// pledgeLayout is the name and amount list used for person-level pledges. It is not a
// collection sheet, so it is reported and left alone.
type pledgeLayout struct {
	HeaderRow int
	NameCol   int
	AmountCol int
}

// Options carries the metadata the plan needs from the upload. Year zero means infer the
// year from the workbook's dated summary.
type Options struct {
	FileName string
	FileHash string
	Year     int
}

// Build reads every sheet and returns the rows that could be imported. Sheets that are
// derived, person-level, or unrecognised are still reported so the operator can see that
// they were seen and deliberately skipped.
func Build(data []byte, opts Options) (*Plan, error) {
	document, err := workbook.Open(data)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = document.Close()
	}()

	names := document.SheetNames()
	if len(names) == 0 {
		return nil, fmt.Errorf("workbook has no sheets")
	}

	// Reading each sheet once keeps the layout scans and the parse passes on the same
	// values. The workbook is small enough that the extra memory is cheaper than reopening
	// the file and risking a different view of the same cells.
	sheetRows := make(map[string][][]string, len(names))
	for _, name := range names {
		rows, readErr := document.Rows(name)
		if readErr != nil {
			return nil, readErr
		}
		sheetRows[name] = rows
	}

	warnings := make([]string, 0)

	// The summary is the only sheet that anchors the workbook to a year, so it is located
	// before the weekly sheet names are turned into dates.
	summaryName := ""
	for _, name := range names {
		if _, ok := detectSummaryLayout(sheetRows[name]); ok {
			summaryName = name
			break
		}
	}

	year := opts.Year
	inferredYear, hasInferredYear := 0, false
	if summaryName != "" {
		inferredYear, hasInferredYear = InferYear(sheetRows[summaryName])
	}
	if hasInferredYear {
		// The dated summary is the only trustworthy year anchor. If the operator typed a
		// different year, the sheet dates win and the report explains why.
		if year != 0 && year != inferredYear {
			warnings = append(warnings, fmt.Sprintf(
				"The dated summary says %d, so the year override %d was ignored.", inferredYear, year))
		}
		year = inferredYear
	} else if year == 0 {
		year = time.Now().Year()
		warnings = append(warnings, fmt.Sprintf(
			"No dated summary sheet was found, so the workbook was read as %d. Set the year on the upload form if that is wrong.", year))
	}
	if !validWorkbookYear(year) {
		return nil, fmt.Errorf("year %d is outside the supported range", year)
	}

	type weeklySheet struct {
		name   string
		rowSet [][]string
		layout weeklyLayout
		date   time.Time
	}
	weeklySheets := make(map[string]weeklySheet)
	weeklyDates := make(map[string]bool)
	weeklySkips := make(map[string]string)

	for _, name := range names {
		layout, isWeekly := detectWeeklyLayout(sheetRows[name])
		if !isWeekly {
			continue
		}

		date, matched, dateErr := ParseSheetDate(name, year)
		if dateErr != nil {
			weeklySkips[name] = dateErr.Error()
			warnings = append(warnings, fmt.Sprintf("%s: %s", name, dateErr.Error()))
			continue
		}
		if !matched {
			reason := "the sheet name does not contain a date, so the rows cannot be posted"
			weeklySkips[name] = reason
			warnings = append(warnings, fmt.Sprintf("%s: %s", name, reason))
			continue
		}

		weeklySheets[name] = weeklySheet{name: name, rowSet: sheetRows[name], layout: layout, date: date}
		weeklyDates[date.Format(DateLayout)] = true
	}

	built := &Plan{
		FileName: opts.FileName,
		FileHash: opts.FileHash,
		Year:     year,
		Rows:     make([]Row, 0),
		Labels:   make([]Label, 0),
		Sheets:   make([]SheetReport, 0, len(names)),
		Warnings: warnings,
	}

	for sheetOrder, name := range names {
		rows := sheetRows[name]

		if weekly, ok := weeklySheets[name]; ok {
			sheetRowsOut, report, sheetWarnings := parseWeeklySheet(weekly.name, weekly.date, weekly.rowSet, weekly.layout, sheetOrder)
			built.Rows = append(built.Rows, sheetRowsOut...)
			built.Sheets = append(built.Sheets, report)
			built.Warnings = append(built.Warnings, sheetWarnings...)
			continue
		}

		if reason, skipped := weeklySkips[name]; skipped {
			built.Sheets = append(built.Sheets, SheetReport{
				Name: name,
				Kind: KindWeekly,
				Note: "Skipped: " + reason + ".",
			})
			continue
		}

		if layout, ok := detectSummaryLayout(rows); ok {
			sheetRowsOut, report, sheetWarnings := parseSummarySheet(name, rows, layout, weeklyDates, sheetOrder)
			built.Rows = append(built.Rows, sheetRowsOut...)
			built.Sheets = append(built.Sheets, report)
			built.Warnings = append(built.Warnings, sheetWarnings...)
			continue
		}

		if layout, ok := detectPledgeLayout(rows); ok {
			built.Sheets = append(built.Sheets, buildPledgeReport(name, rows, layout))
			continue
		}

		if isFundLedger(rows) {
			built.Sheets = append(built.Sheets, SheetReport{
				Name: name,
				Kind: KindFundLedger,
				Note: "Fund ledger. Not imported because the weekly collection rows already record the same money.",
			})
			continue
		}

		if isUndatedSummary(rows) {
			built.Sheets = append(built.Sheets, SheetReport{
				Name: name,
				Kind: KindUndatedSummary,
				Note: "Undated summary. Not imported because it repeats a weekly sheet and would double count.",
			})
			continue
		}

		built.Sheets = append(built.Sheets, SheetReport{
			Name: name,
			Kind: KindUnrecognised,
			Note: "Sheet layout was not recognised, so it was not imported.",
		})
	}

	// Chronological order makes the preview read like the register the operator already
	// knows. The sheet order then line number keep rows from the same sheet together.
	sort.SliceStable(built.Rows, func(left, right int) bool {
		if !built.Rows[left].Date.Equal(built.Rows[right].Date) {
			return built.Rows[left].Date.Before(built.Rows[right].Date)
		}
		if built.Rows[left].SheetOrder != built.Rows[right].SheetOrder {
			return built.Rows[left].SheetOrder < built.Rows[right].SheetOrder
		}
		return built.Rows[left].Line < built.Rows[right].Line
	})

	built.Labels = summariseLabels(built.Rows)

	if len(built.Rows) == 0 {
		built.Warnings = append(built.Warnings, "No dated collection rows were found, so there is nothing to import.")
	}

	return built, nil
}

// detectPledgeLayout finds the NAME and AMOUNT header pair used by the Cement pledge list.
// Requiring NAME before AMOUNT keeps a weekly sheet's single AMOUNT layout from matching.
func detectPledgeLayout(rows [][]string) (pledgeLayout, bool) {
	for rowIndex := 0; rowIndex < len(rows) && rowIndex < weeklyHeaderScanRows; rowIndex++ {
		nameCol, amountCol := -1, -1
		for colIndex := 0; colIndex < len(rows[rowIndex]); colIndex++ {
			switch strings.ToUpper(workbook.Cell(rows[rowIndex], colIndex)) {
			case "NAME":
				nameCol = colIndex
			case "AMOUNT":
				amountCol = colIndex
			}
		}
		if nameCol >= 0 && amountCol > nameCol {
			return pledgeLayout{HeaderRow: rowIndex, NameCol: nameCol, AmountCol: amountCol}, true
		}
	}
	return pledgeLayout{}, false
}

// buildPledgeReport totals a person-level pledge list without turning any name into a
// transaction. The list belongs to a fund decision that the importer cannot make alone.
func buildPledgeReport(name string, rows [][]string, layout pledgeLayout) SheetReport {
	report := SheetReport{
		Name: name,
		Kind: KindPledgeList,
		Note: "Person-level pledge list. Not imported because it is a fund record, not a dated collection.",
	}
	for rowIndex := layout.HeaderRow + 1; rowIndex < len(rows); rowIndex++ {
		if workbook.Cell(rows[rowIndex], layout.NameCol) == "" {
			continue
		}
		amount, ok := parseAmount(workbook.Cell(rows[rowIndex], layout.AmountCol))
		if !ok || amount == 0 {
			continue
		}
		report.Rows++
		report.Total += amount
	}
	return report
}

// isFundLedger looks for a dated entry in the first two columns. The scholarship sheet has
// a maturity date and a balance, then dated contributions, which is enough shape to keep
// it out of the collection import.
func isFundLedger(rows [][]string) bool {
	datedEntries := 0
	for _, row := range rows {
		if _, ok := workbook.ParseDate(workbook.Cell(row, 0)); !ok {
			continue
		}
		if _, amountOK := parseAmount(workbook.Cell(row, 1)); amountOK {
			datedEntries++
		}
	}
	return datedEntries > 0
}

// undatedSummaryMinLabels is the number of label and amount pairs that makes a sheet look
// like a monthly list rather than a stray note. Weekly sheets are detected first, so this
// only decides between two non-dated shapes.
const undatedSummaryMinLabels = 2

// isUndatedSummary recognises the monthly aggregate that repeats a weekly sheet. It has
// labels in column B, amounts in column C, and a TOTAL row, but no dates at all.
func isUndatedSummary(rows [][]string) bool {
	labelRows := 0
	totalRows := 0
	for _, row := range rows {
		label := CleanLabel(workbook.Cell(row, 1))
		if label == "" {
			continue
		}
		if isTotalLabel(label) {
			totalRows++
			continue
		}
		if _, ok := parseAmount(workbook.Cell(row, 2)); ok {
			labelRows++
		}
	}
	return totalRows > 0 && labelRows >= undatedSummaryMinLabels
}

// summariseLabels groups the final rows by mapping key. The preview decides one category
// per label, and the commit applies that choice to every row with the same key.
func summariseLabels(rows []Row) []Label {
	labels := make([]Label, 0)
	indexByKey := make(map[string]int)

	for _, row := range rows {
		index, exists := indexByKey[row.Key]
		if !exists {
			indexByKey[row.Key] = len(labels)
			index = len(labels)
			labels = append(labels, Label{
				Key:      row.Key,
				Display:  row.Label,
				Category: row.Category,
				Source:   row.Source,
			})
		}
		labels[index].Rows++
		labels[index].Total += row.Amount
	}
	return labels
}
