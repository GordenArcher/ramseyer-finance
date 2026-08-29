package db

import (
	"path/filepath"
	"testing"
)

func TestResetFinancialRecordsClearsLedgerAndPreservesAppState(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "reset.db")
	if err := Initialize(databasePath); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	defer Close()

	var categoryID int64
	if err := DB.QueryRow(`SELECT id FROM categories WHERE type = 'income' ORDER BY id LIMIT 1`).Scan(&categoryID); err != nil {
		t.Fatalf("load seeded category: %v", err)
	}
	transactionResult, err := DB.Exec(`
		INSERT INTO transactions (date, type, category, category_id, amount)
		VALUES ('2026-01-01', 'income', 'Test income', ?, 100)
	`, categoryID)
	if err != nil {
		t.Fatalf("insert transaction: %v", err)
	}
	transactionID, _ := transactionResult.LastInsertId()

	statements := []struct {
		name string
		sql  string
		args []any
	}{
		{"audit log", `INSERT INTO transaction_audit_log (transaction_id, action, snapshot_json) VALUES (?, 'created', '{}')`, []any{transactionID}},
		{"budget", `INSERT INTO budgets (year, category_id, amount) VALUES (2026, ?, 100)`, []any{categoryID}},
		{"opening balance", `INSERT INTO opening_balances (year, account_type, amount) VALUES (2026, 'bank', 50)`, nil},
		{"fund roll-forward", `INSERT INTO fund_rollforwards (year, opening_balance) VALUES (2026, 50)`, nil},
		{"account opening", `INSERT INTO account_opening_balances (year, category_id, amount) VALUES (2026, ?, 50)`, []any{categoryID}},
		{"fixed-asset opening", `INSERT INTO fixed_asset_openings (year, category_id, opening_cost) VALUES (2026, ?, 50)`, []any{categoryID}},
		{"Trial Balance year", `INSERT INTO trial_balance_years (year) VALUES (2026)`, nil},
		{"Trial Balance entry", `INSERT INTO trial_balance_entries (year, account_type, account_name, debit) VALUES (2026, 'asset', 'Test asset', 50)`, nil},
		{"auth setting", `INSERT OR REPLACE INTO settings (key, value) VALUES ('auth.pin_hash', 'keep-me')`, nil},
		{"backup history", `INSERT INTO backup_events (kind, status, file_path, note) VALUES ('fresh-start-recovery', 'success', '/tmp/recovery.db', 'keep-me')`, nil},
	}
	for _, statement := range statements {
		if _, err := DB.Exec(statement.sql, statement.args...); err != nil {
			t.Fatalf("insert %s: %v", statement.name, err)
		}
	}

	if err := ResetFinancialRecords(); err != nil {
		t.Fatalf("reset financial records: %v", err)
	}

	for _, table := range []string{
		"transactions",
		"transaction_audit_log",
		"budgets",
		"opening_balances",
		"fund_rollforwards",
		"account_opening_balances",
		"fixed_asset_openings",
		"trial_balance_years",
		"trial_balance_entries",
	} {
		var count int
		if err := DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d, want 0", table, count)
		}
	}

	for _, preserved := range []struct {
		name  string
		query string
	}{
		{"categories", `SELECT COUNT(*) FROM categories`},
		{"PIN setting", `SELECT COUNT(*) FROM settings WHERE key = 'auth.pin_hash' AND value = 'keep-me'`},
		{"backup history", `SELECT COUNT(*) FROM backup_events WHERE note = 'keep-me'`},
	} {
		var count int
		if err := DB.QueryRow(preserved.query).Scan(&count); err != nil {
			t.Fatalf("count preserved %s: %v", preserved.name, err)
		}
		if count == 0 {
			t.Fatalf("%s was not preserved", preserved.name)
		}
	}
}
