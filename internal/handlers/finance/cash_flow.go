package finance

import (
	"fmt"
	"net/http"
	"ramseyer-finance/internal/db"
)

// CashFlowData is a year-scoped review of the transaction types that can inform cash flow.
// Asset and liability entries remain positive informational totals because their category or
// payment-method label does not prove whether cash entered or left the church.
type CashFlowData struct {
	Active                 string
	Year                   string
	PriorYear              string
	Years                  []int
	IncomeEntries          float64
	ExpenditureEntries     float64
	IncomeLessExpenditure  float64
	AssetEntries           float64
	LiabilityEntries       float64
	PPEEntries             float64
	InvestmentEntries      float64
	IntangibleAssetEntries float64
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

// buildCashFlowData totals only the selected year's saved transactions. Income less expenditure
// is calculated because those types explicitly describe money received and spent. Asset and
// liability entries are shown separately without inventing a direction or counter-entry.
func buildCashFlowData(year int) (CashFlowData, error) {
	years, err := reportYears(year)
	if err != nil {
		return CashFlowData{}, err
	}
	data := CashFlowData{Active: "cash-flow", Year: fmt.Sprintf("%d", year), PriorYear: fmt.Sprintf("%d", year-1), Years: years}

	data.IncomeEntries, err = loadTrialBalanceTypeTotal(year, "income")
	if err != nil {
		return CashFlowData{}, err
	}
	data.ExpenditureEntries, err = loadTrialBalanceTypeTotal(year, "expenditure")
	if err != nil {
		return CashFlowData{}, err
	}
	data.AssetEntries, err = loadTrialBalanceTypeTotal(year, "asset")
	if err != nil {
		return CashFlowData{}, err
	}
	data.LiabilityEntries, err = loadTrialBalanceTypeTotal(year, "liability")
	if err != nil {
		return CashFlowData{}, err
	}
	data.IncomeLessExpenditure = data.IncomeEntries - data.ExpenditureEntries

	data.PPEEntries, err = loadTrialBalanceNoteTotal(year, "asset", "21")
	if err != nil {
		return CashFlowData{}, err
	}
	data.InvestmentEntries, err = loadTrialBalanceNoteTotal(year, "asset", "22")
	if err != nil {
		return CashFlowData{}, err
	}
	data.IntangibleAssetEntries, err = loadTrialBalanceNoteTotal(year, "asset", "23")
	if err != nil {
		return CashFlowData{}, err
	}
	return data, nil
}

func loadTrialBalanceTypeTotal(year int, accountType string) (float64, error) {
	startDate, endDate := yearBounds(year)
	var total float64
	err := db.DB.QueryRow(`
		SELECT COALESCE(SUM(amount), 0)
		FROM financial_postings
		WHERE account_type = ? AND date >= ? AND date < ?
	`, accountType, startDate, endDate).Scan(&total)
	return total, err
}

func loadTrialBalanceNoteTotal(year int, accountType, noteRef string) (float64, error) {
	startDate, endDate := yearBounds(year)
	var total float64
	err := db.DB.QueryRow(`
		SELECT COALESCE(SUM(posting.amount), 0)
		FROM financial_postings posting
		JOIN categories category ON category.id = posting.category_id
		WHERE posting.account_type = ? AND category.note_ref = ?
			AND posting.date >= ? AND posting.date < ?
	`, accountType, noteRef, startDate, endDate,
	).Scan(&total)
	return total, err
}
