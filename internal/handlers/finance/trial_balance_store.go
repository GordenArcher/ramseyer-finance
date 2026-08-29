package finance

import (
	"database/sql"
	"fmt"
	"ramseyer-finance/internal/db"
	"strconv"
)

const trialBalanceLiveSyncSettingPrefix = "trial_balance_live_sync_v1_year_"

// ensureTrialBalanceYear creates an independent book the first time a year is requested.
// Existing installations begin with a one-time transaction-derived snapshot so upgrading
// does not erase their figures. The marker table is separate from the entries table so a
// user may intentionally delete every row without the app unexpectedly reseeding it.
func ensureTrialBalanceYear(year int) error {
	var exists int
	err := db.DB.QueryRow("SELECT 1 FROM trial_balance_years WHERE year = ?", year).Scan(&exists)
	if err == nil {
		if err := backfillUnsyncedTransactionCreates(year); err != nil {
			return err
		}
		return ensureOpeningAccumulatedFundLine(year)
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("check Trial Balance year: %w", err)
	}

	seedLines, err := buildLegacyTrialBalanceSeed(year)
	if err != nil {
		return err
	}
	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec("INSERT OR IGNORE INTO trial_balance_years (year) VALUES (?)", year)
	if err != nil {
		return fmt.Errorf("initialize Trial Balance year: %w", err)
	}
	inserted, _ := result.RowsAffected()
	if inserted == 0 {
		if err := tx.Commit(); err != nil {
			return err
		}
		return ensureOpeningAccumulatedFundLine(year)
	}
	for index, line := range seedLines {
		if _, err := tx.Exec(`
			INSERT INTO trial_balance_entries
				(year, account_type, note_ref, account_name, debit, credit, sort_order, source_category_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, year, line.AccountType, line.Note, line.Account, line.Debit, line.Credit, index+1, line.SourceCategoryID); err != nil {
			return fmt.Errorf("seed Trial Balance row: %w", err)
		}
	}
	// Mark a newly seeded year as live-synchronised before committing it. Its seed already
	// contains every transaction that existed at this moment, so a later compatibility
	// backfill must not add those same amounts a second time.
	if _, err := tx.Exec(`
		INSERT INTO settings (key, value) VALUES (?, datetime('now','localtime'))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, trialBalanceLiveSyncSettingPrefix+strconv.Itoa(year)); err != nil {
		return fmt.Errorf("mark live Trial Balance synchronization: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return ensureOpeningAccumulatedFundLine(year)
}

// backfillUnsyncedTransactionCreates repairs years initialized by the immediately previous
// release, where the one-time seed worked but transactions entered afterwards did not reach
// the Trial Balance. Only transactions created after the year's initialization timestamp are
// added, and a durable marker makes the repair run once. New years write the marker while
// seeding and therefore never enter this compatibility path.
func backfillUnsyncedTransactionCreates(year int) error {
	settingKey := trialBalanceLiveSyncSettingPrefix + strconv.Itoa(year)
	var marker string
	err := db.DB.QueryRow("SELECT value FROM settings WHERE key = ?", settingKey).Scan(&marker)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return fmt.Errorf("check live Trial Balance synchronization: %w", err)
	}

	type transactionTotal struct {
		AccountType string
		CategoryID  int64
		Amount      float64
	}
	rows, err := db.DB.Query(`
		SELECT t.type, t.category_id, COALESCE(SUM(t.amount), 0)
		FROM transactions t
		JOIN trial_balance_years y ON y.year = ?
		WHERE t.date >= ? AND t.date < ?
			AND COALESCE(t.category_id, 0) > 0
			AND t.created_at > y.initialized_at
		GROUP BY t.type, t.category_id
	`, year, fmt.Sprintf("%04d-01-01", year), fmt.Sprintf("%04d-01-01", year+1))
	if err != nil {
		return fmt.Errorf("query unsynchronised Trial Balance transactions: %w", err)
	}
	var totals []transactionTotal
	for rows.Next() {
		var total transactionTotal
		if err := rows.Scan(&total.AccountType, &total.CategoryID, &total.Amount); err != nil {
			rows.Close()
			return fmt.Errorf("scan unsynchronised Trial Balance transactions: %w", err)
		}
		totals = append(totals, total)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close unsynchronised Trial Balance transactions: %w", err)
	}

	tx, err := db.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, total := range totals {
		if err := applyTransactionTrialBalanceDelta(tx, year, total.AccountType, total.CategoryID, total.Amount); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`
		INSERT INTO settings (key, value) VALUES (?, datetime('now','localtime'))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, settingKey); err != nil {
		return fmt.Errorf("save live Trial Balance synchronization marker: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit live Trial Balance synchronization: %w", err)
	}
	return nil
}

// ensureOpeningAccumulatedFundLine guarantees that every year has one visible equity row
// inside the Trial Balance. The old Setup-only workflow could leave a fresh year without any
// place to enter the brought-forward fund, which produced a warning that told the operator to
// leave the primary entry screen. Keeping the row here makes the Trial Balance self-contained.
func ensureOpeningAccumulatedFundLine(year int) error {
	var nextSort int
	if err := db.DB.QueryRow("SELECT COALESCE(MAX(sort_order), 0) + 1 FROM trial_balance_entries WHERE year = ?", year).Scan(&nextSort); err != nil {
		return fmt.Errorf("load opening fund Trial Balance position: %w", err)
	}
	_, err := db.DB.Exec(`
		INSERT INTO trial_balance_entries (year, account_type, note_ref, account_name, debit, credit, sort_order)
		SELECT ?, 'equity', '', 'Opening Accumulated Fund', 0, 0, ?
		WHERE NOT EXISTS (
			SELECT 1 FROM trial_balance_entries
			WHERE year = ? AND account_type = 'equity'
		)
	`, year, nextSort, year)
	if err != nil {
		return fmt.Errorf("ensure opening accumulated fund row: %w", err)
	}
	return nil
}

// applyTransactionTrialBalanceDelta keeps supporting transaction records and the primary
// Trial Balance in one accounting flow. The caller first initializes the affected year, then
// invokes this helper inside the same SQL transaction as the transaction mutation. If either
// write fails, both roll back, preventing a saved register entry from disagreeing with Notes
// and statements.
func applyTransactionTrialBalanceDelta(tx *sql.Tx, year int, accountType string, categoryID int64, delta float64) error {
	var line TrialBalanceLine
	err := tx.QueryRow(`
		SELECT id, debit, credit
		FROM trial_balance_entries
		WHERE year = ? AND source_category_id = ?
		ORDER BY id
		LIMIT 1
	`, year, categoryID).Scan(&line.ID, &line.Debit, &line.Credit)
	if err == sql.ErrNoRows {
		var accountName, noteRef, categoryType string
		if err := tx.QueryRow(`
			SELECT type, name, COALESCE(note_ref, '')
			FROM categories
			WHERE id = ?
		`, categoryID).Scan(&categoryType, &accountName, &noteRef); err != nil {
			return fmt.Errorf("load transaction category for Trial Balance: %w", err)
		}
		if categoryType != accountType {
			return fmt.Errorf("transaction category type does not match Trial Balance type")
		}
		var nextSort int
		if err := tx.QueryRow("SELECT COALESCE(MAX(sort_order), 0) + 1 FROM trial_balance_entries WHERE year = ?", year).Scan(&nextSort); err != nil {
			return fmt.Errorf("load transaction Trial Balance position: %w", err)
		}
		result, err := tx.Exec(`
			INSERT INTO trial_balance_entries
				(year, account_type, note_ref, account_name, debit, credit, sort_order, source_category_id)
			VALUES (?, ?, ?, ?, 0, 0, ?, ?)
		`, year, accountType, noteRef, accountName, nextSort, categoryID)
		if err != nil {
			return fmt.Errorf("create transaction Trial Balance row: %w", err)
		}
		line.ID, err = result.LastInsertId()
		if err != nil {
			return fmt.Errorf("resolve transaction Trial Balance row: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("load transaction Trial Balance row: %w", err)
	}

	line.AccountType = accountType
	updatedAmount := trialBalanceAmount(accountType, line.Debit, line.Credit) + delta
	line.Debit = 0
	line.Credit = 0
	assignNaturalBalance(&line, updatedAmount)
	if _, err := tx.Exec(`
		UPDATE trial_balance_entries
		SET debit = ?, credit = ?, updated_at = datetime('now','localtime')
		WHERE id = ? AND year = ?
	`, line.Debit, line.Credit, line.ID, year); err != nil {
		return fmt.Errorf("apply transaction to Trial Balance: %w", err)
	}
	if _, err := tx.Exec("UPDATE trial_balance_years SET updated_at = datetime('now','localtime') WHERE year = ?", year); err != nil {
		return fmt.Errorf("touch transaction Trial Balance year: %w", err)
	}
	return nil
}

type trialBalanceSeedLine struct {
	TrialBalanceLine
	SourceCategoryID any
}

// buildLegacyTrialBalanceSeed creates the first saved account set and opening amounts for a
// year. Later transaction mutations apply targeted deltas instead of rebuilding the entire
// year, so direct Trial Balance adjustments remain intact.
func buildLegacyTrialBalanceSeed(year int) ([]trialBalanceSeedLine, error) {
	startDate, endDate := yearBounds(year)
	positions, err := loadCategoryPositions(year)
	if err != nil {
		return nil, err
	}
	assetSchedule, err := buildFixedAssetData(year)
	if err != nil {
		return nil, fmt.Errorf("build fixed-asset Trial Balance seed: %w", err)
	}
	for _, line := range assetSchedule.Lines {
		if line.ClosingCost != 0 || line.OpeningCost != 0 {
			positions[line.CategoryID] = line.ClosingCost
		}
	}

	rows, err := db.DB.Query(`
		SELECT c.id, c.type, c.name, c.parent_id, COALESCE(parent.name, ''), COALESCE(c.note_ref, ''),
			COALESCE(SUM(CASE WHEN t.date >= ? AND t.date < ? THEN t.amount ELSE 0 END), 0)
		FROM categories c
		LEFT JOIN categories parent ON parent.id = c.parent_id
		LEFT JOIN transactions t ON t.category_id = c.id AND t.type = c.type AND t.date >= ? AND t.date < ?
		WHERE COALESCE(c.is_active, 1) = 1
		GROUP BY c.id, c.type, c.name, c.parent_id, parent.name, c.note_ref
		ORDER BY CAST(NULLIF(c.note_ref, '') AS INTEGER),
			CASE c.type WHEN 'income' THEN 0 WHEN 'expenditure' THEN 1 WHEN 'asset' THEN 2 ELSE 3 END,
			CASE WHEN c.parent_id = 0 THEN c.id ELSE c.parent_id END, c.parent_id, c.id
	`, startDate, endDate, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("query Trial Balance seed accounts: %w", err)
	}
	defer rows.Close()
	var lines []trialBalanceSeedLine
	for rows.Next() {
		var id, parentID int64
		var accountType, name, parentName, noteRef string
		var periodAmount float64
		if err := rows.Scan(&id, &accountType, &name, &parentID, &parentName, &noteRef, &periodAmount); err != nil {
			return nil, err
		}
		amount := periodAmount
		if accountType == "asset" || accountType == "liability" {
			amount = positions[id]
		}
		// Notes in the workbook display the leaf caption itself. The source category ID is
		// still retained for migration traceability, so a parent prefix is unnecessary and
		// would make the upgraded Notes visibly differ from the existing report.
		label := name
		line := trialBalanceSeedLine{
			TrialBalanceLine: TrialBalanceLine{AccountType: accountType, Note: noteRef, Account: label},
			SourceCategoryID: id,
		}
		assignNaturalBalance(&line.TrialBalanceLine, amount)
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if assetSchedule.TotalClosingAccumulatedDep != 0 {
		lines = append(lines, trialBalanceSeedLine{TrialBalanceLine: TrialBalanceLine{
			AccountType: "asset", Note: "21", Account: "Accumulated Depreciation & Amortization", Credit: assetSchedule.TotalClosingAccumulatedDep,
		}})
	}
	postedDepreciation, err := loadNoteMovement("expenditure", "21", startDate, endDate, false)
	if err != nil {
		return nil, err
	}
	if postedDepreciation == 0 && assetSchedule.TotalCharge != 0 {
		lines = append(lines, trialBalanceSeedLine{TrialBalanceLine: TrialBalanceLine{
			AccountType: "expenditure", Note: "21", Account: "Depreciation & Amortization Expense", Debit: assetSchedule.TotalCharge,
		}})
	}
	fund, err := loadFundRollforward(year)
	if err != nil {
		return nil, err
	}
	adjustedOpeningFund := fund.OpeningBalance + fund.PriorYearAdjustment
	if adjustedOpeningFund != 0 {
		line := trialBalanceSeedLine{TrialBalanceLine: TrialBalanceLine{AccountType: "equity", Account: "Opening Accumulated Fund"}}
		assignNaturalBalance(&line.TrialBalanceLine, adjustedOpeningFund)
		lines = append(lines, line)
	}
	return lines, nil
}

func assignNaturalBalance(line *TrialBalanceLine, amount float64) {
	naturalDebit := line.AccountType == "asset" || line.AccountType == "expenditure"
	if (naturalDebit && amount >= 0) || (!naturalDebit && amount < 0) {
		line.Debit = absFloat(amount)
		return
	}
	line.Credit = absFloat(amount)
}

func absFloat(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

// trialBalanceAmount converts debit/credit storage into the positive presentation used by
// each statement family. Contra balances naturally become negative, which keeps the Notes
// tied to the exact TB row instead of hiding them behind special-case arithmetic.
func trialBalanceAmount(accountType string, debit, credit float64) float64 {
	if accountType == "asset" || accountType == "expenditure" {
		return debit - credit
	}
	return credit - debit
}
