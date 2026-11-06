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

// seedImprestCategories creates the imprest book's head list on startup, but only while the
// table is empty. An operator may rename or delete a seeded head, and re-inserting the
// defaults on every launch would undo that work. The empty check keeps a fresh database
// usable without making the seed authoritative after the first run.
func seedImprestCategories() error {
	var count int
	if err := DB.QueryRow("SELECT COUNT(*) FROM imprest_categories").Scan(&count); err != nil {
		return fmt.Errorf("count imprest categories: %w", err)
	}
	if count > 0 {
		return nil
	}

	for _, name := range defaultImprestCategories {
		if _, err := DB.Exec(
			"INSERT INTO imprest_categories (name, is_active) VALUES (?, 1)",
			name,
		); err != nil {
			return fmt.Errorf("seed imprest category %s: %w", name, err)
		}
	}
	return nil
}
