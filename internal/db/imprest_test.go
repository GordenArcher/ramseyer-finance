package db

import (
	"path/filepath"
	"testing"
)

func TestSeedImprestCategoriesOnlyFillsAnEmptyTable(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "imprest-seed.db")
	if err := Initialize(databasePath); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	defer Close()

	// Simulate the operator edits that must survive the next launch: one rename and one
	// deletion. Re-running the seed must not bring the original defaults back.
	if _, err := DB.Exec(`UPDATE imprest_categories SET name = 'TRANSPORT' WHERE name = 'T&T'`); err != nil {
		t.Fatalf("rename seeded imprest head: %v", err)
	}
	if _, err := DB.Exec(`DELETE FROM imprest_categories WHERE name = 'REFRESHMENT'`); err != nil {
		t.Fatalf("delete seeded imprest head: %v", err)
	}

	if err := seedImprestCategories(); err != nil {
		t.Fatalf("rerun imprest seed: %v", err)
	}

	var (
		total   int
		renamed int
		deleted int
	)
	if err := DB.QueryRow(`SELECT COUNT(*) FROM imprest_categories`).Scan(&total); err != nil {
		t.Fatalf("count imprest heads: %v", err)
	}
	if err := DB.QueryRow(`SELECT COUNT(*) FROM imprest_categories WHERE name = 'TRANSPORT'`).Scan(&renamed); err != nil {
		t.Fatalf("count renamed head: %v", err)
	}
	if err := DB.QueryRow(`SELECT COUNT(*) FROM imprest_categories WHERE name = 'REFRESHMENT'`).Scan(&deleted); err != nil {
		t.Fatalf("count deleted head: %v", err)
	}
	if total != len(defaultImprestCategories)-1 {
		t.Fatalf("imprest head count = %d, want %d after the operator edit", total, len(defaultImprestCategories)-1)
	}
	if renamed != 1 {
		t.Fatalf("renamed head count = %d, want the rename preserved", renamed)
	}
	if deleted != 0 {
		t.Fatalf("deleted head count = %d, want the deletion preserved", deleted)
	}

	// An empty table is still populated so a fresh install and a deliberate full clear both
	// leave the page usable.
	if _, err := DB.Exec(`DELETE FROM imprest_categories`); err != nil {
		t.Fatalf("clear imprest heads: %v", err)
	}
	if err := seedImprestCategories(); err != nil {
		t.Fatalf("seed emptied imprest table: %v", err)
	}
	if err := DB.QueryRow(`SELECT COUNT(*) FROM imprest_categories`).Scan(&total); err != nil {
		t.Fatalf("count reseeded imprest heads: %v", err)
	}
	if total != len(defaultImprestCategories) {
		t.Fatalf("reseeded imprest head count = %d, want %d", total, len(defaultImprestCategories))
	}
}
