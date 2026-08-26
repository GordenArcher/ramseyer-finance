package finance

import (
	"fmt"
	"net/http"
	"ramseyer-finance/internal/db"
)

// CashFlowData is the workbook-aligned indirect cash-flow statement. Each subtotal is
// retained separately so the template can show the operating, investing, and financing
// sections and finish with an explicit reconciliation to Note 26 cash balances.
type CashFlowData struct {
	Active                   string
	Year                     string
	PriorYear                string
	Years                    []int
	Surplus                  float64
	DepreciationAmortization float64
	PriorYearAdjustment      float64
	InventoryMovement        float64
	ReceivablesMovement      float64
	PayablesMovement         float64
	NetOperatingCash         float64
	PPEAcquisitions          float64
	InvestmentAcquisitions   float64
	IntangibleAcquisitions   float64
	NetInvestingCash         float64
	LongTermLoanMovement     float64
	NetFinancingCash         float64
	NetCashChange            float64
	OpeningCash              float64
	ExpectedClosingCash      float64
	ReportedClosingCash      float64
	ReconciliationDifference float64
}

func CashFlowStatement(w http.ResponseWriter, r *http.Request) {
	year, err := parseReportYear(r.URL.Query().Get("year"))
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	data, err := buildCashFlowData(year)
	if err != nil {
		serverError(w, err)
		return
	}
	RenderTemplate(w, "cash-flow", data)
}

// buildCashFlowData follows the workbook's indirect method. Working-capital movements use
// opening minus closing for assets and closing minus opening for liabilities, so an asset
// increase consumes cash while a liability increase releases cash. The final difference is
// deliberately not forced to zero: it is the control that reveals missing cash-account or
// balance-sheet movements.
func buildCashFlowData(year int) (CashFlowData, error) {
	if err := ensureTrialBalanceYear(year); err != nil {
		return CashFlowData{}, err
	}
	if err := ensureTrialBalanceYear(year - 1); err != nil {
		return CashFlowData{}, err
	}
	years, err := reportYears(year)
	if err != nil {
		return CashFlowData{}, err
	}
	data := CashFlowData{Active: "cash-flow", Year: fmt.Sprintf("%d", year), PriorYear: fmt.Sprintf("%d", year-1), Years: years}

	currentIncome, err := loadTrialBalanceTypeTotal(year, "income")
	if err != nil {
		return CashFlowData{}, err
	}
	currentExpense, err := loadTrialBalanceTypeTotal(year, "expenditure")
	if err != nil {
		return CashFlowData{}, err
	}
	data.Surplus = currentIncome - currentExpense
	data.DepreciationAmortization, err = loadTrialBalanceNoteTotal(year, "expenditure", "21")
	if err != nil {
		return CashFlowData{}, err
	}

	priorInventory, err := loadTrialBalanceOpeningNoteTotal(year, "asset", "24")
	if err != nil {
		return CashFlowData{}, err
	}
	currentInventory, err := loadTrialBalanceNoteTotal(year, "asset", "24")
	if err != nil {
		return CashFlowData{}, err
	}
	priorReceivables, err := loadTrialBalanceOpeningNoteTotal(year, "asset", "25")
	if err != nil {
		return CashFlowData{}, err
	}
	currentReceivables, err := loadTrialBalanceNoteTotal(year, "asset", "25")
	if err != nil {
		return CashFlowData{}, err
	}
	priorPayables, err := loadTrialBalanceOpeningNoteTotal(year, "liability", "28")
	if err != nil {
		return CashFlowData{}, err
	}
	currentPayables, err := loadTrialBalanceNoteTotal(year, "liability", "28")
	if err != nil {
		return CashFlowData{}, err
	}
	data.InventoryMovement = priorInventory - currentInventory
	data.ReceivablesMovement = priorReceivables - currentReceivables
	data.PayablesMovement = currentPayables - priorPayables
	data.NetOperatingCash = data.Surplus + data.DepreciationAmortization + data.InventoryMovement + data.ReceivablesMovement + data.PayablesMovement

	priorPPE, err := loadTrialBalanceNoteTotal(year-1, "asset", "21")
	if err != nil {
		return CashFlowData{}, err
	}
	currentPPE, err := loadTrialBalanceNoteTotal(year, "asset", "21")
	if err != nil {
		return CashFlowData{}, err
	}
	data.PPEAcquisitions = maxFloat(currentPPE-priorPPE+data.DepreciationAmortization, 0)
	priorInvestment, err := loadTrialBalanceNoteTotal(year-1, "asset", "22")
	if err != nil {
		return CashFlowData{}, err
	}
	currentInvestment, err := loadTrialBalanceNoteTotal(year, "asset", "22")
	if err != nil {
		return CashFlowData{}, err
	}
	data.InvestmentAcquisitions = maxFloat(currentInvestment-priorInvestment, 0)
	priorIntangible, err := loadTrialBalanceNoteTotal(year-1, "asset", "23")
	if err != nil {
		return CashFlowData{}, err
	}
	currentIntangible, err := loadTrialBalanceNoteTotal(year, "asset", "23")
	if err != nil {
		return CashFlowData{}, err
	}
	data.IntangibleAcquisitions = maxFloat(currentIntangible-priorIntangible, 0)
	data.NetInvestingCash = -data.PPEAcquisitions - data.InvestmentAcquisitions - data.IntangibleAcquisitions

	priorLoan, err := loadTrialBalanceNoteTotal(year-1, "liability", "27")
	if err != nil {
		return CashFlowData{}, err
	}
	currentLoan, err := loadTrialBalanceNoteTotal(year, "liability", "27")
	if err != nil {
		return CashFlowData{}, err
	}
	data.LongTermLoanMovement = currentLoan - priorLoan
	data.NetFinancingCash = data.LongTermLoanMovement
	data.NetCashChange = data.NetOperatingCash + data.NetInvestingCash + data.NetFinancingCash
	data.OpeningCash, err = loadTrialBalanceOpeningNoteTotal(year, "asset", "26")
	if err != nil {
		return CashFlowData{}, err
	}
	data.ReportedClosingCash, err = loadTrialBalanceNoteTotal(year, "asset", "26")
	if err != nil {
		return CashFlowData{}, err
	}
	data.ExpectedClosingCash = data.OpeningCash + data.NetCashChange
	data.ReconciliationDifference = data.ReportedClosingCash - data.ExpectedClosingCash
	return data, nil
}

// buildLegacyCashFlowData is retained as a migration reference. The active cash-flow page
// uses closing TB balances and their prior-year comparatives, matching the workbook links.
func buildLegacyCashFlowData(year int) (CashFlowData, error) {
	years, err := reportYears(year)
	if err != nil {
		return CashFlowData{}, fmt.Errorf("load cash-flow years: %w", err)
	}
	data := CashFlowData{
		Active:    "cash-flow",
		Year:      fmt.Sprintf("%d", year),
		PriorYear: fmt.Sprintf("%d", year-1),
		Years:     years,
	}

	startDate, endDate := yearBounds(year)
	var income, expenditure float64
	if err := loadIncomeExpenseTotals(startDate, endDate, &income, &expenditure); err != nil {
		return CashFlowData{}, fmt.Errorf("load cash-flow surplus: %w", err)
	}
	data.Surplus = income - expenditure
	data.DepreciationAmortization, err = loadNoteMovement("expenditure", "21", startDate, endDate, false)
	if err != nil {
		return CashFlowData{}, fmt.Errorf("load depreciation and amortization: %w", err)
	}
	if data.DepreciationAmortization == 0 {
		assetSchedule, err := buildFixedAssetData(year)
		if err != nil {
			return CashFlowData{}, fmt.Errorf("build fixed-asset schedule for cash flow: %w", err)
		}
		data.DepreciationAmortization = assetSchedule.TotalCharge
		// The operating surplus query above contains only posted expenditure. If Note 21
		// supplied an automatic charge, deduct it here before adding it back so the indirect
		// cash-flow reconciliation starts from the same accrual surplus as the annual report.
		data.Surplus -= assetSchedule.TotalCharge
	}
	fund, err := loadFundRollforward(year)
	if err != nil {
		return CashFlowData{}, err
	}
	data.PriorYearAdjustment = fund.PriorYearAdjustment

	priorInventory, err := loadNoteOpeningPosition(year, "asset", "24")
	if err != nil {
		return CashFlowData{}, err
	}
	currentInventory, err := loadNotePosition(year, "asset", "24")
	if err != nil {
		return CashFlowData{}, err
	}
	priorReceivables, err := loadNoteOpeningPosition(year, "asset", "25")
	if err != nil {
		return CashFlowData{}, err
	}
	currentReceivables, err := loadNotePosition(year, "asset", "25")
	if err != nil {
		return CashFlowData{}, err
	}
	priorPayables, err := loadNoteOpeningPosition(year, "liability", "28")
	if err != nil {
		return CashFlowData{}, err
	}
	currentPayables, err := loadNotePosition(year, "liability", "28")
	if err != nil {
		return CashFlowData{}, err
	}
	data.InventoryMovement = priorInventory - currentInventory
	data.ReceivablesMovement = priorReceivables - currentReceivables
	data.PayablesMovement = currentPayables - priorPayables
	data.NetOperatingCash = data.Surplus + data.DepreciationAmortization + data.PriorYearAdjustment +
		data.InventoryMovement + data.ReceivablesMovement + data.PayablesMovement

	data.PPEAcquisitions, err = loadNoteMovement("asset", "21", startDate, endDate, true)
	if err != nil {
		return CashFlowData{}, err
	}
	data.InvestmentAcquisitions, err = loadNoteMovement("asset", "22", startDate, endDate, true)
	if err != nil {
		return CashFlowData{}, err
	}
	data.IntangibleAcquisitions, err = loadNoteMovement("asset", "23", startDate, endDate, true)
	if err != nil {
		return CashFlowData{}, err
	}
	data.NetInvestingCash = -data.PPEAcquisitions - data.InvestmentAcquisitions - data.IntangibleAcquisitions

	data.LongTermLoanMovement, err = loadNoteMovement("liability", "27", startDate, endDate, false)
	if err != nil {
		return CashFlowData{}, err
	}
	data.NetFinancingCash = data.LongTermLoanMovement
	data.NetCashChange = data.NetOperatingCash + data.NetInvestingCash + data.NetFinancingCash

	data.OpeningCash, err = loadCashOpeningPosition(year)
	if err != nil {
		return CashFlowData{}, err
	}
	data.ReportedClosingCash, err = loadCashPosition(year)
	if err != nil {
		return CashFlowData{}, err
	}
	data.ExpectedClosingCash = data.OpeningCash + data.NetCashChange
	data.ReconciliationDifference = data.ReportedClosingCash - data.ExpectedClosingCash
	return data, nil
}

func loadTrialBalanceTypeTotal(year int, accountType string) (float64, error) {
	rows, err := db.DB.Query("SELECT debit, credit FROM trial_balance_entries WHERE year = ? AND account_type = ?", year, accountType)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var total float64
	for rows.Next() {
		var debit, credit float64
		if err := rows.Scan(&debit, &credit); err != nil {
			return 0, err
		}
		total += trialBalanceAmount(accountType, debit, credit)
	}
	return total, rows.Err()
}

func loadTrialBalanceNoteTotal(year int, accountType, noteRef string) (float64, error) {
	rows, err := db.DB.Query("SELECT debit, credit FROM trial_balance_entries WHERE year = ? AND account_type = ? AND note_ref = ?", year, accountType, noteRef)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var total float64
	for rows.Next() {
		var debit, credit float64
		if err := rows.Scan(&debit, &credit); err != nil {
			return 0, err
		}
		total += trialBalanceAmount(accountType, debit, credit)
	}
	return total, rows.Err()
}

// loadTrialBalanceOpeningNoteTotal honours an explicitly configured opening for the
// selected year, which preserves first-year and upgraded books that have no prior TB. When
// no opening was entered, the prior year's saved closing note becomes the opening balance.
func loadTrialBalanceOpeningNoteTotal(year int, accountType, noteRef string) (float64, error) {
	var amount float64
	var count int
	if err := db.DB.QueryRow(`
		SELECT COALESCE(SUM(opening.amount), 0), COUNT(*)
		FROM account_opening_balances opening
		JOIN categories c ON c.id = opening.category_id
		WHERE opening.year = ? AND c.type = ? AND c.note_ref = ?
	`, year, accountType, noteRef).Scan(&amount, &count); err != nil {
		return 0, err
	}
	if count > 0 {
		return amount, nil
	}
	if accountType == "asset" && noteRef == "26" {
		if err := db.DB.QueryRow("SELECT COALESCE(SUM(amount), 0), COUNT(*) FROM opening_balances WHERE year = ?", year).Scan(&amount, &count); err != nil {
			return 0, err
		}
		if count > 0 {
			return amount, nil
		}
	}
	return loadTrialBalanceNoteTotal(year-1, accountType, noteRef)
}

func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

func loadNoteBalance(categoryType, noteRef, endDate string) (float64, error) {
	var amount float64
	err := db.DB.QueryRow(`
		SELECT COALESCE(SUM(t.amount), 0)
		FROM transactions t
		JOIN categories c ON c.id = t.category_id AND c.type = t.type
		WHERE c.type = ? AND c.note_ref = ? AND t.date < ?
	`, categoryType, noteRef, endDate).Scan(&amount)
	if err != nil {
		return 0, fmt.Errorf("load note %s balance: %w", noteRef, err)
	}
	return amount, nil
}

func loadNotePosition(year int, categoryType, noteRef string) (float64, error) {
	positions, err := loadCategoryPositions(year)
	if err != nil {
		return 0, err
	}
	rows, err := db.DB.Query(`SELECT id FROM categories WHERE type = ? AND note_ref = ?`, categoryType, noteRef)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var total float64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		total += positions[id]
	}
	return total, rows.Err()
}

// loadNoteOpeningPosition uses the selected year's independently entered openings when
// present. If the operator has not configured that note yet, the preceding year-end
// position remains the backward-compatible opening. This keeps working-capital cash-flow
// movements aligned with the same opening values used by the balance sheet.
func loadNoteOpeningPosition(year int, categoryType, noteRef string) (float64, error) {
	var amount float64
	var count int
	if err := db.DB.QueryRow(`
		SELECT COALESCE(SUM(b.amount), 0), COUNT(*)
		FROM account_opening_balances b
		JOIN categories c ON c.id = b.category_id
		WHERE b.year = ? AND c.type = ? AND c.note_ref = ?
	`, year, categoryType, noteRef).Scan(&amount, &count); err != nil {
		return 0, fmt.Errorf("load Note %s opening for %d: %w", noteRef, year, err)
	}
	if count > 0 {
		return amount, nil
	}
	return loadNotePosition(year-1, categoryType, noteRef)
}

// loadNoteMovement returns signed activity within one year. positiveOnly is used for asset
// acquisitions because reductions are disposals or corrections, not acquisition cash
// outflows, and must not be netted into the additions line without disposal proceeds data.
func loadNoteMovement(categoryType, noteRef, startDate, endDate string, positiveOnly bool) (float64, error) {
	predicate := ""
	if positiveOnly {
		predicate = " AND t.amount > 0"
	}
	var amount float64
	err := db.DB.QueryRow(`
		SELECT COALESCE(SUM(t.amount), 0)
		FROM transactions t
		JOIN categories c ON c.id = t.category_id AND c.type = t.type
		WHERE c.type = ? AND c.note_ref = ? AND t.date >= ? AND t.date < ?`+predicate,
		categoryType, noteRef, startDate, endDate,
	).Scan(&amount)
	if err != nil {
		return 0, fmt.Errorf("load note %s movement: %w", noteRef, err)
	}
	return amount, nil
}

// loadCashPosition preserves compatibility with the previous Bank/Cash/Momo opening-balance
// setup. If a year has explicit legacy openings, those replace earlier cash movements and
// are combined only with the selected year's Note 26 movement; otherwise all Note 26
// postings up to year-end form the closing balance.
func loadCashPosition(year int) (float64, error) {
	startDate, endDate := yearBounds(year)
	var accountOpening float64
	var accountOpeningCount int
	if err := db.DB.QueryRow(`
		SELECT COALESCE(SUM(b.amount), 0), COUNT(*)
		FROM account_opening_balances b
		JOIN categories c ON c.id = b.category_id
		WHERE b.year = ? AND c.type = 'asset' AND c.note_ref = '26'
	`, year).Scan(&accountOpening, &accountOpeningCount); err != nil {
		return 0, fmt.Errorf("load Note 26 account openings for %d: %w", year, err)
	}
	if accountOpeningCount > 0 {
		movement, err := loadNoteMovement("asset", "26", startDate, endDate, false)
		return accountOpening + movement, err
	}
	var opening float64
	if err := db.DB.QueryRow(`SELECT COALESCE(SUM(amount), 0) FROM opening_balances WHERE year = ?`, year).Scan(&opening); err != nil {
		return 0, fmt.Errorf("load cash opening balance for %d: %w", year, err)
	}
	if opening != 0 {
		movement, err := loadNoteMovement("asset", "26", startDate, endDate, false)
		return opening + movement, err
	}
	return loadNoteBalance("asset", "26", endDate)
}

// loadCashOpeningPosition honours Note 26 openings entered for the selected year before
// falling back to the legacy Bank/Cash/Momo setup and, finally, the previous closing cash
// position. The precedence matters during migration because a confirmed opening should
// replace incomplete historical postings rather than be ignored by the cash-flow report.
func loadCashOpeningPosition(year int) (float64, error) {
	var accountOpening float64
	var accountCount int
	if err := db.DB.QueryRow(`
		SELECT COALESCE(SUM(b.amount), 0), COUNT(*)
		FROM account_opening_balances b
		JOIN categories c ON c.id = b.category_id
		WHERE b.year = ? AND c.type = 'asset' AND c.note_ref = '26'
	`, year).Scan(&accountOpening, &accountCount); err != nil {
		return 0, fmt.Errorf("load Note 26 opening for %d: %w", year, err)
	}
	if accountCount > 0 {
		return accountOpening, nil
	}

	var legacyOpening float64
	var legacyCount int
	if err := db.DB.QueryRow(`
		SELECT COALESCE(SUM(amount), 0), COUNT(*)
		FROM opening_balances
		WHERE year = ?
	`, year).Scan(&legacyOpening, &legacyCount); err != nil {
		return 0, fmt.Errorf("load legacy cash opening for %d: %w", year, err)
	}
	if legacyCount > 0 {
		return legacyOpening, nil
	}
	return loadCashPosition(year - 1)
}
