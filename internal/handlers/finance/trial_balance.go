package finance

import (
	"fmt"
	"net/http"
	"ramseyer-finance/internal/db"
	"strconv"
)

// TrialBalanceLine is one calculated account balance for the selected year. Debit and Credit
// are presentation values derived from Amount and the category's normal side; none of these
// values are persisted by the Trial Balance page.
type TrialBalanceLine struct {
	ID          int64
	AccountType string
	Note        string
	Account     string
	Debit       float64
	Credit      float64
	Amount      float64
	NormalSide  string
	IsCustom    bool
}

// TrialBalanceGroup follows the workbook's note organization while remaining a calculated
// software view. Grouping is metadata only: saved amounts still belong exclusively to dated
// transaction entries.
type TrialBalanceGroup struct {
	Key         string
	AccountType string
	TypeLabel   string
	Note        string
	Title       string
	Lines       []TrialBalanceLine
}

type TrialBalanceData struct {
	Active        string
	Year          string
	Years         []int
	Lines         []TrialBalanceLine
	Groups        []TrialBalanceGroup
	TotalDebit    float64
	TotalCredit   float64
	Difference    float64
	UnpairedCount int
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

// buildTrialBalanceData calculates the selected year's quick-lookup balances directly from
// dated financial postings. Income and expenditure reset on 1 January, while assets and
// liabilities remain cumulative positions through year end. The opening accumulated fund is
// derived from the prior asset/liability position, so it is never another editable source.
func buildTrialBalanceData(year int) (TrialBalanceData, error) {
	years, err := reportYears(year)
	if err != nil {
		return TrialBalanceData{}, err
	}
	data := TrialBalanceData{Active: "trial-balance", Year: strconv.Itoa(year), Years: years}
	startDate, endDate := yearBounds(year)
	rows, err := db.DB.Query(`
		SELECT c.id, c.type, c.note_ref, c.name,
			COALESCE(SUM(CASE
				WHEN c.type IN ('income', 'expenditure') AND posting.date >= ? AND posting.date < ? THEN posting.amount
				WHEN c.type IN ('asset', 'liability') AND posting.date < ? THEN posting.amount
				ELSE 0
			END), 0)
		FROM categories c
		LEFT JOIN financial_postings posting ON posting.category_id = c.id
		WHERE COALESCE(c.is_active, 1) = 1
		GROUP BY c.id, c.type, c.note_ref, c.name, c.parent_id
		ORDER BY CAST(NULLIF(c.note_ref, '') AS INTEGER),
			CASE c.type WHEN 'income' THEN 0 WHEN 'expenditure' THEN 1 WHEN 'asset' THEN 2 ELSE 3 END,
			CASE WHEN c.parent_id = 0 THEN c.id ELSE c.parent_id END, c.parent_id, c.id
	`, startDate, endDate, endDate)
	if err != nil {
		return TrialBalanceData{}, fmt.Errorf("query calculated Trial Balance rows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var line TrialBalanceLine
		if err := rows.Scan(&line.ID, &line.AccountType, &line.Note, &line.Account, &line.Amount); err != nil {
			return TrialBalanceData{}, fmt.Errorf("scan calculated Trial Balance row: %w", err)
		}
		line.NormalSide = trialBalanceNormalSide(line.AccountType)
		assignNaturalBalance(&line, line.Amount)
		appendTrialBalanceLine(&data, line)
	}
	if err := rows.Err(); err != nil {
		return TrialBalanceData{}, err
	}

	// Prior-period income and expenditure close into accumulated fund. Because the software
	// retains the dated entries, the brought-forward amount is the actual net asset position at
	// 1 January—not another figure the operator can type over in Setup or Trial Balance.
	var openingAssets, openingLiabilities float64
	if err := db.DB.QueryRow(`
		SELECT
			COALESCE(SUM(CASE WHEN account_type = 'asset' THEN amount ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN account_type = 'liability' THEN amount ELSE 0 END), 0)
		FROM financial_postings
		WHERE date < ?
	`, startDate).Scan(&openingAssets, &openingLiabilities); err != nil {
		return TrialBalanceData{}, fmt.Errorf("calculate opening accumulated fund: %w", err)
	}
	if openingFund := openingAssets - openingLiabilities; openingFund != 0 {
		line := TrialBalanceLine{AccountType: "equity", Account: "Opening Accumulated Fund", Amount: openingFund, NormalSide: "Credit"}
		assignNaturalBalance(&line, openingFund)
		appendTrialBalanceLine(&data, line)
	}

	if err := db.DB.QueryRow(`
		SELECT COUNT(*) FROM transactions
		WHERE date >= ? AND date < ? AND COALESCE(counter_category_id, 0) = 0
	`, startDate, endDate).Scan(&data.UnpairedCount); err != nil {
		return TrialBalanceData{}, fmt.Errorf("count incomplete financial entries: %w", err)
	}
	data.Difference = data.TotalDebit - data.TotalCredit
	return data, nil
}

// appendTrialBalanceLine keeps totals and grouping in the same append operation. That prevents
// a future report change from adding a visible row without also including it in the debit/credit
// control totals used to validate the statement flow.
func appendTrialBalanceLine(data *TrialBalanceData, line TrialBalanceLine) {
	data.Lines = append(data.Lines, line)
	data.TotalDebit += line.Debit
	data.TotalCredit += line.Credit
	groupKey := line.Note + ":" + line.AccountType
	if len(data.Groups) == 0 || data.Groups[len(data.Groups)-1].Key != groupKey {
		data.Groups = append(data.Groups, TrialBalanceGroup{
			Key:         groupKey,
			AccountType: line.AccountType,
			TypeLabel:   trialBalanceTypeLabel(line.AccountType),
			Note:        line.Note,
			Title:       trialBalanceGroupTitle(line.Note, line.AccountType),
		})
	}
	lastGroup := &data.Groups[len(data.Groups)-1]
	lastGroup.Lines = append(lastGroup.Lines, line)
}

func assignNaturalBalance(line *TrialBalanceLine, amount float64) {
	if line.AccountType == "asset" || line.AccountType == "expenditure" {
		if amount >= 0 {
			line.Debit = amount
		} else {
			line.Credit = -amount
		}
		return
	}
	if amount >= 0 {
		line.Credit = amount
	} else {
		line.Debit = -amount
	}
}

func trialBalanceAmount(accountType string, debit, credit float64) float64 {
	if accountType == "asset" || accountType == "expenditure" {
		return debit - credit
	}
	return credit - debit
}

func trialBalanceNormalSide(accountType string) string {
	if accountType == "asset" || accountType == "expenditure" {
		return "Debit"
	}
	return "Credit"
}

func trialBalanceTypeLabel(accountType string) string {
	switch accountType {
	case "income":
		return "Income"
	case "expenditure":
		return "Expenditure"
	case "asset":
		return "Asset"
	case "liability":
		return "Liability"
	case "equity":
		return "Equity / Fund"
	default:
		return accountType
	}
}

func trialBalanceGroupTitle(noteRef, accountType string) string {
	if noteRef != "" {
		return noteTitle(noteRef, "Note "+noteRef)
	}
	if accountType == "equity" {
		return "Opening funds and equity"
	}
	return "Direct accounts"
}

func absFloat(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}
