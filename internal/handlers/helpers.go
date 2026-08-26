package handlers

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"ramseyer-finance/internal/db"
	"sort"
	"strconv"
	"strings"
	"time"
)

// BudgetOption represents a single selectable category in the budget configuration
// interface. It carries the database ID (used as a foreign key when saving budget amounts),
// the category type ("income" or "expenditure"), and the display name. Budget options are
// limited to top-level categories only, matching the granularity at which the annual
// budget comparison operates.
type BudgetOption struct {
	ID   int64
	Type string
	Name string
}

// SavedBudget represents a single persisted budget row, joined with its category metadata
// for display in the setup page's budget table. The CategoryType and CategoryName fields
// come from the categories table via a JOIN, allowing the UI to show human-readable labels
// without additional queries. Amount is stored as a float64, matching the application's
// convention for monetary values.
type SavedBudget struct {
	ID           int64
	Year         int
	CategoryType string
	CategoryName string
	Amount       float64
}

// SavedOpeningBalance represents a single persisted opening balance row for the setup
// page's balance table. It pairs a year, an account type identifier ("bank", "cash", or
// "momo"), a human-readable account label for display, and the monetary amount. The
// AccountLabel is derived from AccountType via the accountLabel helper rather than being
// stored in the database, keeping the schema normalised.
type SavedOpeningBalance struct {
	Year         int
	AccountType  string
	AccountLabel string
	Amount       float64
}

// SavedFundRollforward stores the two independently entered values required to bridge the
// prior closing accumulated fund into the current reporting year. The current surplus is
// calculated from transactions, so keeping it out of this setup record prevents operators
// from accidentally overriding the statement of financial performance.
type SavedFundRollforward struct {
	Year                int
	OpeningBalance      float64
	PriorYearAdjustment float64
}

type BalanceAccountOption struct {
	ID    int64
	Type  string
	Label string
}

type SavedAccountOpeningBalance struct {
	Year         int
	CategoryType string
	AccountLabel string
	Amount       float64
}

type FixedAssetOpeningOption struct {
	ID    int64
	Label string
}

type SavedFixedAssetOpening struct {
	Year                  int
	AccountLabel          string
	OpeningCost           float64
	OpeningAccumulatedDep float64
}

// FundRollforward is the calculation input shared by the financial performance and
// financial position reports. Exists distinguishes an intentionally entered zero from a
// missing setup record, which lets reports warn about incomplete year-opening data.
type FundRollforward struct {
	OpeningBalance      float64
	PriorYearAdjustment float64
	Exists              bool
}

// TransactionCategoryMeta holds the resolved category metadata for a single transaction's
// category assignment. It includes the category's own ID, type, and name, plus the parent
// category's ID and name (if the category is a subcategory), and the note_ref code. This
// struct is used when editing an existing transaction to pre-populate the category dropdown
// and display the hierarchical category path.
type TransactionCategoryMeta struct {
	ID         int64
	Type       string
	Name       string
	ParentID   int64
	ParentName string
	NoteRef    string
}

// CategoryChoice represents a single option in a flat category selection dropdown, used
// by transaction edit forms and the register filter. The Label field already includes the
// parent prefix (e.g., "Offering / Children Service") so the UI can render a simple flat
// list without needing to understand the two-level hierarchy. The Type field allows the
// frontend to filter or group choices by transaction type.
type CategoryChoice struct {
	ID    int64
	Type  string
	Label string
}

// serverError logs the given error with a standard prefix and writes a generic 500
// Internal Server Error response to the client. The actual error message is intentionally
// not included in the HTTP response body to avoid leaking implementation details (database
// schema, file paths, etc.) to end users. The full error is written to the server log for
// administrator diagnosis.
func serverError(w http.ResponseWriter, err error) {
	log.Printf("request failed: %v", err)
	http.Error(w, "Internal server error", http.StatusInternalServerError)
}

// badRequest writes a 400 Bad Request response with the given message as the response body.
// Unlike serverError, this function includes the message directly in the HTTP response
// because 400-level errors represent client mistakes (invalid parameters, missing fields)
// that the user or frontend needs to understand and correct.
func badRequest(w http.ResponseWriter, message string) {
	http.Error(w, message, http.StatusBadRequest)
}

// parseReportYear converts a raw query parameter string into a valid report year. If the
// input is empty or whitespace-only, it returns the current calendar year as a sensible
// default—this means navigating to a report page without a "?year=" parameter automatically
// shows the current year's data. Non-numeric input produces an error; numeric input is
// returned as-is without range validation, allowing historical years and near-future years
// for planning purposes.
func parseReportYear(raw string) (int, error) {
	// I default empty year selectors to the current year so direct navigation and bookmarked report
	// routes still land on a useful period without requiring every caller to supply a query param.
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Now().Year(), nil
	}

	year, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid year %q", value)
	}

	return year, nil
}

// yearBounds returns the inclusive start date and exclusive end date strings for a given
// calendar year, both in "2006-01-02" format. The start is January 1st at midnight UTC,
// and the end is January 1st of the following year. Using an exclusive upper bound ("<" in
// SQL queries) is the standard pattern across all report helpers because it correctly
// includes all times on December 31st without needing to handle time components.
func yearBounds(year int) (string, string) {
	start := time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(1, 0, 0)
	return start.Format("2006-01-02"), end.Format("2006-01-02")
}

// monthBounds returns the inclusive start date and exclusive end date strings for the
// month containing the given time, both in "2006-01-02" format. The start is the first day
// of the month at midnight UTC, and the end is the first day of the following month. This
// is used by the dashboard to compute monthly income/expense totals and by any other
// handler that needs a single month's date range.
func monthBounds(now time.Time) (string, string) {
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	return start.Format("2006-01-02"), end.Format("2006-01-02")
}

// reportYears builds the list of years available in the report page year-picker dropdown.
// It merges three sources: the currently selected year (always included, even if it has no
// data), the years that actually contain transactions or budgets or opening balances
// (loaded from the database), and a window of ±2 years around the selected year. This
// approach ensures the picker includes both historical data years and near-future years
// for planning, while never being completely empty even on a fresh database.
func reportYears(selected int) ([]int, error) {
	// I merge known data years with a small window around the selected year so the selector works
	// both for historical browsing and near-future setup or planning.
	years, err := loadKnownYears()
	if err != nil {
		return nil, err
	}

	yearSet := map[int]struct{}{selected: {}}
	for _, year := range years {
		yearSet[year] = struct{}{}
	}
	// Add a window of two years on either side of the selected year so the user can
	// navigate forward or backward without hitting dead ends in the dropdown, even if
	// those surrounding years have no data yet.
	for year := selected - 2; year <= selected+2; year++ {
		yearSet[year] = struct{}{}
	}

	return sortYears(yearSet), nil
}

// setupYears builds the list of years available in the setup page year-picker dropdown.
// Unlike reportYears, which centres on the selected year, this function centres on the
// current year and extends further into the future (up to +5 years) because the setup page
// is where users configure budgets and opening balances for upcoming periods. Known data
// years from the database are merged in so historical setup data remains accessible.
func setupYears() ([]int, error) {
	currentYear := time.Now().Year()
	years, err := loadKnownYears()
	if err != nil {
		return nil, err
	}

	yearSet := map[int]struct{}{}
	// Default window: one year in the past (for reviewing last year's setup) and five
	// years into the future (for forward planning). This wider future window reflects
	// the setup page's role in configuring upcoming budgets and balances.
	for year := currentYear - 1; year <= currentYear+5; year++ {
		yearSet[year] = struct{}{}
	}
	for _, year := range years {
		yearSet[year] = struct{}{}
	}

	return sortYears(yearSet), nil
}

// loadKnownYears queries the database for all distinct years that appear across the three
// year-bearing data sources: transactions (extracted from the date column), budgets, and
// opening balances. The UNION query ensures each year appears only once in the result set,
// and the IS NOT NULL filter excludes transactions with empty date strings. The results
// are returned in ascending order and used by both the report and setup year pickers to
// ensure data-backed years are always selectable.
func loadKnownYears() ([]int, error) {
	// I treat transactions, budgets, and opening balances as year-bearing sources because the UI
	// needs one shared year vocabulary across setup and reporting.
	rows, err := db.DB.Query(`
		SELECT DISTINCT year
		FROM (
			SELECT CAST(strftime('%Y', date) AS INTEGER) AS year
			FROM transactions
			WHERE date <> ''
			UNION
			SELECT year FROM budgets
			UNION
			SELECT year FROM opening_balances
			UNION
			SELECT year FROM fund_rollforwards
			UNION
			SELECT year FROM account_opening_balances
			UNION
			SELECT year FROM fixed_asset_openings
		)
		WHERE year IS NOT NULL
		ORDER BY year
	`)
	if err != nil {
		return nil, fmt.Errorf("query known years: %w", err)
	}
	defer rows.Close()

	var years []int
	for rows.Next() {
		var year int
		if err := rows.Scan(&year); err != nil {
			return nil, fmt.Errorf("scan known year: %w", err)
		}
		years = append(years, year)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate known years: %w", err)
	}

	return years, nil
}

func loadFixedAssetOpeningOptions() ([]FixedAssetOpeningOption, error) {
	rows, err := db.DB.Query(`
		SELECT c.id, parent.name || ' / ' || c.name
		FROM categories c
		JOIN categories parent ON parent.id = c.parent_id
		WHERE c.type = 'asset' AND parent.name IN ('Property, Plant & Equipment', 'Intangible Assets')
		ORDER BY CASE parent.name WHEN 'Property, Plant & Equipment' THEN 0 ELSE 1 END, c.id
	`)
	if err != nil {
		return nil, fmt.Errorf("query fixed-asset opening options: %w", err)
	}
	defer rows.Close()
	var options []FixedAssetOpeningOption
	for rows.Next() {
		var option FixedAssetOpeningOption
		if err := rows.Scan(&option.ID, &option.Label); err != nil {
			return nil, fmt.Errorf("scan fixed-asset opening option: %w", err)
		}
		options = append(options, option)
	}
	return options, rows.Err()
}

func loadSavedFixedAssetOpenings() ([]SavedFixedAssetOpening, error) {
	rows, err := db.DB.Query(`
		SELECT o.year, parent.name || ' / ' || c.name, o.opening_cost, o.opening_accumulated_depreciation
		FROM fixed_asset_openings o
		JOIN categories c ON c.id = o.category_id
		JOIN categories parent ON parent.id = c.parent_id
		ORDER BY o.year DESC, parent.id, c.id
	`)
	if err != nil {
		return nil, fmt.Errorf("query fixed-asset openings: %w", err)
	}
	defer rows.Close()
	var openings []SavedFixedAssetOpening
	for rows.Next() {
		var opening SavedFixedAssetOpening
		if err := rows.Scan(&opening.Year, &opening.AccountLabel, &opening.OpeningCost, &opening.OpeningAccumulatedDep); err != nil {
			return nil, fmt.Errorf("scan fixed-asset opening: %w", err)
		}
		openings = append(openings, opening)
	}
	return openings, rows.Err()
}

func loadBalanceAccountOptions() ([]BalanceAccountOption, error) {
	rows, err := db.DB.Query(`
		SELECT c.id, c.type,
			CASE WHEN parent.name IS NULL THEN c.name ELSE parent.name || ' / ' || c.name END
		FROM categories c
		LEFT JOIN categories parent ON parent.id = c.parent_id
		WHERE c.type IN ('asset', 'liability') AND COALESCE(c.is_active, 1) = 1
			AND c.name NOT IN ('Property, Plant & Equipment', 'Intangible Assets')
			AND COALESCE(parent.name, '') NOT IN ('Property, Plant & Equipment', 'Intangible Assets')
		ORDER BY CASE c.type WHEN 'asset' THEN 0 ELSE 1 END,
			CASE WHEN c.parent_id = 0 THEN c.id ELSE c.parent_id END, c.parent_id, c.id
	`)
	if err != nil {
		return nil, fmt.Errorf("query balance account options: %w", err)
	}
	defer rows.Close()
	var options []BalanceAccountOption
	for rows.Next() {
		var option BalanceAccountOption
		if err := rows.Scan(&option.ID, &option.Type, &option.Label); err != nil {
			return nil, fmt.Errorf("scan balance account option: %w", err)
		}
		options = append(options, option)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate balance account options: %w", err)
	}
	return options, nil
}

func loadSavedAccountOpeningBalances() ([]SavedAccountOpeningBalance, error) {
	rows, err := db.DB.Query(`
		SELECT b.year, c.type,
			CASE WHEN parent.name IS NULL THEN c.name ELSE parent.name || ' / ' || c.name END,
			b.amount
		FROM account_opening_balances b
		JOIN categories c ON c.id = b.category_id
		LEFT JOIN categories parent ON parent.id = c.parent_id
		ORDER BY b.year DESC, c.type, COALESCE(parent.id, c.id), c.id
	`)
	if err != nil {
		return nil, fmt.Errorf("query account opening balances: %w", err)
	}
	defer rows.Close()
	var balances []SavedAccountOpeningBalance
	for rows.Next() {
		var balance SavedAccountOpeningBalance
		if err := rows.Scan(&balance.Year, &balance.CategoryType, &balance.AccountLabel, &balance.Amount); err != nil {
			return nil, fmt.Errorf("scan account opening balance: %w", err)
		}
		balances = append(balances, balance)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate account opening balances: %w", err)
	}
	return balances, nil
}

func loadSavedFundRollforwards() ([]SavedFundRollforward, error) {
	rows, err := db.DB.Query(`
		SELECT year, opening_balance, prior_year_adjustment
		FROM fund_rollforwards
		ORDER BY year DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query fund rollforwards: %w", err)
	}
	defer rows.Close()

	var records []SavedFundRollforward
	for rows.Next() {
		var record SavedFundRollforward
		if err := rows.Scan(&record.Year, &record.OpeningBalance, &record.PriorYearAdjustment); err != nil {
			return nil, fmt.Errorf("scan fund rollforward: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fund rollforwards: %w", err)
	}
	return records, nil
}

func loadFundRollforward(year int) (FundRollforward, error) {
	var result FundRollforward
	err := db.DB.QueryRow(`
		SELECT opening_balance, prior_year_adjustment
		FROM fund_rollforwards
		WHERE year = ?
	`, year).Scan(&result.OpeningBalance, &result.PriorYearAdjustment)
	if err == sql.ErrNoRows {
		return result, nil
	}
	if err != nil {
		return FundRollforward{}, fmt.Errorf("query fund rollforward for %d: %w", year, err)
	}
	result.Exists = true
	return result, nil
}

// sortYears converts a set of unique years into a sorted integer slice in ascending order.
// The map-to-slice conversion and sorting are combined into one helper because multiple
// functions (reportYears, setupYears) build year sets from different sources and all need
// the same final sorted output for dropdown rendering.
func sortYears(yearSet map[int]struct{}) []int {
	years := make([]int, 0, len(yearSet))
	for year := range yearSet {
		years = append(years, year)
	}
	sort.Ints(years)
	return years
}

// loadCategories retrieves category options for a given type from the database, returning
// them as CatOption structs suitable for populating a template dropdown or list. When
// topLevelOnly is true, only parent categories (parent_id=0) are returned—this is used
// by the budget configuration, which operates at the top-category level. When false, all
// categories are returned, and subcategories are tagged with Indent=1 so the template can
// visually indent them. Results are ordered by database ID, which corresponds to the
// manual ordering defined in the seed data.
func loadCategories(categoryType string, topLevelOnly bool) ([]CatOption, error) {
	// I keep category loading generic because the same hierarchy powers data entry, setup, and
	// statements. The template only needs the display indent, not a richer tree object.
	query := `SELECT id, name, parent_id FROM categories WHERE type=? AND COALESCE(is_active, 1) = 1`
	if topLevelOnly {
		query += ` AND parent_id=0`
	}
	query += `
		ORDER BY
			CASE WHEN parent_id = 0 THEN id ELSE parent_id END,
			parent_id,
			id
	`

	rows, err := db.DB.Query(query, categoryType)
	if err != nil {
		return nil, fmt.Errorf("query %s categories: %w", categoryType, err)
	}
	defer rows.Close()

	var options []CatOption
	for rows.Next() {
		var option CatOption
		var parentID int64
		if err := rows.Scan(&option.ID, &option.Name, &parentID); err != nil {
			return nil, fmt.Errorf("scan %s category: %w", categoryType, err)
		}
		// Subcategories are visually indented in the dropdown. The Indent field is used
		// by the template to add CSS padding or a prefix (like "--") so users can
		// distinguish between top-level and child categories at a glance.
		if parentID > 0 {
			option.Indent = 1
		}
		options = append(options, option)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s categories: %w", categoryType, err)
	}

	return options, nil
}

// lookupTransactionCategoryMeta resolves the full category metadata for a given category
// ID and transaction type. It joins against the categories table to fetch the category's
// own details and its parent's name (if any). This is used when loading an existing
// transaction for editing—the edit form needs to display the current category assignment
// as a human-readable label like "Offering / Children Service" and pre-select the correct
// entry in the category dropdown.
func lookupTransactionCategoryMeta(categoryID int64, transactionType string) (TransactionCategoryMeta, error) {
	var meta TransactionCategoryMeta
	err := db.DB.QueryRow(`
		SELECT c.id, c.type, c.name, c.parent_id, COALESCE(parent.name, ''), c.note_ref
		FROM categories c
		LEFT JOIN categories parent ON parent.id = c.parent_id
		WHERE c.id = ? AND c.type = ?
	`, categoryID, transactionType).Scan(
		&meta.ID,
		&meta.Type,
		&meta.Name,
		&meta.ParentID,
		&meta.ParentName,
		&meta.NoteRef,
	)
	if err != nil {
		return TransactionCategoryMeta{}, err
	}
	return meta, nil
}

// loadAllCategoryChoices retrieves every category from the database and formats each one
// as a CategoryChoice with a pre-computed display label. Subcategories are labelled as
// "ParentName / CategoryName" (e.g., "Offering / Children Service"), while top-level
// categories use just their own name. The results are sorted by type (income first, then
// expenditure, asset, liability) and then hierarchically—parent categories appear before
// their children, and categories within the same parent are ordered by database ID. This
// function is designed to be called once and cached or passed to templates, avoiding
// repeated queries on every form render.
func loadAllCategoryChoices() ([]CategoryChoice, error) {
	// I flatten category labels once here so edit forms and register screens do not need to rebuild
	// parent-child wording every time they render.
	rows, err := db.DB.Query(`
		SELECT c.id, c.type, c.name, COALESCE(parent.name, '')
		FROM categories c
		LEFT JOIN categories parent ON parent.id = c.parent_id
		ORDER BY
			CASE c.type
				WHEN 'income' THEN 0
				WHEN 'expenditure' THEN 1
				WHEN 'asset' THEN 2
				WHEN 'liability' THEN 3
				ELSE 4
			END,
			CASE WHEN c.parent_id = 0 THEN c.id ELSE parent.id END,
			c.parent_id,
			c.id
	`)
	if err != nil {
		return nil, fmt.Errorf("query category choices: %w", err)
	}
	defer rows.Close()

	var choices []CategoryChoice
	for rows.Next() {
		var (
			choice     CategoryChoice
			name       string
			parentName string
		)
		if err := rows.Scan(&choice.ID, &choice.Type, &name, &parentName); err != nil {
			return nil, fmt.Errorf("scan category choice: %w", err)
		}
		// Pre-compose the display label so templates can render a flat <option> list
		// without conditional logic. The " / " separator is a deliberate choice to
		// distinguish hierarchical relationships from category names that themselves
		// contain slashes or other separators.
		if parentName != "" {
			choice.Label = parentName + " / " + name
		} else {
			choice.Label = name
		}
		choices = append(choices, choice)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate category choices: %w", err)
	}

	return choices, nil
}

// loadBudgetCategories returns the top-level income and expenditure categories suitable
// for the budget configuration interface. Budgets are only tracked at the parent category
// level (not per subcategory), matching the granularity of the annual budget comparison
// report. Results are ordered with income categories first, then expenditure, both in
// their database ID order (which reflects the manual seed ordering).
func loadBudgetCategories() ([]BudgetOption, error) {
	// I scope budget categories to top-level income and expenditure because that is the exact
	// granularity the annual budget comparison logic expects.
	rows, err := db.DB.Query(`
		SELECT id, type, name
		FROM categories
		WHERE parent_id=0 AND type IN ('income', 'expenditure') AND COALESCE(is_active, 1) = 1
		ORDER BY CASE type WHEN 'income' THEN 0 ELSE 1 END, id
	`)
	if err != nil {
		return nil, fmt.Errorf("query budget categories: %w", err)
	}
	defer rows.Close()

	var options []BudgetOption
	for rows.Next() {
		var option BudgetOption
		if err := rows.Scan(&option.ID, &option.Type, &option.Name); err != nil {
			return nil, fmt.Errorf("scan budget category: %w", err)
		}
		options = append(options, option)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate budget categories: %w", err)
	}

	return options, nil
}

// loadSavedBudgets retrieves all persisted budget rows from the database, joined with
// their category type and name for display. Results are ordered by year descending (newest
// first), then by category type (income before expenditure), then by category ID (which
// follows the manual seed ordering). This ordering groups all budgets for the same year
// together and presents them in a logical sequence for the setup page table.
func loadSavedBudgets() ([]SavedBudget, error) {
	rows, err := db.DB.Query(`
		SELECT b.id, b.year, c.type, c.name, b.amount
		FROM budgets b
		JOIN categories c ON c.id = b.category_id
		ORDER BY b.year DESC, c.type, c.id
	`)
	if err != nil {
		return nil, fmt.Errorf("query saved budgets: %w", err)
	}
	defer rows.Close()

	var budgets []SavedBudget
	for rows.Next() {
		var budget SavedBudget
		if err := rows.Scan(&budget.ID, &budget.Year, &budget.CategoryType, &budget.CategoryName, &budget.Amount); err != nil {
			return nil, fmt.Errorf("scan saved budget: %w", err)
		}
		budgets = append(budgets, budget)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate saved budgets: %w", err)
	}

	return budgets, nil
}

// loadSavedOpeningBalances retrieves all persisted opening balance rows from the database,
// ordered by year descending (newest first) and then by account type in a fixed display
// order: bank, cash, momo. Each row's AccountLabel is derived from the AccountType using
// the accountLabel helper, translating the internal key ("bank") to a display label
// ("Bank") suitable for the setup page table.
func loadSavedOpeningBalances() ([]SavedOpeningBalance, error) {
	rows, err := db.DB.Query(`
		SELECT year, account_type, amount
		FROM opening_balances
		ORDER BY year DESC,
			CASE account_type
				WHEN 'bank' THEN 0
				WHEN 'cash' THEN 1
				WHEN 'momo' THEN 2
				ELSE 3
			END
	`)
	if err != nil {
		return nil, fmt.Errorf("query saved opening balances: %w", err)
	}
	defer rows.Close()

	var balances []SavedOpeningBalance
	for rows.Next() {
		var balance SavedOpeningBalance
		if err := rows.Scan(&balance.Year, &balance.AccountType, &balance.Amount); err != nil {
			return nil, fmt.Errorf("scan opening balance: %w", err)
		}
		balance.AccountLabel = accountLabel(balance.AccountType)
		balances = append(balances, balance)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate opening balances: %w", err)
	}

	return balances, nil
}

// loadOpeningBalanceMap retrieves the opening balances for a specific year and returns
// them as a map keyed by account type string ("bank", "cash", "momo"). This map-based
// return type is designed for programmatic consumption by the dashboard and balance sheet
// builders, which need to add opening balances to in-year transaction movements without
// iterating over a slice. Years with no configured balances return an empty map (not an
// error), and all missing keys default to Go's zero value for float64 (0.0).
func loadOpeningBalanceMap(year int) (map[string]float64, error) {
	// I return a normalized map keyed by account role because callers care about bank/cash/momo
	// semantics, not about setup table row ids.
	rows, err := db.DB.Query(`SELECT account_type, amount FROM opening_balances WHERE year=?`, year)
	if err != nil {
		return nil, fmt.Errorf("query opening balances for %d: %w", year, err)
	}
	defer rows.Close()

	balances := map[string]float64{}
	for rows.Next() {
		var accountType string
		var amount float64
		if err := rows.Scan(&accountType, &amount); err != nil {
			return nil, fmt.Errorf("scan opening balance for %d: %w", year, err)
		}
		balances[accountType] = amount
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate opening balances for %d: %w", year, err)
	}

	return balances, nil
}

// accountLabel converts an internal account type identifier to its human-readable display
// label. The three recognised types are "bank" → "Bank", "cash" → "Cash", and "momo" →
// "Momo". Any unrecognised type string is returned unchanged as a defensive fallback,
// ensuring that future account types added to the database will still display something
// meaningful rather than an empty string.
func accountLabel(accountType string) string {
	switch accountType {
	case "bank":
		return "Bank"
	case "cash":
		return "Cash"
	case "momo":
		return "Momo"
	default:
		return accountType
	}
}

// sanitizeReturnTo validates and sanitises a return-to URL parameter to prevent open
// redirect attacks. It only permits values that start with "/" (relative, in-app paths)
// and rejects anything else—including absolute URLs, protocol-relative URLs, and empty
// strings—by returning the provided fallback instead. This function should be used on
// any user-supplied redirect target before passing it to http.Redirect.
func sanitizeReturnTo(raw, fallback string) string {
	// I only allow relative in-app targets here because several forms echo a `return_to` value.
	// This keeps those redirects from becoming open redirect vectors.
	value := strings.TrimSpace(raw)
	if strings.HasPrefix(value, "/") {
		return value
	}
	return fallback
}

// queryEscape keeps feedback messages safe when handlers append them to an in-app redirect.
// Authentication has its own package-local equivalent; this shared version serves the
// transaction, setup, category, and backup route facades that remain in this package.
func queryEscape(raw string) string {
	replacer := strings.NewReplacer(
		" ", "+",
		"\"", "",
		"#", "",
		"&", "and",
		"?", "",
	)
	return replacer.Replace(raw)
}

// alertTone maps server feedback to the non-alarming success or warning styles used by
// authenticated pages. Keeping this presentation rule in the facade avoids domain packages
// depending on one another merely to classify a redirect message.
func alertTone(message string) string {
	normalized := strings.ToLower(strings.TrimSpace(message))
	if normalized == "" {
		return "success"
	}
	for _, marker := range []string{
		"incorrect", "invalid", "did not", "must", "create", "already",
		"replace", "failed", "duplicate", "required", "no backup file",
	} {
		if strings.Contains(normalized, marker) {
			return "warning"
		}
	}
	return "success"
}
