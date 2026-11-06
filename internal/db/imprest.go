package db

import (
	"fmt"
)

// defaultImprestCategories are the heads the client's imprest cash book already uses. They
// are stored exactly as written in that workbook so an uploaded row matches without a
// translation table. Keeping our own list instead of borrowing the expenditure chart is
// deliberate: the imprest book is a separate record with its own vocabulary, and mapping
// these short heads onto the chart of accounts would force a guess on every upload.
var defaultImprestCategories = []string{
	"T&T",
	"REFRESHMENT",
	"CLEANING",
	"REPAIRS & MTCE",
	"STATIONERY",
	"OTHERS",
	"FUEL",
	"WELFARE",
	"SSNIT",
	"DATA",
	"SECURITY",
}

// seedImprestCategories creates the imprest book's head list on startup. INSERT OR IGNORE
// keeps operator-added heads and any rename intact, so this only fills the gaps a fresh or
// upgraded database is missing.
func seedImprestCategories() error {
	for _, name := range defaultImprestCategories {
		if _, err := DB.Exec(
			"INSERT OR IGNORE INTO imprest_categories (name, is_active) VALUES (?, 1)",
			name,
		); err != nil {
			return fmt.Errorf("seed imprest category %s: %w", name, err)
		}
	}
	return nil
}
