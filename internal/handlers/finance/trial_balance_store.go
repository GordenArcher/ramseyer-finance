package finance

import (
	"database/sql"
	"fmt"
	"ramseyer-finance/internal/db"
)

// ensureTrialBalanceYear creates an independent book the first time a year is requested.
// Existing installations begin with a one-time transaction-derived snapshot so upgrading
// does not erase their figures. The marker table is separate from the entries table so a
// user may intentionally delete every row without the app unexpectedly reseeding it.
func ensureTrialBalanceYear(year int) error {
	var exists int
	err := db.DB.QueryRow("SELECT 1 FROM trial_balance_years WHERE year = ?", year).Scan(&exists)
	if err == nil {
		return nil
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
		return tx.Commit()
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
	return tx.Commit()
}

type trialBalanceSeedLine struct {
	TrialBalanceLine
	SourceCategoryID any
}

// buildLegacyTrialBalanceSeed is intentionally isolated from normal report reads. It is
// only an upgrade bridge: once the year marker exists, every downstream report reads the
// saved Trial Balance and never recalculates these values from transactions.
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
		line := trialBalanceSeedLine{TrialBalanceLine: TrialBalanceLine{AccountType: "equity", Account: "Adjusted Opening Accumulated Fund"}}
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
