package db

import (
	"path/filepath"
	"testing"
)

func TestInitializeBackfillsLegacyTransactionCategoryIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	if err := Initialize(path); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	defer Close()

	if _, err := DB.Exec(
		`INSERT INTO transactions (date, type, category, description, amount)
		VALUES ('2026-01-01', 'income', 'Offering', '', 42)`,
	); err != nil {
		t.Fatalf("insert legacy transaction: %v", err)
	}

	if err := backfillTransactionCategoryIDs(); err != nil {
		t.Fatalf("backfill transaction category IDs: %v", err)
	}

	var categoryID int64
	if err := DB.QueryRow(
		`SELECT COALESCE(category_id, 0) FROM transactions WHERE category='Offering'`,
	).Scan(&categoryID); err != nil {
		t.Fatalf("query backfilled transaction: %v", err)
	}
	if categoryID == 0 {
		t.Fatalf("category ID was not backfilled")
	}
}

func TestSyncTransactionCategoryMetadataBackfillsNoteRef(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	if err := Initialize(path); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	defer Close()

	var offeringID int64
	if err := DB.QueryRow(`SELECT id FROM categories WHERE type='income' AND name='Offering'`).Scan(&offeringID); err != nil {
		t.Fatalf("lookup offering category: %v", err)
	}

	if _, err := DB.Exec(
		`INSERT INTO transactions (date, type, category, category_id, note_ref, description, amount)
		VALUES ('2026-01-01', 'income', 'Legacy Offering', ?, '', '', 42)`,
		offeringID,
	); err != nil {
		t.Fatalf("insert transaction with stale metadata: %v", err)
	}

	if err := syncTransactionCategoryMetadata(); err != nil {
		t.Fatalf("sync transaction metadata: %v", err)
	}

	var (
		categoryName string
		noteRef      string
	)
	if err := DB.QueryRow(
		`SELECT category, note_ref FROM transactions WHERE category_id=?`,
		offeringID,
	).Scan(&categoryName, &noteRef); err != nil {
		t.Fatalf("query synced transaction: %v", err)
	}
	if categoryName != "Offering" {
		t.Fatalf("category = %q, want Offering", categoryName)
	}
	if noteRef != "1" {
		t.Fatalf("note_ref = %q, want 1", noteRef)
	}
}
