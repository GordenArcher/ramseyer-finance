package handlers

import (
	"fmt"
	"net/http"
	"ramseyer-finance/internal/db"
)

// MonthlyData carries the template variables for the monthly breakdown report page. It
// includes the year label, the list of available years for the picker dropdown, the twelve
// month rows with income/expense/surplus figures, the year-to-date running totals, and the
// chart data for the monthly income-vs-expense bar chart with a surplus trend line. The
// YTD totals accumulate month by month within buildMonthlyData so they reflect the sum of
// all months from January through December of the selected year.
type MonthlyData struct {
	Active     string
	Year       string
	Years      []int
	Months     []MonthRow
	Chart      ChartData
	YTDIncome  float64
	YTDExpense float64
	YTDSurplus float64
}

// MonthRow represents a single month's financial summary in the monthly report table. It
// pairs the full month name with the aggregated income, expense, and computed surplus
// (income minus expense) for that month. A month with no transactions will show zeros
// across all three columns rather than being omitted, keeping the table structurally
// consistent for all twelve months.
type MonthRow struct {
	Month   string
	Income  float64
	Expense float64
	Surplus float64
}

// MonthlyReport serves the monthly breakdown report for a given year. It parses the "year"
// query parameter, builds the twelve-month dataset with chart data and YTD totals, and
// renders the template. The report provides a month-by-month view of income, expenditure,
// and surplus, complementing the annual summary with a finer-grained time breakdown.
func MonthlyReport(w http.ResponseWriter, r *http.Request) {
	year, err := parseReportYear(r.URL.Query().Get("year"))
	if err != nil {
		badRequest(w, err.Error())
		return
	}

	data, err := buildMonthlyData(year)
	if err != nil {
		serverError(w, err)
		return
	}
	RenderTemplate(w, "monthly", data)
}

// QuarterlyData carries the template variables for the quarterly breakdown report page.
// It includes the year label, the list of available years, the four quarter rows with
// aggregated income/expense/surplus figures, the chart data for the quarterly bar chart,
// and the full-year totals. Each quarter aggregates three months of data from the same
// underlying monthly rollup used by the monthly report, ensuring consistency between the
// two views.
type QuarterlyData struct {
	Active       string
	Year         string
	Years        []int
	Quarters     []QuarterRow
	Chart        ChartData
	TotalIncome  float64
	TotalExpense float64
	TotalSurplus float64
}

// QuarterRow represents a single quarter's financial summary in the quarterly report table.
// It pairs the quarter label (e.g., "Q1 (Jan-Mar)") with the aggregated income, expense,
// and surplus for that three-month period. The quarter values are derived by summing the
// monthly totals from the shared loadMonthlyTotals function, not by querying the database
// with a separate quarterly grouping.
type QuarterRow struct {
	Quarter string
	Income  float64
	Expense float64
	Surplus float64
}

// QuarterlyReport serves the quarterly breakdown report for a given year. It parses the
// "year" query parameter, builds the four-quarter dataset by aggregating monthly totals,
// and renders the template. Because quarterly data is derived from monthly data rather than
// queried independently, the quarterly and monthly reports will always be mathematically
// consistent—a quarter's totals will exactly equal the sum of its three constituent months.
func QuarterlyReport(w http.ResponseWriter, r *http.Request) {
	year, err := parseReportYear(r.URL.Query().Get("year"))
	if err != nil {
		badRequest(w, err.Error())
		return
	}

	data, err := buildQuarterlyData(year)
	if err != nil {
		serverError(w, err)
		return
	}
	RenderTemplate(w, "quarterly", data)
}

// AnnualData carries the template variables for the annual income statement report page.
// It includes the current and prior year labels, the list of available years, any reporting
// warnings (e.g., unclassified transactions), the detailed line-item breakdown for both
// income and expenditure categories with comparative prior-year amounts and budget variance,
// the chart data for the year-over-year comparison, and the summary totals. Each line item
// includes a note_ref for cross-referencing with the notes report, the current and prior
// year amounts, the budgeted amount, and the computed variance (actual minus budget).
type AnnualData struct {
	Active            string
	Year              string
	PriorYear         string
	Years             []int
	Warnings          []string
	IncomeLines       []LineRow
	ExpenseLines      []LineRow
	Chart             ChartData
	TotalIncome       float64
	TotalPriorIncome  float64
	TotalExpense      float64
	TotalPriorExpense float64
	Surplus           float64
	PriorSurplus      float64
	OpeningFund       float64
	PriorAdjustment   float64
	AdjustedFund      float64
	ClosingFund       float64
	FundConfigured    bool
}

// LineRow represents a single row in the annual income statement's line-item table. It
// carries the note_ref for cross-referencing, the category display name, the current-year
// amount, the prior-year amount (for comparison), the budgeted amount (for variance
// calculation), and the computed variance (actual minus budget). The IsTotal and IsBold
// flags are used to style the summary row at the bottom of each section differently from
// the detail rows.
type LineRow struct {
	Note        string
	Name        string
	Amount      float64
	PriorAmount float64
	Budget      float64
	Variance    float64
	IsTotal     bool
	IsBold      bool
}

// AnnualReport serves the annual income statement for a given year. It parses the "year"
// query parameter, builds the full comparative dataset including income and expenditure
// line items with prior-year and budget comparisons, and renders the template. The report
// provides a complete picture of the year's financial performance, with each top-level
// category broken out into its own row and summary totals at the bottom.
func AnnualReport(w http.ResponseWriter, r *http.Request) {
	year, err := parseReportYear(r.URL.Query().Get("year"))
	if err != nil {
		badRequest(w, err.Error())
		return
	}

	data, err := buildAnnualData(year)
	if err != nil {
		serverError(w, err)
		return
	}
	RenderTemplate(w, "annual", data)
}

// buildMonthlyData assembles the complete MonthlyData struct for a given year. It loads
// the monthly totals from the database via loadMonthlyTotals (which returns a map of month
// numbers to income/expense aggregates), iterates through the twelve calendar months in
// order, populates the table rows and chart series, and computes the running year-to-date
// totals. Months with no transactions appear with zero values rather than being omitted,
// keeping the twelve-month grid intact.
func buildMonthlyData(year int) (MonthlyData, error) {
	// I build monthly and quarterly views from the same monthly totals map so there is one source
	// of truth for period rollups. Quarterly should be a recomposition of months, not separate math.
	years, err := reportYears(year)
	if err != nil {
		return MonthlyData{}, fmt.Errorf("load report years: %w", err)
	}

	data := MonthlyData{
		Active: "monthly",
		Year:   fmt.Sprintf("%d", year),
		Years:  years,
	}

	monthlyTotals, err := loadMonthlyTotals(year)
	if err != nil {
		return MonthlyData{}, fmt.Errorf("load monthly totals: %w", err)
	}

	monthNames := []string{
		"January", "February", "March", "April", "May", "June",
		"July", "August", "September", "October", "November", "December",
	}
	// Build the table rows and chart series simultaneously by iterating months 1–12 in
	// order. The YTD totals accumulate across the loop so December's YTD equals the
	// full-year total. The chart uses three-letter month abbreviations as X-axis labels.
	var incomeSeries []float64
	var expenseSeries []float64
	var surplusSeries []float64
	for monthIndex, name := range monthNames {
		totals := monthlyTotals[monthIndex+1]
		data.YTDIncome += totals.Income
		data.YTDExpense += totals.Expense
		incomeSeries = append(incomeSeries, totals.Income)
		expenseSeries = append(expenseSeries, totals.Expense)
		surplusSeries = append(surplusSeries, totals.Income-totals.Expense)
		data.Chart.Labels = append(data.Chart.Labels, name[:3])
		data.Months = append(data.Months, MonthRow{
			Month:   name,
			Income:  totals.Income,
			Expense: totals.Expense,
			Surplus: totals.Income - totals.Expense,
		})
	}
	data.YTDSurplus = data.YTDIncome - data.YTDExpense
	// Chart colours match the dashboard: gold bars for income, green bars for expense,
	// and a red line for surplus/deficit trend.
	data.Chart.Datasets = []ChartDataset{
		{
			Label:     "Income",
			Type:      "bar",
			Color:     "#ba7a11",
			SoftColor: "rgba(186, 122, 17, 0.18)",
			Values:    incomeSeries,
		},
		{
			Label:     "Expense",
			Type:      "bar",
			Color:     "#184e48",
			SoftColor: "rgba(24, 78, 72, 0.16)",
			Values:    expenseSeries,
		},
		{
			Label:  "Surplus",
			Type:   "line",
			Color:  "#b44432",
			Values: surplusSeries,
		},
	}

	return data, nil
}

// buildQuarterlyData assembles the complete QuarterlyData struct for a given year. It
// loads the same monthly totals map used by the monthly report, then aggregates months
// into the four standard calendar quarters (Q1: Jan–Mar, Q2: Apr–Jun, Q3: Jul–Sep, Q4:
// Oct–Dec). Because the quarterly data is derived from the monthly rollup rather than
// queried directly from the database, the quarterly totals will always exactly equal the
// sum of the corresponding monthly report rows—a property that would be harder to
// guarantee with independent quarterly queries.
func buildQuarterlyData(year int) (QuarterlyData, error) {
	// I derive quarter values from monthly totals instead of querying quarters directly because the
	// monthly rollup is already the primitive this app uses everywhere else.
	years, err := reportYears(year)
	if err != nil {
		return QuarterlyData{}, fmt.Errorf("load report years: %w", err)
	}

	data := QuarterlyData{
		Active: "quarterly",
		Year:   fmt.Sprintf("%d", year),
		Years:  years,
	}

	monthlyTotals, err := loadMonthlyTotals(year)
	if err != nil {
		return QuarterlyData{}, fmt.Errorf("load monthly totals: %w", err)
	}

	// Define the four calendar quarters with their month numbers. The label includes the
	// month range for clarity (e.g., "Q1 (Jan-Mar)") while the chart uses just the
	// abbreviated quarter label (e.g., "Q1").
	quarters := []struct {
		Label  string
		Months []int
	}{
		{Label: "Q1 (Jan-Mar)", Months: []int{1, 2, 3}},
		{Label: "Q2 (Apr-Jun)", Months: []int{4, 5, 6}},
		{Label: "Q3 (Jul-Sep)", Months: []int{7, 8, 9}},
		{Label: "Q4 (Oct-Dec)", Months: []int{10, 11, 12}},
	}

	var incomeSeries []float64
	var expenseSeries []float64
	var surplusSeries []float64
	for _, quarter := range quarters {
		row := QuarterRow{Quarter: quarter.Label}
		for _, month := range quarter.Months {
			row.Income += monthlyTotals[month].Income
			row.Expense += monthlyTotals[month].Expense
		}
		row.Surplus = row.Income - row.Expense
		data.Quarters = append(data.Quarters, row)
		data.Chart.Labels = append(data.Chart.Labels, quarter.Label[:2])
		incomeSeries = append(incomeSeries, row.Income)
		expenseSeries = append(expenseSeries, row.Expense)
		surplusSeries = append(surplusSeries, row.Surplus)
		// The full-year totals are the sum of all four quarters, which is equivalent to
		// the sum of all twelve months. Computing them here avoids a separate query.
		data.TotalIncome += row.Income
		data.TotalExpense += row.Expense
	}
	data.TotalSurplus = data.TotalIncome - data.TotalExpense
	// Chart uses the same colour scheme as the monthly report for visual consistency
	// across the reporting section.
	data.Chart.Datasets = []ChartDataset{
		{
			Label:     "Income",
			Type:      "bar",
			Color:     "#ba7a11",
			SoftColor: "rgba(186, 122, 17, 0.18)",
			Values:    incomeSeries,
		},
		{
			Label:     "Expense",
			Type:      "bar",
			Color:     "#184e48",
			SoftColor: "rgba(24, 78, 72, 0.16)",
			Values:    expenseSeries,
		},
		{
			Label:  "Surplus",
			Type:   "line",
			Color:  "#b44432",
			Values: surplusSeries,
		},
	}

	return data, nil
}

// buildAnnualData assembles the complete AnnualData struct for a given year. It loads the
// reporting warnings (data quality issues that may affect the statement), then builds the
// income and expenditure line items separately via loadAnnualLines. Each section ends with
// a bold total row. The chart provides a year-over-year comparison of total income, total
// expenditure, and net surplus, with the prior year shown in grey and the current year in
// gold. The budget variance for each line is computed as (actual - budget), so a positive
// variance means income exceeded budget (favourable) or expenditure exceeded budget
// (unfavourable), depending on the section.
func buildAnnualData(year int) (AnnualData, error) {
	// I keep the annual statement comparative by construction because that is the fastest way to
	// expose drift against the prior year without leaving the page.
	years, err := reportYears(year)
	if err != nil {
		return AnnualData{}, fmt.Errorf("load report years: %w", err)
	}

	data := AnnualData{
		Active:    "annual",
		Year:      fmt.Sprintf("%d", year),
		PriorYear: fmt.Sprintf("%d", year-1),
		Years:     years,
	}
	// Reporting warnings highlight potential data quality issues (e.g., transactions
	// with missing categories, or opening balances that haven't been configured for
	// the year). These are shown at the top of the report so the user can address
	// them before relying on the figures.
	data.Warnings, err = loadReportingWarnings(year)
	if err != nil {
		return AnnualData{}, fmt.Errorf("load annual warnings: %w", err)
	}

	data.IncomeLines, data.TotalIncome, data.TotalPriorIncome, err = loadAnnualLines(year, "income")
	if err != nil {
		return AnnualData{}, fmt.Errorf("load annual income lines: %w", err)
	}
	// Append a bold total row at the end of the income section. The IsTotal and IsBold
	// flags tell the template to apply distinct styling (e.g., a border-top, bold text,
	// and a different background) to the summary row.
	data.IncomeLines = append(data.IncomeLines, LineRow{
		Name:        "TOTAL INCOME",
		Amount:      data.TotalIncome,
		PriorAmount: data.TotalPriorIncome,
		IsTotal:     true,
		IsBold:      true,
	})

	data.ExpenseLines, data.TotalExpense, data.TotalPriorExpense, err = loadAnnualLines(year, "expenditure")
	if err != nil {
		return AnnualData{}, fmt.Errorf("load annual expenditure lines: %w", err)
	}
	assetSchedule, err := buildFixedAssetData(year)
	if err != nil {
		return AnnualData{}, fmt.Errorf("build current fixed-asset schedule: %w", err)
	}
	priorAssetSchedule, err := buildFixedAssetData(year - 1)
	if err != nil {
		return AnnualData{}, fmt.Errorf("build prior fixed-asset schedule: %w", err)
	}
	for index := range data.ExpenseLines {
		if data.ExpenseLines[index].Name != "Depreciation & Amortization Expenses" {
			continue
		}
		// Explicit expense postings remain authoritative when present. Otherwise the app
		// supplies the charge calculated by Note 21 so the performance statement and asset
		// schedule cannot silently omit depreciation.
		if data.ExpenseLines[index].Amount == 0 {
			data.ExpenseLines[index].Amount = assetSchedule.TotalCharge
			data.TotalExpense += assetSchedule.TotalCharge
		}
		if data.ExpenseLines[index].PriorAmount == 0 {
			data.ExpenseLines[index].PriorAmount = priorAssetSchedule.TotalCharge
			data.TotalPriorExpense += priorAssetSchedule.TotalCharge
		}
		data.ExpenseLines[index].Variance = data.ExpenseLines[index].Amount - data.ExpenseLines[index].Budget
		break
	}

	// The PCG statement presents harvest proceeds net of harvest expenses. I keep the
	// underlying postings in their natural income and expenditure types for monthly and
	// operational reporting, then perform the presentation reclassification here. Removing
	// the same amount from income and expenditure preserves the year's surplus while making
	// both the statement line and Note 5 agree with the workbook.
	var harvestExpense LineRow
	filteredExpenses := data.ExpenseLines[:0]
	for _, line := range data.ExpenseLines {
		if line.Name == "Harvest Expenses" {
			harvestExpense = line
			continue
		}
		filteredExpenses = append(filteredExpenses, line)
	}
	data.ExpenseLines = filteredExpenses
	if harvestExpense.Name != "" {
		for index := range data.IncomeLines {
			if data.IncomeLines[index].Name != "Harvest Proceeds" {
				continue
			}
			data.IncomeLines[index].Name = "Harvest Proceeds (Net)"
			data.IncomeLines[index].Amount -= harvestExpense.Amount
			data.IncomeLines[index].PriorAmount -= harvestExpense.PriorAmount
			data.IncomeLines[index].Budget -= harvestExpense.Budget
			data.IncomeLines[index].Variance = data.IncomeLines[index].Amount - data.IncomeLines[index].Budget
			break
		}
		data.TotalIncome -= harvestExpense.Amount
		data.TotalPriorIncome -= harvestExpense.PriorAmount
		data.TotalExpense -= harvestExpense.Amount
		data.TotalPriorExpense -= harvestExpense.PriorAmount
	}
	for index := range data.IncomeLines {
		if data.IncomeLines[index].IsTotal {
			data.IncomeLines[index].Amount = data.TotalIncome
			data.IncomeLines[index].PriorAmount = data.TotalPriorIncome
			break
		}
	}
	data.ExpenseLines = append(data.ExpenseLines, LineRow{
		Name:        "TOTAL EXPENDITURE",
		Amount:      data.TotalExpense,
		PriorAmount: data.TotalPriorExpense,
		IsTotal:     true,
		IsBold:      true,
	})

	data.Surplus = data.TotalIncome - data.TotalExpense
	data.PriorSurplus = data.TotalPriorIncome - data.TotalPriorExpense
	fund, err := loadFundRollforward(year)
	if err != nil {
		return AnnualData{}, fmt.Errorf("load accumulated fund roll-forward: %w", err)
	}
	data.FundConfigured = fund.Exists
	data.OpeningFund = fund.OpeningBalance
	data.PriorAdjustment = fund.PriorYearAdjustment
	data.AdjustedFund = data.OpeningFund + data.PriorAdjustment
	data.ClosingFund = data.AdjustedFund + data.Surplus
	// The annual comparison chart uses paired bars for the current and prior year,
	// grouped by the three key metrics: income, expenditure, and surplus. The prior
	// year uses a muted grey to push visual focus toward the current year's results.
	data.Chart = ChartData{
		Labels: []string{"Income", "Expenditure", "Surplus"},
		Datasets: []ChartDataset{
			{
				Label:     data.PriorYear,
				Type:      "bar",
				Color:     "#5a6475",
				SoftColor: "rgba(90, 100, 117, 0.14)",
				Values: []float64{
					data.TotalPriorIncome,
					data.TotalPriorExpense,
					data.PriorSurplus,
				},
			},
			{
				Label:     data.Year,
				Type:      "bar",
				Color:     "#ba7a11",
				SoftColor: "rgba(186, 122, 17, 0.18)",
				Values: []float64{
					data.TotalIncome,
					data.TotalExpense,
					data.Surplus,
				},
			},
		},
	}
	return data, nil
}

// amountTotals is a small internal struct that pairs income and expense totals for a single
// time period (a month). It is used as the value type in the map returned by
// loadMonthlyTotals, where each month number maps to its income and expense aggregates.
// This is more efficient than returning two separate maps or a slice of pairs, and it
// keeps the income and expense for each month bundled together logically.
type amountTotals struct {
	Income  float64
	Expense float64
}

// loadMonthlyTotals queries the database for monthly income and expense totals within a
// given year. It uses a single grouped query that extracts the month number from each
// transaction's date via strftime and sums amounts by type and month. The result is a map
// keyed by month number (1–12), where each entry contains the income and expense totals.
// Months with no transactions are simply absent from the map (not populated with zeros),
// and callers are expected to handle missing keys by treating them as zero—which is Go's
// default behaviour for map lookups on float64 values. The query uses the year's date
// boundaries for the WHERE clause so SQLite can leverage the date index.
func loadMonthlyTotals(year int) (map[int]amountTotals, error) {
	// I use date ranges instead of `strftime` filters so SQLite can still benefit from the date
	// index. The caller gets a month-number map that can be reshaped however it wants.
	startDate, endDate := yearBounds(year)
	rows, err := db.DB.Query(`
		SELECT CAST(strftime('%m', date) AS INTEGER) AS month_number, type, COALESCE(SUM(amount), 0)
		FROM transactions
		WHERE date >= ? AND date < ? AND type IN ('income', 'expenditure')
		GROUP BY month_number, type
	`, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	totals := map[int]amountTotals{}
	for rows.Next() {
		var (
			monthNumber     int
			transactionType string
			total           float64
		)
		if err := rows.Scan(&monthNumber, &transactionType, &total); err != nil {
			return nil, err
		}
		// Since the query groups by both month_number and type, each month can produce
		// up to two rows (one for income, one for expenditure). We read the existing
		// entry from the map, update the relevant field, and write it back.
		current := totals[monthNumber]
		switch transactionType {
		case "income":
			current.Income = total
		case "expenditure":
			current.Expense = total
		}
		totals[monthNumber] = current
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return totals, nil
}

// loadAnnualLines retrieves the line items for one section (income or expenditure) of the
// annual income statement. It queries the database for all top-level categories of the
// given type, aggregating transaction amounts for both the current year and the prior year
// using conditional SUM(CASE WHEN ...) expressions, and joins against the budgets table to
// fetch the budgeted amount for the current year. Children categories are included in the
// parent's total via a self-join on the categories table (leaf.id = top.id OR leaf.parent_id
// = top.id), matching the same aggregation pattern used by the balance sheet. The function
// returns the ordered line items, the total of all current-year amounts, and the total of
// all prior-year amounts.
func loadAnnualLines(year int, categoryType string) ([]LineRow, float64, float64, error) {
	// I load current year, prior year, and budget in one grouped pass so the annual rows stay
	// aligned by top-level category instead of being stitched together from separate queries.
	startDate, endDate := yearBounds(year)
	priorStartDate, priorEndDate := yearBounds(year - 1)
	rows, err := db.DB.Query(`
		SELECT
			top.name,
			top.note_ref,
			COALESCE(SUM(CASE WHEN t.date >= ? AND t.date < ? THEN t.amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN t.date >= ? AND t.date < ? THEN t.amount ELSE 0 END), 0),
			COALESCE(b.amount, 0)
		FROM categories top
		LEFT JOIN categories leaf
			ON leaf.type = top.type
			AND (leaf.id = top.id OR leaf.parent_id = top.id)
		LEFT JOIN transactions t
			ON t.category_id = leaf.id
			AND t.type = top.type
			AND t.date >= ?
			AND t.date < ?
		LEFT JOIN budgets b
			ON b.year = ?
			AND b.category_id = top.id
		WHERE top.type = ? AND top.parent_id = 0
		GROUP BY top.id, top.name, top.note_ref, b.amount
		ORDER BY CAST(NULLIF(top.note_ref, '') AS INTEGER), top.id
	`, startDate, endDate, priorStartDate, priorEndDate, priorStartDate, endDate, year, categoryType)
	if err != nil {
		return nil, 0, 0, err
	}
	defer rows.Close()

	var (
		lines      []LineRow
		total      float64
		priorTotal float64
	)
	for rows.Next() {
		var line LineRow
		if err := rows.Scan(&line.Name, &line.Note, &line.Amount, &line.PriorAmount, &line.Budget); err != nil {
			return nil, 0, 0, err
		}
		// Variance is computed as actual minus budget. For income categories, a positive
		// variance is favourable (earned more than budgeted); for expenditure categories,
		// a positive variance is unfavourable (spent more than budgeted). The template
		// can apply colour coding based on the section type.
		line.Variance = line.Amount - line.Budget
		lines = append(lines, line)
		total += line.Amount
		priorTotal += line.PriorAmount
	}
	if err := rows.Err(); err != nil {
		return nil, 0, 0, err
	}

	return lines, total, priorTotal, nil
}
