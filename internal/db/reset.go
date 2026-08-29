package db

import "fmt"

// ResetFinancialRecords removes the accounting activity that belongs to the current body
// of financial work while deliberately preserving authentication, application settings,
// the standard chart of accounts, dashboard preferences, and backup history. This gives
// the operator a genuinely empty financial record without making them configure a new PIN
// or losing the recovery snapshot that was created immediately before the reset.
func ResetFinancialRecords() error {
	if DB == nil {
		return fmt.Errorf("database is not initialized")
	}

	// One transaction makes the reset all-or-nothing. A partial clear would be worse than no
	// clear because reports could combine old Trial Balance rows with missing transactions or
	// opening balances, so any failure rolls the complete operation back.
	tx, err := DB.Begin()
	if err != nil {
		return fmt.Errorf("begin financial record reset: %w", err)
	}
	defer tx.Rollback()

	// Child and audit tables are cleared before their parents so this remains correct even
	// if a restored older database has stricter foreign-key behavior than the current schema.
	financialTables := []string{
		"transaction_audit_log",
		"trial_balance_entries",
		"trial_balance_years",
		"budgets",
		"opening_balances",
		"fund_rollforwards",
		"account_opening_balances",
		"fixed_asset_openings",
		"transactions",
	}
	for _, table := range financialTables {
		if _, err := tx.Exec("DELETE FROM " + table); err != nil {
			return fmt.Errorf("clear %s: %w", table, err)
		}
	}

	// Resetting these counters makes the first records in the fresh ledger start from a
	// predictable identity without changing category IDs, which transactions and setup rows
	// continue to reference after the standard chart is preserved.
	if _, err := tx.Exec(`
		DELETE FROM sqlite_sequence
		WHERE name IN ('transactions', 'transaction_audit_log', 'budgets', 'opening_balances', 'trial_balance_entries')
	`); err != nil {
		return fmt.Errorf("reset financial record counters: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit financial record reset: %w", err)
	}
	return nil
}
