package finance

import (
	"fmt"
	"ramseyer-finance/internal/db"
	"strings"
)

// ImprestCategory is one head in the imprest book. The list is deliberately separate from
// the expenditure chart: the cash book uses short names like "T&T" that mean nothing to the
// main chart, and inventing an account for each one would make the two books disagree.
type ImprestCategory struct {
	ID   int64
	Name string
}

// ImprestEntry is one saved imprest payment shown on the imprest page.
type ImprestEntry struct {
	ID          int64
	Date        string
	Category    string
	Description string
	Amount      float64
}

// loadImprestCategories reads the active imprest heads in seed order so the dropdown and
// the upload matcher see the same list.
func loadImprestCategories() ([]ImprestCategory, error) {
	rows, err := db.DB.Query(
		`SELECT id, name FROM imprest_categories WHERE COALESCE(is_active, 1) = 1 ORDER BY id`,
	)
	if err != nil {
		return nil, fmt.Errorf("load imprest categories: %w", err)
	}
	defer rows.Close()

	var categories []ImprestCategory
	for rows.Next() {
		var category ImprestCategory
		if err := rows.Scan(&category.ID, &category.Name); err != nil {
			return nil, fmt.Errorf("scan imprest category: %w", err)
		}
		categories = append(categories, category)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate imprest categories: %w", err)
	}
	return categories, nil
}

// matchImprestCategory resolves an uploaded DETAILS label against the imprest heads. The
// match is case-insensitive and exact only. Fuzzy matching would hide a misspelled new head
// behind an existing one, and the operator would never learn the spelling changed.
func matchImprestCategory(categories []ImprestCategory, label string) (ImprestCategory, bool) {
	needle := strings.ToLower(strings.TrimSpace(label))
	if needle == "" {
		return ImprestCategory{}, false
	}
	for _, category := range categories {
		if strings.ToLower(strings.TrimSpace(category.Name)) == needle {
			return category, true
		}
	}
	return ImprestCategory{}, false
}

// lookupImprestCategory resolves a category chosen in the manual form by its database ID.
func lookupImprestCategory(categoryID int64) (ImprestCategory, error) {
	var category ImprestCategory
	err := db.DB.QueryRow(
		`SELECT id, name FROM imprest_categories WHERE id = ? AND COALESCE(is_active, 1) = 1`,
		categoryID,
	).Scan(&category.ID, &category.Name)
	if err != nil {
		return ImprestCategory{}, err
	}
	return category, nil
}

// loadImprestTotal sums the imprest book for one date range. It is kept apart from
// loadIncomeExpenseTotals because imprest is not part of the main ledger; the two totals
// are added only when the page or statement needs the overall expenditure figure.
func loadImprestTotal(startDate, endDate string) (float64, error) {
	var total float64
	err := db.DB.QueryRow(
		`SELECT COALESCE(SUM(amount), 0) FROM imprest_expenses WHERE date >= ? AND date < ?`,
		startDate,
		endDate,
	).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("load imprest total: %w", err)
	}
	return total, nil
}

// loadMonthlyImprestTotals returns one total per calendar month so the monthly and
// quarterly reports can show imprest alongside the main expenditure without a second query
// per month.
func loadMonthlyImprestTotals(year int) (map[int]float64, error) {
	startDate, endDate := yearBounds(year)
	rows, err := db.DB.Query(`
		SELECT CAST(strftime('%m', date) AS INTEGER) AS month_number, COALESCE(SUM(amount), 0)
		FROM imprest_expenses
		WHERE date >= ? AND date < ?
		GROUP BY month_number
	`, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("load monthly imprest totals: %w", err)
	}
	defer rows.Close()

	totals := map[int]float64{}
	for rows.Next() {
		var (
			monthNumber int
			total       float64
		)
		if err := rows.Scan(&monthNumber, &total); err != nil {
			return nil, fmt.Errorf("scan monthly imprest total: %w", err)
		}
		totals[monthNumber] = total
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate monthly imprest totals: %w", err)
	}
	return totals, nil
}

// loadImprestEntries lists the selected year's imprest payments newest first. The page is a
// working list, not a report, so a bounded result is enough and keeps the render cheap.
func loadImprestEntries(year, limit int) ([]ImprestEntry, error) {
	startDate, endDate := yearBounds(year)
	rows, err := db.DB.Query(`
		SELECT id, date, category, description, amount
		FROM imprest_expenses
		WHERE date >= ? AND date < ?
		ORDER BY date DESC, id DESC
		LIMIT ?
	`, startDate, endDate, limit)
	if err != nil {
		return nil, fmt.Errorf("load imprest entries: %w", err)
	}
	defer rows.Close()

	var entries []ImprestEntry
	for rows.Next() {
		var entry ImprestEntry
		if err := rows.Scan(&entry.ID, &entry.Date, &entry.Category, &entry.Description, &entry.Amount); err != nil {
			return nil, fmt.Errorf("scan imprest entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate imprest entries: %w", err)
	}
	return entries, nil
}

// insertImprestExpense stores one imprest payment. The category name is denormalised so a
// later rename cannot rewrite history, matching how the main transactions table keeps the
// account name it posted under.
func insertImprestExpense(date string, category ImprestCategory, description string, amount float64) error {
	_, err := db.DB.Exec(
		`INSERT INTO imprest_expenses (date, category_id, category, description, amount, updated_at)
		VALUES (?, ?, ?, ?, ?, datetime('now','localtime'))`,
		date,
		category.ID,
		category.Name,
		description,
		amount,
	)
	if err != nil {
		return fmt.Errorf("insert imprest expense: %w", err)
	}
	return nil
}
