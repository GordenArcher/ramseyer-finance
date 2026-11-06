package finance

import (
	"database/sql"
	"fmt"
	"ramseyer-finance/internal/db"
	"strings"
)

// ImprestCategory is one head in the imprest book. The list is deliberately separate from
// the expenditure chart: the cash book uses short names like "T&T" that mean nothing to the
// main chart, and inventing an account for each one would make the two books disagree.
type ImprestCategory struct {
	ID         int64
	Name       string
	EntryCount int
}

// ImprestEntry is one saved imprest payment shown on the imprest page.
type ImprestEntry struct {
	ID          int64
	Date        string
	Category    string
	Description string
	Amount      float64
}

// EditableImprestEntry holds the current values of an imprest payment being edited. Loaded
// distinguishes an intentionally empty form from a failed lookup so the template only opens
// the edit modal when there is a real row to change.
type EditableImprestEntry struct {
	Loaded      bool
	ID          int64
	Date        string
	CategoryID  int64
	Description string
	Amount      float64
}

// loadImprestCategories reads the active imprest heads in seed order so the dropdown and
// the upload matcher see the same list. EntryCount is loaded with the list rather than per
// row so the head manager can show usage without a query for every head.
func loadImprestCategories() ([]ImprestCategory, error) {
	rows, err := db.DB.Query(`
		SELECT c.id, c.name, COUNT(e.id)
		FROM imprest_categories c
		LEFT JOIN imprest_expenses e ON e.category_id = c.id
		WHERE COALESCE(c.is_active, 1) = 1
		GROUP BY c.id, c.name
		ORDER BY c.id
	`)
	if err != nil {
		return nil, fmt.Errorf("load imprest categories: %w", err)
	}
	defer rows.Close()

	var categories []ImprestCategory
	for rows.Next() {
		var category ImprestCategory
		if err := rows.Scan(&category.ID, &category.Name, &category.EntryCount); err != nil {
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

// loadImprestEntries lists one page of the selected year's imprest payments newest first,
// plus the total row count so the caller can build pagination links. Paging in SQL keeps the
// page cheap even when the cash book holds hundreds of payments for the year.
func loadImprestEntries(year, page, pageSize int) ([]ImprestEntry, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 1
	}

	startDate, endDate := yearBounds(year)
	var total int
	if err := db.DB.QueryRow(
		`SELECT COUNT(*) FROM imprest_expenses WHERE date >= ? AND date < ?`,
		startDate,
		endDate,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count imprest entries: %w", err)
	}

	rows, err := db.DB.Query(`
		SELECT id, date, category, description, amount
		FROM imprest_expenses
		WHERE date >= ? AND date < ?
		ORDER BY date DESC, id DESC
		LIMIT ? OFFSET ?
	`, startDate, endDate, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("load imprest entries: %w", err)
	}
	defer rows.Close()

	var entries []ImprestEntry
	for rows.Next() {
		var entry ImprestEntry
		if err := rows.Scan(&entry.ID, &entry.Date, &entry.Category, &entry.Description, &entry.Amount); err != nil {
			return nil, 0, fmt.Errorf("scan imprest entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate imprest entries: %w", err)
	}
	return entries, total, nil
}

// loadEditableImprestEntry retrieves one payment for the edit modal. It returns only the
// editable fields; the denormalised category name is refreshed from the selected head on
// save so a rename and its saved payments cannot drift apart.
func loadEditableImprestEntry(entryID int64) (EditableImprestEntry, error) {
	var entry EditableImprestEntry
	err := db.DB.QueryRow(`
		SELECT id, date, category_id, COALESCE(description, ''), amount
		FROM imprest_expenses
		WHERE id = ?
	`, entryID).Scan(&entry.ID, &entry.Date, &entry.CategoryID, &entry.Description, &entry.Amount)
	if err != nil {
		return EditableImprestEntry{}, err
	}
	entry.Loaded = true
	return entry, nil
}

// insertImprestExpense stores one imprest payment. The category name is denormalised so a
// later rename can update history in one place, matching how the main transactions table
// keeps the account name it posted under.
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

// updateImprestExpense changes one saved payment. Both category_id and the denormalised
// category name are written together so the ledger, head list, and usage counts agree.
func updateImprestExpense(entryID int64, date string, category ImprestCategory, description string, amount float64) error {
	result, err := db.DB.Exec(`
		UPDATE imprest_expenses
		SET date = ?, category_id = ?, category = ?, description = ?, amount = ?,
			updated_at = datetime('now','localtime')
		WHERE id = ?
	`, date, category.ID, category.Name, description, amount, entryID)
	if err != nil {
		return fmt.Errorf("update imprest expense: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check updated imprest expense: %w", err)
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// deleteImprestExpense removes one saved payment.
func deleteImprestExpense(entryID int64) error {
	result, err := db.DB.Exec(`DELETE FROM imprest_expenses WHERE id = ?`, entryID)
	if err != nil {
		return fmt.Errorf("delete imprest expense: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check deleted imprest expense: %w", err)
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// imprestCategoryNameTaken reports whether another head already uses the same name. The
// comparison ignores case and surrounding space because upload matching is case-insensitive,
// so two heads that differ only by case would make every upload ambiguous.
func imprestCategoryNameTaken(name string, exceptID int64) (bool, error) {
	var count int
	err := db.DB.QueryRow(`
		SELECT COUNT(*)
		FROM imprest_categories
		WHERE LOWER(TRIM(name)) = LOWER(?) AND id <> ?
	`, strings.TrimSpace(name), exceptID).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check imprest category name: %w", err)
	}
	return count > 0, nil
}

// createImprestCategory adds an operator-defined head beyond the workbook's original list.
func createImprestCategory(name string) error {
	if _, err := db.DB.Exec(
		`INSERT INTO imprest_categories (name, is_active) VALUES (?, 1)`,
		strings.TrimSpace(name),
	); err != nil {
		return fmt.Errorf("create imprest category: %w", err)
	}
	return nil
}

// renameImprestCategory renames a head and rewrites the denormalised category name on every
// saved payment that used it. Both writes share one transaction so history never points at
// a head name the head list no longer contains.
func renameImprestCategory(categoryID int64, name string) error {
	tx, err := db.DB.Begin()
	if err != nil {
		return fmt.Errorf("begin imprest category rename: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.Exec(
		`UPDATE imprest_categories SET name = ? WHERE id = ?`,
		strings.TrimSpace(name),
		categoryID,
	)
	if err != nil {
		return fmt.Errorf("rename imprest category: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check renamed imprest category: %w", err)
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	if _, err := tx.Exec(`
		UPDATE imprest_expenses
		SET category = ?, updated_at = datetime('now','localtime')
		WHERE category_id = ?
	`, strings.TrimSpace(name), categoryID); err != nil {
		return fmt.Errorf("update imprest payment head names: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit imprest category rename: %w", err)
	}
	return nil
}

// imprestCategoryUsage counts the saved payments that still reference a head. Deletion is
// refused while this is non-zero so existing history cannot be orphaned.
func imprestCategoryUsage(categoryID int64) (int, error) {
	var count int
	if err := db.DB.QueryRow(
		`SELECT COUNT(*) FROM imprest_expenses WHERE category_id = ?`,
		categoryID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count imprest category usage: %w", err)
	}
	return count, nil
}

// deleteImprestCategory removes an unused head. The caller checks usage first and reports
// the rename alternative when payments still depend on it.
func deleteImprestCategory(categoryID int64) error {
	result, err := db.DB.Exec(`DELETE FROM imprest_categories WHERE id = ?`, categoryID)
	if err != nil {
		return fmt.Errorf("delete imprest category: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check deleted imprest category: %w", err)
	}
	if rowsAffected == 0 {
		return sql.ErrNoRows
	}
	return nil
}
