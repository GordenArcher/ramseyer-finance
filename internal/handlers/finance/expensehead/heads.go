package expensehead

import (
	"database/sql"
	"errors"
	"fmt"
	"ramseyer-finance/internal/db"
	expenseheadparser "ramseyer-finance/internal/finance/dataentry/expense/head"
)

// Ensure resolves one normalised expense head to an expenditure account. When the chart
// does not carry the name, the account is created under Other Expenditure. The boolean
// reports whether this call actually added the account, so the import report can name the
// heads the upload introduced instead of listing every row that used them.
func Ensure(name string) (int64, bool, error) {
	name = expenseheadparser.Normalize(name)
	if name == "" {
		return 0, false, fmt.Errorf("expense head is empty")
	}

	// Reuse any existing account with the same name, case-insensitively. The chart already
	// carries names like "Stationery" while the office sheet writes "STATIONERY", and both
	// must resolve to the same row. A top-level account wins over a same-named child.
	var existingID int64
	err := db.DB.QueryRow(`
		SELECT id
		FROM categories
		WHERE type = 'expenditure' AND lower(trim(name)) = lower(trim(?))
		ORDER BY CASE WHEN parent_id = 0 THEN 0 ELSE 1 END, id
		LIMIT 1
	`, name).Scan(&existingID)
	if err == nil {
		return existingID, false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, fmt.Errorf("look up expense head %q: %w", name, err)
	}

	parentID, err := ensureParent()
	if err != nil {
		return 0, false, err
	}

	result, err := db.DB.Exec(`
		INSERT OR IGNORE INTO categories (type, name, parent_id, note_ref, report_section, is_active)
		VALUES ('expenditure', ?, ?, ?, '', 1)
	`, name, parentID, expenseheadparser.ParentNote)
	if err != nil {
		return 0, false, fmt.Errorf("create expense head %q: %w", name, err)
	}

	var id int64
	if err := db.DB.QueryRow(
		`SELECT id FROM categories WHERE type = 'expenditure' AND name = ? AND parent_id = ?`,
		name,
		parentID,
	).Scan(&id); err != nil {
		return 0, false, fmt.Errorf("reload expense head %q: %w", name, err)
	}

	// A concurrent upload may have inserted the same head between the lookup above and this
	// insert. INSERT OR IGNORE keeps the row unique, and RowsAffected tells the report
	// whether this request is the one that created it.
	affected, err := result.RowsAffected()
	if err != nil {
		return id, false, nil
	}
	return id, affected > 0, nil
}

// ensureParent returns the chart account that groups imported expense heads. It is created
// on first use, so a fresh install and an existing installation take the same path without
// a schema migration or a new seed entry.
func ensureParent() (int64, error) {
	var id int64
	err := db.DB.QueryRow(
		`SELECT id FROM categories WHERE type = 'expenditure' AND name = ? AND parent_id = 0`,
		expenseheadparser.ParentName,
	).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("look up expense head parent: %w", err)
	}

	if _, err := db.DB.Exec(`
		INSERT OR IGNORE INTO categories (type, name, parent_id, note_ref, report_section, is_active)
		VALUES ('expenditure', ?, 0, ?, '', 1)
	`, expenseheadparser.ParentName, expenseheadparser.ParentNote); err != nil {
		return 0, fmt.Errorf("create expense head parent: %w", err)
	}

	if err := db.DB.QueryRow(
		`SELECT id FROM categories WHERE type = 'expenditure' AND name = ? AND parent_id = 0`,
		expenseheadparser.ParentName,
	).Scan(&id); err != nil {
		return 0, fmt.Errorf("reload expense head parent: %w", err)
	}
	return id, nil
}
