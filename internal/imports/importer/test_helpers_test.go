package importer

import (
	"path/filepath"
	"ramseyer-finance/internal/db"
	"testing"
)

func setupTestDB(t *testing.T) {
	t.Helper()

	if err := db.Initialize(filepath.Join(t.TempDir(), "test.db")); err != nil {
		t.Fatalf("initialize test database: %v", err)
	}
	t.Cleanup(db.Close)
}

func incomeCategory(t *testing.T, name string) CategoryOption {
	t.Helper()

	categories, err := ActiveIncomeCategories(db.DB)
	if err != nil {
		t.Fatalf("load income categories: %v", err)
	}
	for _, category := range categories {
		if category.Name == name {
			return category
		}
	}
	t.Fatalf("income category %q not found", name)
	return CategoryOption{}
}
