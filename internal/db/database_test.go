package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestInitializeAddsTransactionClassificationColumnsBeforeCreatingIndexes(t *testing.T) {
	Close()
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacyDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	// This table mirrors the immediately preceding application schema: category_id and
	// updated_at already exists, but the visible payment classification has not been introduced
	// yet. Initialize must add it safely without reviving the retired counter-account model.
	if _, err := legacyDB.Exec(`
		CREATE TABLE transactions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			date TEXT NOT NULL,
			type TEXT NOT NULL,
			category TEXT NOT NULL,
			category_id INTEGER,
			subcategory TEXT DEFAULT '',
			description TEXT DEFAULT '',
			amount REAL NOT NULL,
			note_ref TEXT DEFAULT '',
			created_at TEXT,
			updated_at TEXT
		)
	`); err != nil {
		legacyDB.Close()
		t.Fatalf("create legacy transaction table: %v", err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	if err := Initialize(path); err != nil {
		t.Fatalf("initialize legacy database: %v", err)
	}
	t.Cleanup(Close)

	exists, err := columnExists("transactions", "counter_category_id")
	if err != nil {
		t.Fatalf("inspect migrated transaction columns: %v", err)
	}
	if exists {
		t.Fatalf("counter_category_id was added to the simplified transaction model")
	}
	paymentMethodExists, err := columnExists("transactions", "payment_method")
	if err != nil {
		t.Fatalf("inspect payment_method column: %v", err)
	}
	if !paymentMethodExists {
		t.Fatalf("payment_method was not added")
	}
}

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

func TestInitializeRemovesLegacyCounterWithoutChangingEnteredAmount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "counter-model.db")
	if err := Initialize(path); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	if _, err := DB.Exec("ALTER TABLE transactions ADD COLUMN counter_category_id INTEGER"); err != nil {
		t.Fatalf("add legacy counter column: %v", err)
	}
	var offeringID, cashID int64
	if err := DB.QueryRow("SELECT id FROM categories WHERE type = 'income' AND name = 'Adult Service Offertory'").Scan(&offeringID); err != nil {
		t.Fatalf("load offering category: %v", err)
	}
	if err := DB.QueryRow("SELECT id FROM categories WHERE type = 'asset' AND name = 'Cash on hand'").Scan(&cashID); err != nil {
		t.Fatalf("load cash category: %v", err)
	}
	if _, err := DB.Exec(`
		INSERT INTO transactions (date, type, category, category_id, counter_category_id, payment_method, amount)
		VALUES ('2026-01-04', 'income', 'Adult Service Offertory', ?, ?, 'cash', 3000)
	`, offeringID, cashID); err != nil {
		t.Fatalf("insert legacy paired transaction: %v", err)
	}
	Close()

	if err := Initialize(path); err != nil {
		t.Fatalf("reinitialize migrated database: %v", err)
	}
	t.Cleanup(Close)
	var counterID sql.NullInt64
	if err := DB.QueryRow("SELECT counter_category_id FROM transactions").Scan(&counterID); err != nil {
		t.Fatalf("load migrated counter: %v", err)
	}
	if counterID.Valid {
		t.Fatalf("legacy counter remained linked to category %d", counterID.Int64)
	}
	var postingCount int
	var postingAmount float64
	if err := DB.QueryRow("SELECT COUNT(*), COALESCE(SUM(amount), 0) FROM financial_postings").Scan(&postingCount, &postingAmount); err != nil {
		t.Fatalf("load migrated financial postings: %v", err)
	}
	if postingCount != 1 || postingAmount != 3000 {
		t.Fatalf("migrated postings = count %d amount %.2f, want one posting of 3000", postingCount, postingAmount)
	}
}

func TestSyncTransactionCategoryMetadataBackfillsNoteRef(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	if err := Initialize(path); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	defer Close()

	var offeringID int64
	if err := DB.QueryRow(`SELECT id FROM categories WHERE type='income' AND name='Offerings'`).Scan(&offeringID); err != nil {
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
	if categoryName != "Offerings" {
		t.Fatalf("category = %q, want Offerings", categoryName)
	}
	if noteRef != "4" {
		t.Fatalf("note_ref = %q, want 4", noteRef)
	}
}

func TestSeedCategoriesMigratesLegacyChildrenAndCashHierarchy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	if err := Initialize(path); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	defer Close()

	offeringResult, err := DB.Exec(`
		INSERT INTO categories (type, name, parent_id, note_ref, report_section)
		VALUES ('income', 'Offering', 0, '1', '')
	`)
	if err != nil {
		t.Fatalf("insert legacy offering: %v", err)
	}
	offeringID, err := offeringResult.LastInsertId()
	if err != nil {
		t.Fatalf("legacy offering id: %v", err)
	}
	if _, err := DB.Exec(`
		INSERT INTO categories (type, name, parent_id, note_ref, report_section)
		VALUES ('income', 'Children Service', ?, '1', '')
	`, offeringID); err != nil {
		t.Fatalf("insert legacy offering child: %v", err)
	}
	if _, err := DB.Exec(`
		INSERT INTO categories (type, name, parent_id, note_ref, report_section)
		VALUES ('asset', 'Legacy Cash Marker', 0, '', '')
	`); err != nil {
		t.Fatalf("insert marker category: %v", err)
	}

	// Fresh initialization already supplied the child Bank account. Move it temporarily to
	// a harmless name so this test can reproduce the old top-level Bank row, then let the
	// migration reconcile that row with the standard Note 26 hierarchy.
	if _, err := DB.Exec(`
		UPDATE categories SET name = 'Workbook Bank Placeholder'
		WHERE type = 'asset' AND name = 'Bank' AND parent_id <> 0
	`); err != nil {
		t.Fatalf("rename standard Bank child: %v", err)
	}
	if _, err := DB.Exec(`
		INSERT INTO categories (type, name, parent_id, note_ref, report_section)
		VALUES ('asset', 'Bank', 0, '', 'current_asset')
	`); err != nil {
		t.Fatalf("insert legacy Bank category: %v", err)
	}
	if err := seedCategories(); err != nil {
		t.Fatalf("rerun category seed migration: %v", err)
	}

	var childNote string
	if err := DB.QueryRow(`
		SELECT note_ref FROM categories
		WHERE type = 'income' AND name = 'Children Service' AND parent_id = ?
	`, offeringID).Scan(&childNote); err != nil {
		t.Fatalf("query migrated offering child: %v", err)
	}
	if childNote != "4" {
		t.Fatalf("legacy child note = %q, want 4", childNote)
	}

	var bankParent string
	if err := DB.QueryRow(`
		SELECT parent.name
		FROM categories bank
		JOIN categories parent ON parent.id = bank.parent_id
		WHERE bank.type = 'asset' AND bank.name = 'Bank'
	`).Scan(&bankParent); err != nil {
		t.Fatalf("query migrated Bank parent: %v", err)
	}
	if bankParent != "Cash & Cash Equivalents" {
		t.Fatalf("Bank parent = %q, want Cash & Cash Equivalents", bankParent)
	}
}
