package finance

import (
	"fmt"
	"net/http"
	"ramseyer-finance/internal/db"
	"ramseyer-finance/internal/handlers/viewmodels"
	"sort"
	"strconv"
	"strings"
)

// BalanceData carries all the template variables for the balance sheet report page. It
// includes the current and prior year labels, the list of available report years for the
// year picker dropdown, and the complete set of line items broken into non-current assets,
// current assets, and liabilities—each with current and prior amounts for side-by-side
// comparison. Totals and derived figures (accumulated fund, income surplus, total equity)
// are carried at the top level for the summary section and the comparison chart.
type BalanceData struct {
	Active                 string
	Year                   string
	PriorYear              string
	Years                  []int
	NonCurrentAssets       []BalanceLine
	CurrentAssets          []BalanceLine
	TotalAssets            float64
	PriorTotalAssets       float64
	LongTermLiabilities    []BalanceLine
	CurrentLiabilities     []BalanceLine
	Chart                  viewmodels.ChartData
	TotalLiabilities       float64
	PriorTotalLiabilities  float64
	AccumulatedFund        float64
	PriorAccumulatedFund   float64
	IncomeSurplus          float64
	PriorIncomeSurplus     float64
	TotalEquity            float64
	PriorTotalEquity       float64
	PriorYearAdjustment    float64
	PriorPriorAdjustment   float64
	BalanceDifference      float64
	PriorBalanceDifference float64
	FundConfigured         bool
	PriorFundConfigured    bool
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
	NonCurrentAssets    map[string]float64
	CurrentAssets       map[string]float64
	LongTermLiabilities map[string]float64
	CurrentLiabilities  map[string]float64
	TotalAssets         float64
	TotalLiabilities    float64
	AccumulatedFund     float64
	PriorYearAdjustment float64
	IncomeSurplus       float64
	TotalEquity         float64
	BalanceDifference   float64
	FundConfigured      bool
}

type balanceCategoryDef struct {
	Name string
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
	return buildTrialBalancePositionData(year)
}

// buildTrialBalancePositionData assembles the statement from the same calculated Trial
// Balance shown on screen. It does not read the retired saved-balance tables, so changing a
// dated entry immediately changes the position statement and its comparative column.
func buildTrialBalancePositionData(year int) (BalanceData, error) {
	years, err := reportYears(year)
	if err != nil {
		return BalanceData{}, err
	}
	data := BalanceData{Active: "balance-sheet", Year: strconv.Itoa(year), PriorYear: strconv.Itoa(year - 1), Years: years}
	type key struct{ Type, Note, Account string }
	type pair struct{ current, prior float64 }
	balances := map[key]pair{}
	var currentIncome, currentExpense, priorIncome, priorExpense float64
	var currentOpeningEquity, priorOpeningEquity float64

	for _, period := range []struct {
		year    int
		current bool
	}{{year, true}, {year - 1, false}} {
		trialBalance, err := buildTrialBalanceData(period.year)
		if err != nil {
			return BalanceData{}, err
		}
		for _, line := range trialBalance.Lines {
			item := key{Type: line.AccountType, Note: line.Note, Account: line.Account}
			amount := line.Amount
			switch item.Type {
			case "asset", "liability":
				// Financial-position statement rows are note totals. Rows without a note are
				// custom standalone accounts and therefore keep their year-specific caption.
				if item.Note != "" {
					item.Account = noteTitle(item.Note, item.Account)
				}
				value := balances[item]
				if period.current {
					value.current += amount
				} else {
					value.prior += amount
				}
				balances[item] = value
			case "income":
				if period.current {
					currentIncome += amount
				} else {
					priorIncome += amount
				}
			case "expenditure":
				if period.current {
					currentExpense += amount
				} else {
					priorExpense += amount
				}
			case "equity":
				if period.current {
					currentOpeningEquity += amount
				} else {
					priorOpeningEquity += amount
				}
			}
		}
	}

	keys := make([]key, 0, len(balances))
	for item := range balances {
		keys = append(keys, item)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		left, _ := strconv.Atoi(keys[i].Note)
		right, _ := strconv.Atoi(keys[j].Note)
		if left != right {
			return left < right
		}
		return keys[i].Account < keys[j].Account
	})
	for _, item := range keys {
		value := balances[item]
		line := BalanceLine{Name: item.Account, Amount: value.current, PriorAmount: value.prior}
		note, _ := strconv.Atoi(item.Note)
		if item.Type == "asset" {
			if note >= 21 && note <= 23 {
				data.NonCurrentAssets = append(data.NonCurrentAssets, line)
			} else {
				data.CurrentAssets = append(data.CurrentAssets, line)
			}
			data.TotalAssets += value.current
			data.PriorTotalAssets += value.prior
		} else {
			if note == 27 {
				data.LongTermLiabilities = append(data.LongTermLiabilities, line)
			} else {
				data.CurrentLiabilities = append(data.CurrentLiabilities, line)
			}
			data.TotalLiabilities += value.current
			data.PriorTotalLiabilities += value.prior
		}
	}
	data.IncomeSurplus = currentIncome - currentExpense
	data.PriorIncomeSurplus = priorIncome - priorExpense
	data.AccumulatedFund = currentOpeningEquity
	data.PriorAccumulatedFund = priorOpeningEquity
	data.TotalEquity = data.AccumulatedFund + data.IncomeSurplus
	data.PriorTotalEquity = data.PriorAccumulatedFund + data.PriorIncomeSurplus
	data.BalanceDifference = data.TotalAssets - data.TotalLiabilities - data.TotalEquity
	data.PriorBalanceDifference = data.PriorTotalAssets - data.PriorTotalLiabilities - data.PriorTotalEquity
	data.FundConfigured = currentOpeningEquity != 0
	data.PriorFundConfigured = priorOpeningEquity != 0
	data.Chart = viewmodels.ChartData{
		Labels: []string{"Assets", "Liabilities", "Equity", "Accumulated Fund", "Income Surplus"},
		Datasets: []viewmodels.ChartDataset{
			{Label: data.PriorYear, Type: "bar", Color: "#5a6475", SoftColor: "rgba(90, 100, 117, 0.14)", Values: []float64{data.PriorTotalAssets, data.PriorTotalLiabilities, data.PriorTotalEquity, data.PriorAccumulatedFund, data.PriorIncomeSurplus}},
			{Label: data.Year, Type: "bar", Color: "#184e48", SoftColor: "rgba(24, 78, 72, 0.16)", Values: []float64{data.TotalAssets, data.TotalLiabilities, data.TotalEquity, data.AccumulatedFund, data.IncomeSurplus}},
		},
	}
	return data, nil
}

// buildLegacyBalanceData is retained only as a migration reference for databases created by
// older application builds. The active statement always uses buildTrialBalancePositionData.
func buildLegacyBalanceData(year int) (BalanceData, error) {
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

	nonCurrentDefs, err := loadTopLevelCategoryDefs("asset", "non_current_asset")
	if err != nil {
		return BalanceData{}, fmt.Errorf("load non-current asset definitions: %w", err)
	}
	currentDefs, err := loadTopLevelCategoryDefs("asset", "current_asset")
	if err != nil {
		return BalanceData{}, fmt.Errorf("load current asset definitions: %w", err)
	}
	longTermLiabilityDefs, err := loadTopLevelCategoryDefs("liability", "long_term_liability")
	if err != nil {
		return BalanceData{}, fmt.Errorf("load long-term liability definitions: %w", err)
	}
	currentLiabilityDefs, err := loadTopLevelCategoryDefs("liability", "current_liability")
	if err != nil {
		return BalanceData{}, fmt.Errorf("load current liability definitions: %w", err)
	}

	// I now use the stored top-level category metadata as the line definition source so any
	// user-created asset or liability category can appear in the statement without code changes.
	for _, item := range nonCurrentDefs {
		data.NonCurrentAssets = append(data.NonCurrentAssets, BalanceLine{
			Name:        item.Name,
			Amount:      currentSnapshot.NonCurrentAssets[item.Name],
			PriorAmount: priorSnapshot.NonCurrentAssets[item.Name],
		})
	}

	for _, item := range currentDefs {
		data.CurrentAssets = append(data.CurrentAssets, BalanceLine{
			Name:        item.Name,
			Amount:      currentSnapshot.CurrentAssets[item.Name],
			PriorAmount: priorSnapshot.CurrentAssets[item.Name],
		})
	}

	for _, item := range longTermLiabilityDefs {
		data.LongTermLiabilities = append(data.LongTermLiabilities, BalanceLine{
			Name:        item.Name,
			Amount:      currentSnapshot.LongTermLiabilities[item.Name],
			PriorAmount: priorSnapshot.LongTermLiabilities[item.Name],
		})
	}

	for _, item := range currentLiabilityDefs {
		data.CurrentLiabilities = append(data.CurrentLiabilities, BalanceLine{
			Name:        item.Name,
			Amount:      currentSnapshot.CurrentLiabilities[item.Name],
			PriorAmount: priorSnapshot.CurrentLiabilities[item.Name],
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
	data.PriorYearAdjustment = currentSnapshot.PriorYearAdjustment
	data.PriorPriorAdjustment = priorSnapshot.PriorYearAdjustment
	data.BalanceDifference = currentSnapshot.BalanceDifference
	data.PriorBalanceDifference = priorSnapshot.BalanceDifference
	data.FundConfigured = currentSnapshot.FundConfigured
	data.PriorFundConfigured = priorSnapshot.FundConfigured

	// Build a grouped bar chart comparing the five key balance sheet figures across the
	// current and prior years. The prior year uses a muted grey colour while the current
	// year uses the application's brand green, making the comparison visually immediate.
	data.Chart = viewmodels.ChartData{
		Labels: []string{"Assets", "Liabilities", "Equity", "Accumulated Fund", "Income Surplus"},
		Datasets: []viewmodels.ChartDataset{
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

	nonCurrentDefs, err := loadTopLevelCategoryDefs("asset", "non_current_asset")
	if err != nil {
		return balanceSnapshot{}, fmt.Errorf("load non-current asset definitions for %d: %w", year, err)
	}
	currentDefs, err := loadTopLevelCategoryDefs("asset", "current_asset")
	if err != nil {
		return balanceSnapshot{}, fmt.Errorf("load current asset definitions for %d: %w", year, err)
	}
	longTermLiabilityDefs, err := loadTopLevelCategoryDefs("liability", "long_term_liability")
	if err != nil {
		return balanceSnapshot{}, fmt.Errorf("load long-term liability definitions for %d: %w", year, err)
	}
	currentLiabilityDefs, err := loadTopLevelCategoryDefs("liability", "current_liability")
	if err != nil {
		return balanceSnapshot{}, fmt.Errorf("load current liability definitions for %d: %w", year, err)
	}

	// Non-current assets are cumulative: all transactions from the beginning of time up to
	// the end of the reporting year. There is no opening balance to add because these
	// categories represent long-term holdings, not flow accounts.
	nonCurrentTotals, err := loadTopLevelPositions("asset", categoryDefNames(nonCurrentDefs), yearStart, yearEnd)
	if err != nil {
		return balanceSnapshot{}, fmt.Errorf("load non-current asset totals for %d: %w", year, err)
	}
	assetSchedule, err := buildFixedAssetData(year)
	if err != nil {
		return balanceSnapshot{}, fmt.Errorf("build non-current asset schedule for %d: %w", year, err)
	}
	// When detailed class movements exist, Note 21's carrying amounts replace gross cost in
	// the financial position. The zero-cost guard preserves legacy installations that posted
	// directly to the old top-level PPE account and therefore cannot yet be depreciated by class.
	if assetSchedule.TotalClosingCost != 0 {
		nonCurrentTotals["Property, Plant & Equipment"] = assetSchedule.PPECarryingAmount
		nonCurrentTotals["Intangible Assets"] = assetSchedule.IntangibleCarryingAmount
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
	currentAssetTotals, err := loadTopLevelPositions("asset", categoryDefNames(currentDefs), yearStart, yearEnd)
	if err != nil {
		return balanceSnapshot{}, fmt.Errorf("load current asset totals for %d: %w", year, err)
	}
	// Liquid accounts sum only transactions within the current year, because the opening
	// balance already captures the position from prior periods.
	cashAssetTotals, err := loadDirectCategorySums("asset", []string{"Bank", "Cash", "Momo"}, yearStart, yearEnd)
	if err != nil {
		return balanceSnapshot{}, fmt.Errorf("load liquid asset totals for %d: %w", year, err)
	}

	currentAssets := map[string]float64{}
	for _, item := range currentDefs {
		switch item.Name {
		case "Bank":
			currentAssets[item.Name] = cashAssetTotals["Bank"] + openingBalances["bank"]
		case "Cash":
			currentAssets[item.Name] = cashAssetTotals["Cash"] + openingBalances["cash"]
		case "Momo":
			currentAssets[item.Name] = cashAssetTotals["Momo"] + openingBalances["momo"]
		default:
			currentAssets[item.Name] = currentAssetTotals[item.Name]
		}
	}

	// Liabilities are cumulative up to year-end, following the same pattern as non-current
	// assets. They represent obligations that persist across periods.
	longTermLiabilityTotals, err := loadTopLevelPositions("liability", categoryDefNames(longTermLiabilityDefs), yearStart, yearEnd)
	if err != nil {
		return balanceSnapshot{}, fmt.Errorf("load long-term liabilities for %d: %w", year, err)
	}
	currentLiabilityTotals, err := loadTopLevelPositions("liability", categoryDefNames(currentLiabilityDefs), yearStart, yearEnd)
	if err != nil {
		return balanceSnapshot{}, fmt.Errorf("load current liabilities for %d: %w", year, err)
	}

	snapshot := balanceSnapshot{
		NonCurrentAssets:    nonCurrentTotals,
		CurrentAssets:       currentAssets,
		LongTermLiabilities: longTermLiabilityTotals,
		CurrentLiabilities:  currentLiabilityTotals,
	}

	// Sum all asset and liability categories into their respective totals. The iteration
	// order doesn't matter here because we're only interested in the aggregated values.
	for _, amount := range snapshot.NonCurrentAssets {
		snapshot.TotalAssets += amount
	}
	for _, amount := range snapshot.CurrentAssets {
		snapshot.TotalAssets += amount
	}
	for _, amount := range snapshot.LongTermLiabilities {
		snapshot.TotalLiabilities += amount
	}
	for _, amount := range snapshot.CurrentLiabilities {
		snapshot.TotalLiabilities += amount
	}

	// Income surplus is the net of all income and expenditure transactions within the
	// reporting year. The opening fund and prior-year adjustment are independently entered
	// in Setup, so the balance check below can expose omitted assets or liabilities instead
	// of manufacturing a residual equity figure that always makes the statement balance.
	var yearlyIncome, yearlyExpense float64
	if err := loadIncomeExpenseTotals(yearStart, yearEnd, &yearlyIncome, &yearlyExpense); err != nil {
		return balanceSnapshot{}, fmt.Errorf("load income surplus for %d: %w", year, err)
	}
	postedDepreciation, err := loadNoteMovement("expenditure", "21", yearStart, yearEnd, false)
	if err != nil {
		return balanceSnapshot{}, fmt.Errorf("load posted depreciation for %d: %w", year, err)
	}
	if postedDepreciation == 0 {
		yearlyExpense += assetSchedule.TotalCharge
	}
	snapshot.IncomeSurplus = yearlyIncome - yearlyExpense
	fund, err := loadFundRollforward(year)
	if err != nil {
		return balanceSnapshot{}, fmt.Errorf("load accumulated fund roll-forward for %d: %w", year, err)
	}
	snapshot.FundConfigured = fund.Exists
	snapshot.PriorYearAdjustment = fund.PriorYearAdjustment
	snapshot.AccumulatedFund = fund.OpeningBalance + fund.PriorYearAdjustment
	snapshot.TotalEquity = snapshot.AccumulatedFund + snapshot.IncomeSurplus
	snapshot.BalanceDifference = snapshot.TotalAssets - snapshot.TotalLiabilities - snapshot.TotalEquity

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
	args := make([]any, 0, len(names)+2)
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

// loadTopLevelPositions combines independently entered opening balances with signed
// current-year movements. When a category family has no configured opening, it falls back
// to the historical cumulative behaviour used by older releases so upgrades do not lose
// balances merely because the new setup screen has not yet been completed.
func loadTopLevelPositions(categoryType string, names []string, startDate, endDate string) (map[string]float64, error) {
	historical, err := loadTopLevelSums(categoryType, names, endDate)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return historical, nil
	}

	placeholders := make([]string, len(names))
	args := []any{startDate[:4], categoryType}
	for index, name := range names {
		placeholders[index] = "?"
		args = append(args, name)
	}
	openingQuery := fmt.Sprintf(`
		SELECT top.name, COALESCE(SUM(b.amount), 0), COUNT(b.category_id)
		FROM categories top
		LEFT JOIN categories leaf ON leaf.type = top.type AND (leaf.id = top.id OR leaf.parent_id = top.id)
		LEFT JOIN account_opening_balances b ON b.category_id = leaf.id AND b.year = ?
		WHERE top.type = ? AND top.parent_id = 0 AND top.name IN (%s)
		GROUP BY top.id, top.name
	`, strings.Join(placeholders, ","))
	rows, err := db.DB.Query(openingQuery, args...)
	if err != nil {
		return nil, err
	}
	openings := map[string]float64{}
	configured := map[string]bool{}
	for rows.Next() {
		var name string
		var amount float64
		var count int
		if err := rows.Scan(&name, &amount, &count); err != nil {
			rows.Close()
			return nil, err
		}
		openings[name] = amount
		configured[name] = count > 0
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	movements, err := loadTopLevelPeriodSums(categoryType, names, startDate, endDate)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		if configured[name] {
			historical[name] = openings[name] + movements[name]
		}
	}

	// Legacy releases stored only Bank/Cash/Momo openings in a separate table. Once those
	// accounts are nested beneath the workbook's Note 26 parent, preserve the confirmed
	// brought-forward total unless the operator has supplied newer account-level openings.
	if categoryType == "asset" && containsString(names, "Cash & Cash Equivalents") && !configured["Cash & Cash Equivalents"] {
		var legacyOpening float64
		var legacyCount int
		if err := db.DB.QueryRow(`
			SELECT COALESCE(SUM(amount), 0), COUNT(*)
			FROM opening_balances
			WHERE year = ?
		`, startDate[:4]).Scan(&legacyOpening, &legacyCount); err != nil {
			return nil, err
		}
		if legacyCount > 0 {
			historical["Cash & Cash Equivalents"] = legacyOpening + movements["Cash & Cash Equivalents"]
		}
	}
	return historical, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func loadTopLevelPeriodSums(categoryType string, names []string, startDate, endDate string) (map[string]float64, error) {
	if len(names) == 0 {
		return map[string]float64{}, nil
	}
	placeholders := make([]string, len(names))
	args := []any{startDate, endDate, categoryType}
	for index, name := range names {
		placeholders[index] = "?"
		args = append(args, name)
	}
	query := fmt.Sprintf(`
		SELECT top.name, COALESCE(SUM(t.amount), 0)
		FROM categories top
		LEFT JOIN categories leaf ON leaf.type = top.type AND (leaf.id = top.id OR leaf.parent_id = top.id)
		LEFT JOIN transactions t ON t.category_id = leaf.id AND t.type = top.type AND t.date >= ? AND t.date < ?
		WHERE top.type = ? AND top.parent_id = 0 AND top.name IN (%s)
		GROUP BY top.id, top.name
	`, strings.Join(placeholders, ","))
	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]float64{}
	for rows.Next() {
		var name string
		var amount float64
		if err := rows.Scan(&name, &amount); err != nil {
			return nil, err
		}
		result[name] = amount
	}
	return result, rows.Err()
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
	args := make([]any, 0, len(names)+3)
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

func loadTopLevelCategoryDefs(categoryType, reportSection string) ([]balanceCategoryDef, error) {
	query := `
		SELECT name
		FROM categories
		WHERE type = ? AND parent_id = 0
	`
	args := []any{categoryType}
	if reportSection != "" {
		query += ` AND COALESCE(report_section, '') = ?`
		args = append(args, reportSection)
	}
	query += ` ORDER BY id`

	rows, err := db.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var defs []balanceCategoryDef
	for rows.Next() {
		var item balanceCategoryDef
		if err := rows.Scan(&item.Name); err != nil {
			return nil, err
		}
		defs = append(defs, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return defs, nil
}

func categoryDefNames(defs []balanceCategoryDef) []string {
	names := make([]string, 0, len(defs))
	for _, item := range defs {
		names = append(names, item.Name)
	}
	return names
}
