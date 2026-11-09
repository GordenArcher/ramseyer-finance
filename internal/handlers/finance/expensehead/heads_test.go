package expensehead

import (
	"path/filepath"
	"testing"

	"ramseyer-finance/internal/db"
)

func TestEnsureCreatesAHeadOnceUnderOtherExpenditure(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "expense-head.db")
	if err := db.Initialize(databasePath); err != nil {
		t.Fatalf("initialize test database: %v", err)
	}
	t.Cleanup(db.Close)

	id, created, err := Ensure("OFFICE NEW HEAD")
	if err != nil {
		t.Fatalf("create expense head: %v", err)
	}
	if !created {
		t.Fatalf("first call reported created = false")
	}

	secondID, createdAgain, err := Ensure("office new head")
	if err != nil {
		t.Fatalf("reuse expense head: %v", err)
	}
	if createdAgain {
		t.Fatalf("second call created the head again")
	}
	if secondID != id {
		t.Fatalf("second call returned id %d, want %d", secondID, id)
	}

	var (
		parentID int64
		noteRef  string
	)
	if err := db.DB.QueryRow(
		`SELECT parent_id, note_ref FROM categories WHERE id = ?`,
		id,
	).Scan(&parentID, &noteRef); err != nil {
		t.Fatalf("load created head: %v", err)
	}
	var parentName string
	if err := db.DB.QueryRow(
		`SELECT name FROM categories WHERE id = ?`,
		parentID,
	).Scan(&parentName); err != nil {
		t.Fatalf("load head parent: %v", err)
	}
	if parentName != "Other Expenditure" || noteRef != "20" {
		t.Fatalf("head parent/note = %q/%q, want Other Expenditure/20", parentName, noteRef)
	}
}
