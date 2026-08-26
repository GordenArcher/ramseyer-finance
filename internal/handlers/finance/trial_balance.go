package finance

import (
	"fmt"
	"net/http"
	"ramseyer-finance/internal/db"
)

type TrialBalanceLine struct {
	Note    string
	Account string
	Debit   float64
	Credit  float64
}

type TrialBalanceData struct {
	Active      string
	Year        string
	Years       []int
	Lines       []TrialBalanceLine
	TotalDebit  float64
	TotalCredit float64
	Difference  float64
}

func TrialBalance(w http.ResponseWriter, r *http.Request) {
	year, err := parseReportYear(r.URL.Query().Get("year"))
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	data, err := buildTrialBalanceData(year)
	if err != nil {
		serverError(w, err)
		return
	}
	RenderTemplate(w, "trial-balance", data)
}

// buildTrialBalanceData is the application's primary completeness control. Flow accounts
// use activity within the selected year and balance-sheet accounts use their configured
// opening plus signed movement. Presenting natural debit and credit columns makes missing
// counterpart entries visible instead of allowing a later report to absorb them into a
// calculated residual.
func buildTrialBalanceData(year int) (TrialBalanceData, error) {
	years, err := reportYears(year)
	if err != nil {
		return TrialBalanceData{}, err
	}
	data := TrialBalanceData{Active: "trial-balance", Year: fmt.Sprintf("%d", year), Years: years}
	startDate, endDate := yearBounds(year)
	positions, err := loadCategoryPositions(year)
	if err != nil {
		return TrialBalanceData{}, err
	}
	assetSchedule, err := buildFixedAssetData(year)
	if err != nil {
		return TrialBalanceData{}, fmt.Errorf("build fixed-asset trial-balance positions: %w", err)
	}
	// Fixed-asset openings carry separate gross-cost and accumulated-depreciation values.
	// Replace the generic net movement for those classes with closing gross cost here, then
	// present accumulated depreciation as its own credit control below.
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
		return TrialBalanceData{}, fmt.Errorf("query trial balance accounts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, parentID int64
		var categoryType, name, parentName, noteRef string
		var periodAmount float64
		if err := rows.Scan(&id, &categoryType, &name, &parentID, &parentName, &noteRef, &periodAmount); err != nil {
			return TrialBalanceData{}, fmt.Errorf("scan trial balance account: %w", err)
		}
		amount := periodAmount
		if categoryType == "asset" || categoryType == "liability" {
			amount = positions[id]
		}
		if amount == 0 {
			continue
		}
		label := name
		if parentName != "" {
			label = parentName + " / " + name
		}
		line := TrialBalanceLine{Note: noteRef, Account: label}
		naturalDebit := categoryType == "asset" || categoryType == "expenditure"
		if (naturalDebit && amount >= 0) || (!naturalDebit && amount < 0) {
			line.Debit = absFloat(amount)
			data.TotalDebit += line.Debit
		} else {
			line.Credit = absFloat(amount)
			data.TotalCredit += line.Credit
		}
		data.Lines = append(data.Lines, line)
	}
	if err := rows.Err(); err != nil {
		return TrialBalanceData{}, err
	}

	if assetSchedule.TotalClosingAccumulatedDep != 0 {
		data.Lines = append(data.Lines, TrialBalanceLine{
			Note:    "21",
			Account: "Accumulated Depreciation & Amortization",
			Credit:  assetSchedule.TotalClosingAccumulatedDep,
		})
		data.TotalCredit += assetSchedule.TotalClosingAccumulatedDep
	}
	postedDepreciation, err := loadNoteMovement("expenditure", "21", startDate, endDate, false)
	if err != nil {
		return TrialBalanceData{}, fmt.Errorf("load posted depreciation for trial balance: %w", err)
	}
	if postedDepreciation == 0 && assetSchedule.TotalCharge != 0 {
		data.Lines = append(data.Lines, TrialBalanceLine{
			Note:    "21",
			Account: "Calculated Depreciation & Amortization Expense",
			Debit:   assetSchedule.TotalCharge,
		})
		data.TotalDebit += assetSchedule.TotalCharge
	}

	fund, err := loadFundRollforward(year)
	if err != nil {
		return TrialBalanceData{}, err
	}
	adjustedOpeningFund := fund.OpeningBalance + fund.PriorYearAdjustment
	if adjustedOpeningFund != 0 {
		line := TrialBalanceLine{Account: "Adjusted Opening Accumulated Fund"}
		if adjustedOpeningFund > 0 {
			line.Credit = adjustedOpeningFund
			data.TotalCredit += adjustedOpeningFund
		} else {
			line.Debit = -adjustedOpeningFund
			data.TotalDebit -= adjustedOpeningFund
		}
		data.Lines = append(data.Lines, line)
	}
	data.Difference = data.TotalDebit - data.TotalCredit
	return data, nil
}

func absFloat(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
