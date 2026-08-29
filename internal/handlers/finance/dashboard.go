package finance

import (
	"fmt"
	"net/http"
	"ramseyer-finance/internal/db"
	"ramseyer-finance/internal/handlers/viewmodels"
	"time"
)

// DashboardData carries all the template variables for the main dashboard page. It bundles
// the current period labels (month and year), the monthly and yearly income/expense/surplus
// summary figures, the current balances for the three liquid accounts (bank, cash, momo),
// the category option lists for the four transaction type dropdowns, the most recent ten
// transactions for the activity feed, and the chart data for the monthly income-vs-expense
// bar chart with a surplus trend line. The OpenModal field allows redirects to pre-open
// a specific modal (e.g., the "add transaction" form) after a form submission or navigation.
type DashboardData struct {
	Active            string
	Message           string
	OpenModal         string
	GreetingIntro     string
	GreetingMessage   string
	GreetingCount     int
	GreetingTimeLabel string
	ShowSetupHint     bool
	CurrentMonth      string
	CurrentYear       int
	CurrentDate       string
	CurrentDayLabel   string
	MonthIncome       float64
	MonthExpense      float64
	MonthSurplus      float64
	YearIncome        float64
	YearExpense       float64
	YearSurplus       float64
	DailyIncome       float64
	DailyExpense      float64
	DailyAsset        float64
	DailyLiability    float64
	DailyNetIncome    float64
	DailyPostedCount  int
	BankBalance       float64
	MomoBalance       float64
	CashBalance       float64
	IncomeCats        []CatOption
	ExpenseCats       []CatOption
	AssetCats         []CatOption
	LiabilityCats     []CatOption
	DailyTransactions []RecentTx
	Chart             viewmodels.ChartData
}

// RecentTx represents a single row in the dashboard's recent transaction feed. It carries
// the bare minimum fields needed for the compact list display: the date, the transaction
// type (used to colour-code the row and prefix the amount with a + or - sign), the category
// name, an optional description, and the monetary amount. Unlike the full transaction editor,
// this struct omits fields like ID, category_id, updated_at, and note_ref because the
// dashboard feed is read-only and designed for quick glancing rather than editing.
type RecentTx struct {
	Date          string
	Type          string
	Category      string
	PaymentMethod string
	Description   string
	Amount        float64
}

// Dashboard serves the main application home screen. It delegates entirely to
// buildDashboardData to assemble the view model, passing the current time, the active
// navigation tab identifier, any feedback message from the query string, and an optional
// modal name to open on page load (e.g., "add-income" after navigating from a shortcut).
// The handler itself is intentionally thin—all data assembly logic lives in the builder
// function so it can be reused by other handlers that need the same dashboard dataset.
func Dashboard(w http.ResponseWriter, r *http.Request) {
	data, err := buildDashboardData(time.Now(), "dashboard", r.URL.Query().Get("msg"), r.URL.Query().Get("open"))
	if err != nil {
		serverError(w, err)
		return
	}
	RenderTemplate(w, "dashboard", data)
}

// buildDashboardData assembles the complete DashboardData struct from the database and
// the provided parameters. It loads all four category type lists for the transaction entry
// modals, computes monthly and yearly income/expense/surplus totals using the same shared
// helpers that power the report pages (ensuring mathematical consistency between the
// dashboard summary and the full financial statements), calculates liquid account balances
// by combining opening balances with in-year transaction movements up to today, fetches
// the ten most recent transactions for the activity feed, and builds the twelve-month
// income-vs-expense chart with a surplus trend line. The function is parameterised with
// the current time rather than calling time.Now() internally so it can be tested with
// fixed dates.
func buildDashboardData(now time.Time, active, message, openModal string) (DashboardData, error) {
	// I build the dashboard from the same category and balance primitives used elsewhere so the
	// home screen stays mathematically consistent with the statements.
	incomeCats, err := loadCategories("income", false)
	if err != nil {
		return DashboardData{}, fmt.Errorf("load income categories: %w", err)
	}
	expenseCats, err := loadCategories("expenditure", false)
	if err != nil {
		return DashboardData{}, fmt.Errorf("load expenditure categories: %w", err)
	}
	assetCats, err := loadCategories("asset", false)
	if err != nil {
		return DashboardData{}, fmt.Errorf("load asset categories: %w", err)
	}
	liabilityCats, err := loadCategories("liability", false)
	if err != nil {
		return DashboardData{}, fmt.Errorf("load liability categories: %w", err)
	}
	data := DashboardData{
		Active:          active,
		Message:         message,
		OpenModal:       openModal,
		CurrentMonth:    now.Format("January 2006"),
		CurrentYear:     now.Year(),
		CurrentDate:     now.Format("2006-01-02"),
		CurrentDayLabel: now.Format("Monday, 02 January 2006"),
		IncomeCats:      incomeCats,
		ExpenseCats:     expenseCats,
		AssetCats:       assetCats,
		LiabilityCats:   liabilityCats,
	}

	greetingConfig, err := loadDashboardGreetingState(now)
	if err != nil {
		return DashboardData{}, fmt.Errorf("load dashboard greeting state: %w", err)
	}
	data.GreetingIntro = greetingConfig.CurrentGreetingIntro
	data.GreetingMessage = greetingConfig.CurrentGreeting
	data.GreetingCount = greetingConfig.GreetingCount
	data.GreetingTimeLabel = greetingConfig.RotationTimeLabel
	data.ShowSetupHint = greetingConfig.ShowSetupHint

	// Compute the date boundaries for the current month and year. These are passed to the
	// shared loadIncomeExpenseTotals function, which is the same function used by the
	// income-statement and balance-sheet report pages—guaranteeing that the dashboard
	// summary numbers match the detailed statements exactly.
	monthStart, monthEnd := monthBounds(now)
	yearStart, yearEnd := yearBounds(now.Year())
	if err := loadIncomeExpenseTotals(monthStart, monthEnd, &data.MonthIncome, &data.MonthExpense); err != nil {
		return DashboardData{}, fmt.Errorf("load monthly totals: %w", err)
	}
	data.MonthSurplus = data.MonthIncome - data.MonthExpense

	if err := loadIncomeExpenseTotals(yearStart, yearEnd, &data.YearIncome, &data.YearExpense); err != nil {
		return DashboardData{}, fmt.Errorf("load yearly totals: %w", err)
	}
	data.YearSurplus = data.YearIncome - data.YearExpense

	// I keep the daily summary separate from the monthly and yearly figures because the client asked
	// for a place to see "today so far" without mentally subtracting values from broader periods.
	dayStart := now.Format("2006-01-02")
	dayEnd := now.AddDate(0, 0, 1).Format("2006-01-02")
	if err := loadDailyDashboardTotals(dayStart, dayEnd, &data); err != nil {
		return DashboardData{}, fmt.Errorf("load daily totals: %w", err)
	}
	data.DailyNetIncome = data.DailyIncome - data.DailyExpense

	// The balance cutoff is tomorrow (now + 1 day) so that transactions dated today are
	// included in the balance display. Using "<" with tomorrow's date effectively means
	// "<= today" while keeping the query compatible with the exclusive upper bound pattern
	// used consistently across all report queries.
	balanceCutoff := now.AddDate(0, 0, 1).Format("2006-01-02")
	rows, err := db.DB.Query(`
		SELECT CASE
			WHEN c.name IN ('Cash on hand', 'Petty Cash', 'Cash') THEN 'Cash'
			WHEN c.name = 'Momo' THEN 'Momo'
			ELSE 'Bank'
		END AS balance_group,
		COALESCE(SUM(posting.amount), 0)
		FROM categories c
		LEFT JOIN financial_postings posting
			ON posting.category_id = c.id
			AND posting.account_type = 'asset'
			AND posting.date < ?
		WHERE c.type = 'asset' AND c.note_ref = '26'
		GROUP BY balance_group
		ORDER BY balance_group
	`, balanceCutoff)
	if err != nil {
		return DashboardData{}, fmt.Errorf("query account balances: %w", err)
	}
	defer rows.Close()

	// Map the workbook's detailed Note 26 accounts into the three compact dashboard cards.
	// The underlying statements retain every individual account; this grouping is presentation
	// only and never creates another saved balance.
	for rows.Next() {
		var name string
		var amount float64
		if err := rows.Scan(&name, &amount); err != nil {
			return DashboardData{}, fmt.Errorf("scan account balance: %w", err)
		}
		switch name {
		case "Bank":
			data.BankBalance = amount
		case "Cash":
			data.CashBalance = amount
		case "Momo":
			data.MomoBalance = amount
		}
	}
	if err := rows.Err(); err != nil {
		return DashboardData{}, fmt.Errorf("iterate account balances: %w", err)
	}

	// I replaced the old generic "recent" feed with a same-day accumulated view because the client
	// specifically asked for a way to see the day's total movement and the postings behind it.
	// Ordering by ID ascending makes the list read like a build-up of the day's activity instead
	// of a reverse-chronological log that is better suited to the full register.
	recentRows, err := db.DB.Query(`
		SELECT date, type, category, COALESCE(NULLIF(payment_method, ''), 'cash'), description, amount
		FROM transactions
		WHERE date >= ? AND date < ?
		ORDER BY date ASC, id ASC
	`, dayStart, dayEnd)
	if err != nil {
		return DashboardData{}, fmt.Errorf("query daily transactions: %w", err)
	}
	defer recentRows.Close()

	for recentRows.Next() {
		var transaction RecentTx
		if err := recentRows.Scan(
			&transaction.Date,
			&transaction.Type,
			&transaction.Category,
			&transaction.PaymentMethod,
			&transaction.Description,
			&transaction.Amount,
		); err != nil {
			return DashboardData{}, fmt.Errorf("scan daily transaction: %w", err)
		}
		transaction.PaymentMethod = paymentMethodLabel(transaction.PaymentMethod)
		data.DailyTransactions = append(data.DailyTransactions, transaction)
	}
	if err := recentRows.Err(); err != nil {
		return DashboardData{}, fmt.Errorf("iterate daily transactions: %w", err)
	}

	// I reuse the same monthly totals aggregation that powers the report pages so the dashboard
	// chart never drifts onto its own calculation path.
	// Build the twelve-month income-vs-expense chart. loadMonthlyTotals returns a map of
	// month numbers (1–12) to income/expense totals, using the same aggregation logic as
	// the income statement report. This ensures the dashboard chart bars match the monthly
	// breakdown a user would see if they navigated to the full report.
	monthlyTotals, err := loadMonthlyTotals(now.Year())
	if err != nil {
		return DashboardData{}, fmt.Errorf("load dashboard chart totals: %w", err)
	}
	// The chart uses three-letter month abbreviations as X-axis labels. We iterate months
	// 1 through 12, pulling each month's totals from the map and computing the surplus
	// as income minus expense. Months with no transactions will have zero values, which
	// the chart renders as empty bars rather than omitting the label.
	chartLabels := []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
	var incomeSeries []float64
	var expenseSeries []float64
	var surplusSeries []float64
	for monthNumber, label := range chartLabels {
		totals := monthlyTotals[monthNumber+1]
		data.Chart.Labels = append(data.Chart.Labels, label)
		incomeSeries = append(incomeSeries, totals.Income)
		expenseSeries = append(expenseSeries, totals.Expense)
		surplusSeries = append(surplusSeries, totals.Income-totals.Expense)
	}
	// Three datasets are rendered: income as gold bars, expense as green bars, and surplus
	// as a red line overlaid on top. The bar+line combination makes it easy to see both
	// the absolute amounts and whether each month ran a surplus or deficit.
	data.Chart.Datasets = []viewmodels.ChartDataset{
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

// loadDailyDashboardTotals populates the dashboard's same-day operational totals using one grouped
// query over the four transaction namespaces. I keep this logic next to the dashboard builder rather
// than reusing the monthly income/expense helper because the daily view needs all four types, plus a
// posting count, to answer the client's question about "what has happened today so far" in one pass.
func loadDailyDashboardTotals(startDate, endDate string, data *DashboardData) error {
	rows, err := db.DB.Query(`
		SELECT type, COALESCE(SUM(amount), 0), COUNT(*)
		FROM transactions
		WHERE date >= ? AND date < ?
		GROUP BY type
	`, startDate, endDate)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			transactionType string
			total           float64
			count           int
		)
		if err := rows.Scan(&transactionType, &total, &count); err != nil {
			return err
		}
		data.DailyPostedCount += count
		switch transactionType {
		case "income":
			data.DailyIncome = total
		case "expenditure":
			data.DailyExpense = total
		case "asset":
			data.DailyAsset = total
		case "liability":
			data.DailyLiability = total
		}
	}

	return rows.Err()
}

// loadIncomeExpenseTotals queries the total income and total expenditure within a given
// date range using a single grouped query. The results are written directly to the
// caller-provided pointer targets, allowing the function to populate multiple call sites
// (dashboard monthly, dashboard yearly, income statement, balance sheet surplus) without
// duplicating the SQL. If a transaction type has no rows in the range, the corresponding
// target is left unchanged (typically remaining at its zero value from initialisation),
// which correctly represents "no transactions" as a zero total.
func loadIncomeExpenseTotals(startDate, endDate string, incomeTarget, expenseTarget *float64) error {
	// I aggregate income and expenditure together in one grouped query so the database does the
	// range math and the caller only handles the two totals it actually needs.
	rows, err := db.DB.Query(`
		SELECT account_type, COALESCE(SUM(amount), 0)
		FROM financial_postings
		WHERE date >= ? AND date < ? AND account_type IN ('income', 'expenditure')
		GROUP BY account_type
	`, startDate, endDate)
	if err != nil {
		return err
	}
	defer rows.Close()

	// The query returns at most two rows (one for income, one for expenditure). We scan
	// each row's type and total, then assign to the appropriate target pointer. If a type
	// has no transactions in the range, it simply won't appear in the result set, and the
	// target pointer will retain whatever value the caller initialised it with.
	for rows.Next() {
		var transactionType string
		var total float64
		if err := rows.Scan(&transactionType, &total); err != nil {
			return err
		}
		switch transactionType {
		case "income":
			*incomeTarget = total
		case "expenditure":
			*expenseTarget = total
		}
	}

	return rows.Err()
}
