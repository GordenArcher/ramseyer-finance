package handlers

import (
	"fmt"
	"net/http"
	"ramseyer-finance/internal/db"
	"strings"
)

// BalanceData carries all the template variables for the balance sheet report page. It
// includes the current and prior year labels, the list of available report years for the
// year picker dropdown, and the complete set of line items broken into non-current assets,
// current assets, and liabilities—each with current and prior amounts for side-by-side
// comparison. Totals and derived figures (accumulated fund, income surplus, total equity)
// are carried at the top level for the summary section and the comparison chart.
type BalanceData struct {
	Active                string
	Year                  string
	PriorYear             string
	Years                 []int
	NonCurrentAssets      []BalanceLine
	CurrentAssets         []BalanceLine
	TotalAssets           float64
	PriorTotalAssets      float64
	Liabilities           []BalanceLine
	Chart                 ChartData
	TotalLiabilities      float64
	PriorTotalLiabilities float64
	AccumulatedFund       float64
	PriorAccumulatedFund  float64
	IncomeSurplus         float64
	PriorIncomeSurplus    float64
	TotalEquity           float64
	PriorTotalEquity      float64
}

// BalanceLine represents a single row in the balance sheet report. It pairs a display name
// with a current-year amount and a prior-year amount, enabling the template to render
// comparative columns without needing to understand how the underlying data was computed.
// This struct is used for non-current assets, current assets, and liabilities alike.
type BalanceLine struct {
	Name        string
	Amount      float64
	PriorAmount float64
}

// balanceSnapshot is an internal accumulator used during balance sheet computation. It
// groups all the category-level totals needed to populate the final BalanceData struct.
// The maps use category display names as keys so the build functions can assemble line
// items in a human-readable order without coupling to database IDs. The package-level
// buildBalanceData function then extracts and orders the map entries into the template
// struct.
type balanceSnapshot struct {
	NonCurrentAssets map[string]float64
	CurrentAssets    map[string]float64
	Liabilities      map[string]float64
	TotalAssets      float64
	TotalLiabilities float64
	AccumulatedFund  float64
	IncomeSurplus    float64
	TotalEquity      float64
}

// BalanceSheet serves the balance sheet report page for a given year. It parses the "year"
// query parameter, builds the full comparative dataset (current year vs. prior year), and
// renders the template. If no year is specified, the current calendar year is used as the
// default. The page includes a year picker, a categorized asset/liability listing, and a
// bar chart comparing the key balance sheet figures across the two periods.
func BalanceSheet(w http.ResponseWriter, r *http.Request) {
	year, err := parseReportYear(r.URL.Query().Get("year"))
	if err != nil {
		badRequest(w, err.Error())
		return
	}

	data, err := buildBalanceData(year)
	if err != nil {
		serverError(w, err)
		return
	}
	RenderTemplate(w, "balance-sheet", data)
}

// buildBalanceData assembles the complete BalanceData struct for a given report year. It
// loads the list of available years for the picker, builds independent snapshots for the
// current and prior years using the same computation rules (ensuring the comparative
// columns are directly comparable), populates each balance sheet section in display order,
// and constructs the chart data series for the visual comparison. The function treats the
// balance sheet as a year-end snapshot assembled from category group totals plus carried
// opening cash positions—it is a different view of the same transaction data, not a
// separate accounting ledger.
func buildBalanceData(year int) (BalanceData, error) {
	// I build current and prior snapshots through the same helper so the comparative columns are
	// produced by identical rules instead of two drifting implementations.
	years, err := reportYears(year)
	if err != nil {
		return BalanceData{}, fmt.Errorf("load report years: %w", err)
	}

	data := BalanceData{
		Active:    "balance-sheet",
		Year:      fmt.Sprintf("%d", year),
		PriorYear: fmt.Sprintf("%d", year-1),
		Years:     years,
	}

	currentSnapshot, err := buildBalanceSnapshot(year)
	if err != nil {
		return BalanceData{}, err
	}
	priorSnapshot, err := buildBalanceSnapshot(year - 1)
	if err != nil {
		return BalanceData{}, err
	}

	// The display order of line items is fixed here rather than being driven by the
	// database. This ensures a consistent presentation regardless of category insertion
	// order and allows the statement to follow a conventional accounting layout.
	nonCurrentNames := []string{
		"Property, Plant & Equipment",
		"GAP Presbytery",
		"Investment (Credit Union)",
	}
	for _, name := range nonCurrentNames {
		data.NonCurrentAssets = append(data.NonCurrentAssets, BalanceLine{
			Name:        name,
			Amount:      currentSnapshot.NonCurrentAssets[name],
			PriorAmount: priorSnapshot.NonCurrentAssets[name],
		})
	}

	// Current assets are listed in a specific order that separates trade receivables from
	// liquid accounts. Each line is sourced from the current asset map in the snapshot,
	// defaulting to zero for categories that have no transactions in the period.
	currentAssetLines := []struct {
		Name        string
		AccountType string
	}{
		{Name: "Receivables (Debtors)"},
		{Name: "Bank", AccountType: "bank"},
		{Name: "Cash", AccountType: "cash"},
		{Name: "Momo", AccountType: "momo"},
	}
	for _, line := range currentAssetLines {
		data.CurrentAssets = append(data.CurrentAssets, BalanceLine{
			Name:        line.Name,
			Amount:      currentSnapshot.CurrentAssets[line.Name],
			PriorAmount: priorSnapshot.CurrentAssets[line.Name],
		})
	}

	liabilityNames := []string{"Payables (Creditors)", "District Assessment Owing"}
	for _, name := range liabilityNames {
		data.Liabilities = append(data.Liabilities, BalanceLine{
			Name:        name,
			Amount:      currentSnapshot.Liabilities[name],
			PriorAmount: priorSnapshot.Liabilities[name],
		})
	}

	// Copy the snapshot totals into the template data. These aggregate values drive the
	// summary section at the bottom of the balance sheet and the chart on the page.
	data.TotalAssets = currentSnapshot.TotalAssets
	data.PriorTotalAssets = priorSnapshot.TotalAssets
	data.TotalLiabilities = currentSnapshot.TotalLiabilities
	data.PriorTotalLiabilities = priorSnapshot.TotalLiabilities
	data.IncomeSurplus = currentSnapshot.IncomeSurplus
	data.PriorIncomeSurplus = priorSnapshot.IncomeSurplus
	data.TotalEquity = currentSnapshot.TotalEquity
	data.PriorTotalEquity = priorSnapshot.TotalEquity
	data.AccumulatedFund = currentSnapshot.AccumulatedFund
	data.PriorAccumulatedFund = priorSnapshot.AccumulatedFund

	// Build a grouped bar chart comparing the five key balance sheet figures across the
	// current and prior years. The prior year uses a muted grey colour while the current
	// year uses the application's brand green, making the comparison visually immediate.
	data.Chart = ChartData{
		Labels: []string{"Assets", "Liabilities", "Equity", "Accumulated Fund", "Income Surplus"},
		Datasets: []ChartDataset{
			{
				Label:     data.PriorYear,
				Type:      "bar",
				Color:     "#5a6475",
				SoftColor: "rgba(90, 100, 117, 0.14)",
				Values: []float64{
					data.PriorTotalAssets,
					data.PriorTotalLiabilities,
					data.PriorTotalEquity,
					data.PriorAccumulatedFund,
					data.PriorIncomeSurplus,
				},
			},
			{
				Label:     data.Year,
				Type:      "bar",
				Color:     "#184e48",
				SoftColor: "rgba(24, 78, 72, 0.16)",
				Values: []float64{
					data.TotalAssets,
					data.TotalLiabilities,
					data.TotalEquity,
					data.AccumulatedFund,
					data.IncomeSurplus,
				},
			},
		},
	}

	return data, nil
}

// buildBalanceSnapshot computes a complete balance sheet position for a single year. It
// determines the year's date boundaries, loads non-current asset totals as cumulative
// balances up to year-end, loads the opening balances for the three liquid accounts (which
// represent cash positions carried forward from prior periods), computes current asset
// totals by adding in-year transaction movements to those opening balances, loads liability
// totals, and then derives the equity components: total equity as assets minus liabilities,
// accumulated fund as total equity minus the current year's income surplus, and income
// surplus as the difference between income and expenditure transactions within the year.
func buildBalanceSnapshot(year int) (balanceSnapshot, error) {
	// I treat the balance sheet as a year-end snapshot assembled from category groups plus carried
	// opening cash positions. It is a different cut of the same data, not a separate ledger.
	yearStart, yearEnd := yearBounds(year)

	// Non-current assets are cumulative: all transactions from the beginning of time up to
	// the end of the reporting year. There is no opening balance to add because these
	// categories represent long-term holdings, not flow accounts.
	nonCurrentNames := []string{
		"Property, Plant & Equipment",
		"GAP Presbytery",
		"Investment (Credit Union)",
	}
	nonCurrentTotals, err := loadTopLevelSums("asset", nonCurrentNames, yearEnd)
	if err != nil {
		return balanceSnapshot{}, fmt.Errorf("load non-current asset totals for %d: %w", year, err)
	}

	// Opening balances are stored in the settings table and represent the cash position at
	// the start of the year. They are added to the in-year transaction movement to produce
	// the year-end position for bank, cash, and momo accounts.
	openingBalances, err := loadOpeningBalanceMap(year)
	if err != nil {
		return balanceSnapshot{}, fmt.Errorf("load opening balances for %d: %w", year, err)
	}

	// I split liquid assets from the other current assets because bank, cash, and momo are the
	// only accounts that combine opening balances with in-year movement.
	// Receivables are treated as a pure cumulative balance (like non-current assets) because
	// they represent amounts owed regardless of when the underlying transaction occurred.
	currentAssetTotals, err := loadDirectCategorySums("asset", []string{"Receivables (Debtors)"}, "", yearEnd)
	if err != nil {
		return balanceSnapshot{}, fmt.Errorf("load current asset totals for %d: %w", year, err)
	}
	// Liquid accounts sum only transactions within the current year, because the opening
	// balance already captures the position from prior periods.
	cashAssetTotals, err := loadDirectCategorySums("asset", []string{"Bank", "Cash", "Momo"}, yearStart, yearEnd)
	if err != nil {
		return balanceSnapshot{}, fmt.Errorf("load liquid asset totals for %d: %w", year, err)
	}

	currentAssets := map[string]float64{
		"Receivables (Debtors)": currentAssetTotals["Receivables (Debtors)"],
		"Bank":                  cashAssetTotals["Bank"] + openingBalances["bank"],
		"Cash":                  cashAssetTotals["Cash"] + openingBalances["cash"],
		"Momo":                  cashAssetTotals["Momo"] + openingBalances["momo"],
	}

	// Liabilities are cumulative up to year-end, following the same pattern as non-current
	// assets. They represent obligations that persist across periods.
	liabilityNames := []string{"Payables (Creditors)", "District Assessment Owing"}
	liabilityTotals, err := loadDirectCategorySums("liability", liabilityNames, "", yearEnd)
	if err != nil {
		return balanceSnapshot{}, fmt.Errorf("load liabilities for %d: %w", year, err)
	}

	snapshot := balanceSnapshot{
		NonCurrentAssets: nonCurrentTotals,
		CurrentAssets:    currentAssets,
		Liabilities:      liabilityTotals,
	}

	// Sum all asset and liability categories into their respective totals. The iteration
	// order doesn't matter here because we're only interested in the aggregated values.
	for _, amount := range snapshot.NonCurrentAssets {
		snapshot.TotalAssets += amount
	}
	for _, amount := range snapshot.CurrentAssets {
		snapshot.TotalAssets += amount
	}
	for _, amount := range snapshot.Liabilities {
		snapshot.TotalLiabilities += amount
	}

	// I derive accumulated fund as residual equity after removing the current-year surplus so the
	// statement does not double-count this year's operating result.
	// Income surplus is the net of all income and expenditure transactions within the
	// reporting year. It feeds into the equity calculation: accumulated fund represents
	// equity built up in prior periods, while income surplus is the current period's
	// contribution.
	var yearlyIncome, yearlyExpense float64
	if err := loadIncomeExpenseTotals(yearStart, yearEnd, &yearlyIncome, &yearlyExpense); err != nil {
		return balanceSnapshot{}, fmt.Errorf("load income surplus for %d: %w", year, err)
	}
	snapshot.IncomeSurplus = yearlyIncome - yearlyExpense
	snapshot.TotalEquity = snapshot.TotalAssets - snapshot.TotalLiabilities
	snapshot.AccumulatedFund = snapshot.TotalEquity - snapshot.IncomeSurplus

	return snapshot, nil
}

// loadTopLevelSums computes the total transaction amount for each named top-level category,
// aggregating both the parent category itself and all of its child subcategories. This is
// used for balance sheet line items that represent a category family (e.g., "Property,
// Plant & Equipment" includes its own direct postings plus those of "Land", "Furniture &
// Equipment", "Building", etc.). The endDate parameter bounds the query to transactions
// strictly before that date, enabling cumulative balance calculations up to a year-end.
// The function builds a parameterised SQL query with placeholders for the category names
// to prevent SQL injection while still supporting a dynamic list of names.
func loadTopLevelSums(categoryType string, names []string, endDate string) (map[string]float64, error) {
	// I roll top-level categories through both the parent and its children because postings can
	// land at either level in this app. The statement should reflect the whole category family.
	if len(names) == 0 {
		return map[string]float64{}, nil
	}

	// Build a parameterised IN clause with one placeholder per category name. We also
	// include the endDate and categoryType as the first two parameters.
	placeholders := make([]string, len(names))
	args := make([]interface{}, 0, len(names)+2)
	args = append(args, endDate, categoryType)
	for index, name := range names {
		placeholders[index] = "?"
		args = append(args, name)
	}

	// The query self-joins the categories table to match each top-level category (parent_id=0)
	// with itself (leaf.id = top.id) and all of its children (leaf.parent_id = top.id). This
	// UNION-like pattern via a LEFT JOIN on the same table avoids needing a recursive CTE
	// and handles the simple two-level hierarchy used in this application.
	query := fmt.Sprintf(`
		SELECT top.name, COALESCE(SUM(t.amount), 0)
		FROM categories top
		LEFT JOIN categories leaf
			ON leaf.type = top.type
			AND (leaf.id = top.id OR leaf.parent_id = top.id)
		LEFT JOIN transactions t
			ON t.category_id = leaf.id
			AND t.type = top.type
			AND t.date < ?
		WHERE top.type = ? AND top.parent_id = 0 AND top.name IN (%s)
		GROUP BY top.id, top.name
		ORDER BY top.id
	`, strings.Join(placeholders, ","))

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	totals := map[string]float64{}
	for rows.Next() {
		var name string
		var amount float64
		if err := rows.Scan(&name, &amount); err != nil {
			return nil, err
		}
		totals[name] = amount
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return totals, nil
}

// loadDirectCategorySums computes the total transaction amount for each named category,
// but unlike loadTopLevelSums, it only sums transactions directly assigned to the named
// category—it does not aggregate child subcategories. This is used for liquid asset
// accounts (Bank, Cash, Momo) and liabilities where the statement line should reflect
// only the direct postings to that category without absorbing activity from related
// subcategories. The startDate and endDate parameters are optional: an empty startDate
// means "from the beginning of time," and an empty endDate means "up to the present."
// When both are provided, the function sums transactions within the date range; when
// only endDate is provided, it sums all transactions up to that date (cumulative balance).
func loadDirectCategorySums(categoryType string, names []string, startDate, endDate string) (map[string]float64, error) {
	// I keep this helper for lines that should not absorb child activity implicitly. For liquid
	// accounts and liabilities, the statement line should reflect the direct category itself.
	if len(names) == 0 {
		return map[string]float64{}, nil
	}

	// Build the query dynamically because the date constraints are optional. We append
	// conditions and their corresponding parameter values only when the date strings are
	// non-empty, keeping the query as simple as possible for each call site.
	args := make([]interface{}, 0, len(names)+3)
	queryBuilder := strings.Builder{}
	queryBuilder.WriteString(`
		SELECT c.name, COALESCE(SUM(t.amount), 0)
		FROM categories c
		LEFT JOIN transactions t
			ON t.category_id = c.id
			AND t.type = c.type
	`)
	if startDate != "" {
		queryBuilder.WriteString(" AND t.date >= ?")
		args = append(args, startDate)
	}
	if endDate != "" {
		queryBuilder.WriteString(" AND t.date < ?")
		args = append(args, endDate)
	}
	queryBuilder.WriteString(`
		WHERE c.type = ? AND c.name IN (
	`)
	args = append(args, categoryType)

	placeholders := make([]string, len(names))
	for index, name := range names {
		placeholders[index] = "?"
		args = append(args, name)
	}
	queryBuilder.WriteString(strings.Join(placeholders, ","))
	queryBuilder.WriteString(`
		)
		GROUP BY c.id, c.name
		ORDER BY c.id
	`)

	rows, err := db.DB.Query(queryBuilder.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	totals := map[string]float64{}
	for rows.Next() {
		var name string
		var amount float64
		if err := rows.Scan(&name, &amount); err != nil {
			return nil, err
		}
		totals[name] = amount
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return totals, nil
}
