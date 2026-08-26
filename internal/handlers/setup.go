package handlers

import (
	"database/sql"
	"errors"
	"net/http"
	"ramseyer-finance/internal/appmeta"
	"ramseyer-finance/internal/db"
	"strconv"
	"strings"
	"time"
)

// SetupData carries all the template variables for the application setup page. This page
// consolidates several administrative concerns into one view: annual budget configuration
// (per top-level income/expenditure category), dashboard identity/greeting preferences,
// opening balance configuration for the three liquid accounts (bank, cash, momo), and the
// automatic backup policy (enabled/disabled, frequency). By grouping these together, the
// setup page serves as the single destination for year-start configuration and ongoing
// operational settings. The Message and MessageTone fields provide feedback after form
// submissions (e.g., "Budget saved" or "Opening balance saved").
type SetupData struct {
	Active             string
	AppVersion         string
	Years              []int
	Categories         []BudgetOption
	Budgets            []SavedBudget
	OpeningBalances    []SavedOpeningBalance
	FundRollforwards   []SavedFundRollforward
	BalanceAccounts    []BalanceAccountOption
	AccountOpenings    []SavedAccountOpeningBalance
	FixedAssetOptions  []FixedAssetOpeningOption
	FixedAssetOpenings []SavedFixedAssetOpening
	AutoBackup         AutoBackupConfig
	DashboardConfig    DashboardGreetingConfig
	Message            string
	MessageTone        string
}

// SetupPage serves the application setup and configuration page. It loads the list of
// available years (centred on the current year with a forward-looking window), the
// top-level income and expenditure categories for budget configuration, any previously
// saved budgets and opening balances for display in their respective tables, the current
// dashboard greeting configuration, and the current auto-backup configuration. Category
// management now lives on its own page so this screen stays focused on finance setup and
// operational controls instead of becoming one oversized administration dashboard.
func SetupPage(w http.ResponseWriter, r *http.Request) {
	// I keep setup intentionally narrower now that categories have their own page. This
	// screen is for budgets, dashboard greeting preferences, opening balances, PIN changes,
	// and backup policy only.
	years, err := setupYears()
	if err != nil {
		serverError(w, err)
		return
	}
	categories, err := loadBudgetCategories()
	if err != nil {
		serverError(w, err)
		return
	}
	budgets, err := loadSavedBudgets()
	if err != nil {
		serverError(w, err)
		return
	}
	openingBalances, err := loadSavedOpeningBalances()
	if err != nil {
		serverError(w, err)
		return
	}
	fundRollforwards, err := loadSavedFundRollforwards()
	if err != nil {
		serverError(w, err)
		return
	}
	balanceAccounts, err := loadBalanceAccountOptions()
	if err != nil {
		serverError(w, err)
		return
	}
	accountOpenings, err := loadSavedAccountOpeningBalances()
	if err != nil {
		serverError(w, err)
		return
	}
	fixedAssetOptions, err := loadFixedAssetOpeningOptions()
	if err != nil {
		serverError(w, err)
		return
	}
	fixedAssetOpenings, err := loadSavedFixedAssetOpenings()
	if err != nil {
		serverError(w, err)
		return
	}
	autoBackupConfig, err := loadAutoBackupConfig()
	if err != nil {
		serverError(w, err)
		return
	}
	dashboardConfig, err := loadDashboardGreetingState(time.Now())
	if err != nil {
		serverError(w, err)
		return
	}

	data := SetupData{
		Active:             "setup",
		AppVersion:         appmeta.CurrentVersion,
		Years:              years,
		Categories:         categories,
		Budgets:            budgets,
		OpeningBalances:    openingBalances,
		FundRollforwards:   fundRollforwards,
		BalanceAccounts:    balanceAccounts,
		AccountOpenings:    accountOpenings,
		FixedAssetOptions:  fixedAssetOptions,
		FixedAssetOpenings: fixedAssetOpenings,
		AutoBackup:         autoBackupConfig,
		DashboardConfig:    dashboardConfig,
		Message:            r.URL.Query().Get("msg"),
		MessageTone:        alertTone(r.URL.Query().Get("msg")),
	}
	RenderTemplate(w, "setup", data)
}

// SaveFixedAssetOpening captures both cost and accumulated depreciation at the start of a
// reporting year. A single net asset balance cannot produce Note 21's two roll-forwards,
// so these values are stored separately and current additions plus the calculated charge
// are applied on top of them.
func SaveFixedAssetOpening(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		badRequest(w, "Invalid form submission")
		return
	}
	year, err := strconv.Atoi(strings.TrimSpace(r.FormValue("year")))
	if err != nil {
		badRequest(w, "Invalid year")
		return
	}
	categoryID, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("category_id")), 10, 64)
	if err != nil || categoryID <= 0 {
		badRequest(w, "Invalid fixed-asset class")
		return
	}
	openingCost, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue("opening_cost")), 64)
	if err != nil || openingCost < 0 {
		badRequest(w, "Opening cost must be zero or greater")
		return
	}
	openingDep, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue("opening_accumulated_depreciation")), 64)
	if err != nil || openingDep < 0 || openingDep > openingCost {
		badRequest(w, "Opening accumulated depreciation must be between zero and opening cost")
		return
	}
	var valid int
	if err := db.DB.QueryRow(`
		SELECT COUNT(*)
		FROM categories c JOIN categories parent ON parent.id = c.parent_id
		WHERE c.id = ? AND c.type = 'asset' AND parent.name IN ('Property, Plant & Equipment', 'Intangible Assets')
	`, categoryID).Scan(&valid); err != nil {
		serverError(w, err)
		return
	}
	if valid == 0 {
		badRequest(w, "Selected account is not a fixed or intangible asset class")
		return
	}
	if _, err := db.DB.Exec(`
		INSERT INTO fixed_asset_openings (year, category_id, opening_cost, opening_accumulated_depreciation, updated_at)
		VALUES (?, ?, ?, ?, datetime('now','localtime'))
		ON CONFLICT(year, category_id) DO UPDATE SET
			opening_cost = excluded.opening_cost,
			opening_accumulated_depreciation = excluded.opening_accumulated_depreciation,
			updated_at = datetime('now','localtime')
	`, year, categoryID, openingCost, openingDep); err != nil {
		serverError(w, err)
		return
	}
	http.Redirect(w, r, "/setup?msg=Fixed-asset+opening+saved", http.StatusSeeOther)
}

// SaveAccountOpeningBalance records the brought-forward position of any asset or liability
// account. This replaces the previous cash-only limitation and lets the statement of
// financial position begin from independently confirmed year-opening balances for every
// workbook note from 21 through 28.
func SaveAccountOpeningBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		badRequest(w, "Invalid form submission")
		return
	}
	year, err := strconv.Atoi(strings.TrimSpace(r.FormValue("year")))
	if err != nil {
		badRequest(w, "Invalid year")
		return
	}
	categoryID, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("category_id")), 10, 64)
	if err != nil || categoryID <= 0 {
		badRequest(w, "Invalid balance-sheet account")
		return
	}
	amount, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue("amount")), 64)
	if err != nil || amount < 0 {
		badRequest(w, "Opening balance must be zero or greater")
		return
	}
	var categoryType string
	if err := db.DB.QueryRow(`SELECT type FROM categories WHERE id = ? AND type IN ('asset', 'liability')`, categoryID).Scan(&categoryType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			badRequest(w, "Opening balances require an asset or liability account")
			return
		}
		serverError(w, err)
		return
	}
	if _, err := db.DB.Exec(`
		INSERT INTO account_opening_balances (year, category_id, amount, updated_at)
		VALUES (?, ?, ?, datetime('now','localtime'))
		ON CONFLICT(year, category_id) DO UPDATE SET amount = excluded.amount, updated_at = datetime('now','localtime')
	`, year, categoryID, amount); err != nil {
		serverError(w, err)
		return
	}
	http.Redirect(w, r, "/setup?msg=Account+opening+balance+saved", http.StatusSeeOther)
}

// SaveFundRollforward stores the opening accumulated fund and any prior-year correction
// for a reporting year. These values are independent of current transactions; the reports
// add the calculated annual surplus to them and then compare the resulting closing fund
// with net assets, exposing incomplete balance-sheet postings instead of hiding them.
func SaveFundRollforward(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		badRequest(w, "Invalid form submission")
		return
	}

	year, err := strconv.Atoi(strings.TrimSpace(r.FormValue("year")))
	if err != nil {
		badRequest(w, "Invalid year")
		return
	}
	openingBalance, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue("opening_balance")), 64)
	if err != nil {
		badRequest(w, "Invalid opening accumulated fund")
		return
	}
	priorAdjustment, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue("prior_year_adjustment")), 64)
	if err != nil {
		badRequest(w, "Invalid prior-year adjustment")
		return
	}

	if _, err := db.DB.Exec(`
		INSERT INTO fund_rollforwards (year, opening_balance, prior_year_adjustment, updated_at)
		VALUES (?, ?, ?, datetime('now','localtime'))
		ON CONFLICT(year) DO UPDATE SET
			opening_balance = excluded.opening_balance,
			prior_year_adjustment = excluded.prior_year_adjustment,
			updated_at = datetime('now','localtime')
	`, year, openingBalance, priorAdjustment); err != nil {
		serverError(w, err)
		return
	}

	http.Redirect(w, r, "/setup?msg=Accumulated+fund+roll-forward+saved", http.StatusSeeOther)
}

// SaveBudget handles POST requests to create or update a budget entry for a specific year
// and top-level category. It validates that the year is a valid integer, the category ID
// refers to an existing top-level income or expenditure category (budgets at the
// subcategory level are not supported—the annual report rolls up comparisons at the
// parent level, and allowing leaf-level budgets would create a mismatch between the
// budget line and the aggregated actuals), and the amount is a non-negative float. The
// upsert (INSERT ... ON CONFLICT DO UPDATE) ensures that re-saving a budget for the same
// year and category overwrites the previous value rather than creating a duplicate row,
// making the form safe for repeated submissions.
func SaveBudget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		badRequest(w, "Invalid form submission")
		return
	}

	year, err := strconv.Atoi(strings.TrimSpace(r.FormValue("year")))
	if err != nil {
		badRequest(w, "Invalid year")
		return
	}

	categoryID, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("category_id")), 10, 64)
	if err != nil || categoryID <= 0 {
		badRequest(w, "Invalid category")
		return
	}

	amount, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue("amount")), 64)
	if err != nil || amount < 0 {
		badRequest(w, "Budget amount must be zero or greater")
		return
	}

	// I restrict budgets to top-level income and expenditure categories because the annual report
	// rolls budget comparisons up at that level. Allowing leaf budgets here would create mismatch.
	// Verify that the category exists, is a top-level category (parent_id=0), and is of a
	// budgetable type (income or expenditure). Asset and liability categories are excluded
	// because the budget comparison feature is designed for income and expenditure tracking.
	var categoryType string
	err = db.DB.QueryRow(
		`SELECT type FROM categories WHERE id=? AND parent_id=0 AND type IN ('income', 'expenditure')`,
		categoryID,
	).Scan(&categoryType)
	if errors.Is(err, sql.ErrNoRows) {
		badRequest(w, "Budget categories must be top-level income or expenditure categories")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}

	// The budgets table has a UNIQUE constraint on (year, category_id), and the upsert
	// syntax handles both initial creation and subsequent updates in one atomic
	// statement. This avoids a read-then-write race condition where two rapid saves
	// could produce a duplicate key error.
	_, err = db.DB.Exec(`
		INSERT INTO budgets (year, category_id, amount)
		VALUES (?, ?, ?)
		ON CONFLICT(year, category_id) DO UPDATE SET amount = excluded.amount
	`, year, categoryID, amount)
	if err != nil {
		serverError(w, err)
		return
	}

	http.Redirect(w, r, "/setup?msg=Budget+saved", http.StatusSeeOther)
}

// DeleteBudget handles POST requests to remove a budget entry by its primary key ID. The
// budget ID comes from a hidden form field in the setup page's budget table. If the ID is
// invalid or the row has already been deleted (by another request or browser tab), the
// DELETE is a no-op (zero rows affected, but no error) and the user is redirected back
// with a success message. There is no additional ownership or authorisation check because
// this is a single-user local application where all data belongs to the same user.
func DeleteBudget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		badRequest(w, "Invalid form submission")
		return
	}

	budgetID, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("id")), 10, 64)
	if err != nil || budgetID <= 0 {
		badRequest(w, "Invalid budget")
		return
	}

	if _, err := db.DB.Exec("DELETE FROM budgets WHERE id=?", budgetID); err != nil {
		serverError(w, err)
		return
	}

	http.Redirect(w, r, "/setup?msg=Budget+deleted", http.StatusSeeOther)
}

// SaveOpeningBalance handles POST requests to create or update an opening balance for a
// specific year and account type. It validates that the year is a valid integer, the
// account type is one of the three recognised liquid account identifiers ("bank", "cash",
// or "momo"), and the amount is a non-negative float. Opening balances represent the
// carried-forward cash position at the start of the fiscal year and are added to in-year
// transaction movements when computing the current balances displayed on the dashboard and
// balance sheet. Like budgets, the upsert handles both creation and updates atomically,
// and the constraint on (year, account_type) ensures only one balance exists per account
// per year.
func SaveOpeningBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		badRequest(w, "Invalid form submission")
		return
	}

	year, err := strconv.Atoi(strings.TrimSpace(r.FormValue("year")))
	if err != nil {
		badRequest(w, "Invalid year")
		return
	}

	// I only allow the liquid account types here because opening balances in this app are meant to
	// represent carried-forward cash positions, not arbitrary asset or liability seed values.
	// The three account types correspond to the liquid asset categories used in the balance
	// sheet and dashboard: Bank, Cash, and Momo (mobile money). These are the only accounts
	// that combine an opening balance with in-year transaction movements.
	accountType := strings.TrimSpace(r.FormValue("account_type"))
	switch accountType {
	case "bank", "cash", "momo":
	default:
		badRequest(w, "Invalid account type")
		return
	}

	amount, err := strconv.ParseFloat(strings.TrimSpace(r.FormValue("amount")), 64)
	if err != nil || amount < 0 {
		badRequest(w, "Opening balance must be zero or greater")
		return
	}

	// The opening_balances table has a UNIQUE constraint on (year, account_type), and
	// the upsert syntax handles both initial configuration and subsequent adjustments
	// in one atomic statement. This ensures that re-saving the opening balance for the
	// same year and account type overwrites the previous value rather than creating a
	// duplicate.
	_, err = db.DB.Exec(`
		INSERT INTO opening_balances (year, account_type, amount)
		VALUES (?, ?, ?)
		ON CONFLICT(year, account_type) DO UPDATE SET amount = excluded.amount
	`, year, accountType, amount)
	if err != nil {
		serverError(w, err)
		return
	}

	http.Redirect(w, r, "/setup?msg=Opening+balance+saved", http.StatusSeeOther)
}

// SaveDashboardGreetingSettings persists the dashboard-facing greeting configuration. I keep
// this separate from the rest of Setup writes because it owns both operator identity
// presentation (display name) and greeting rotation behavior, which are unrelated to budgets
// or opening balances even though they share the same settings page.
func SaveDashboardGreetingSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		badRequest(w, "Invalid form submission")
		return
	}

	displayName := strings.TrimSpace(r.FormValue("display_name"))
	rotationTime := strings.TrimSpace(r.FormValue("rotation_time"))
	enabled := strings.TrimSpace(r.FormValue("enabled")) != ""
	if rotationTime == "" {
		rotationTime = defaultGreetingRotationTime
	}
	if !isValidGreetingRotationTime(rotationTime) {
		badRequest(w, "Greeting rotation time must use the 24-hour HH:MM format")
		return
	}

	if err := db.SetSetting(dashboardDisplayNameSettingKey, displayName); err != nil {
		serverError(w, err)
		return
	}
	if err := db.SetSetting(dashboardGreetingEnabledSettingKey, boolSetting(enabled)); err != nil {
		serverError(w, err)
		return
	}
	if err := db.SetSetting(dashboardGreetingTimeSettingKey, rotationTime); err != nil {
		serverError(w, err)
		return
	}

	http.Redirect(w, r, "/setup?msg=Dashboard+greeting+settings+saved", http.StatusSeeOther)
}

func boolSetting(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
