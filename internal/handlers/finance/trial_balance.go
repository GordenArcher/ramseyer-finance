package finance

import (
	"fmt"
	"net/http"
	"ramseyer-finance/internal/db"
	"strconv"
)

// TrialBalanceLine is one calculated category total for the selected year. Amount is the sum
// of the dated transactions the operator actually saved; the lookup never creates a second side.
type TrialBalanceLine struct {
	ID          int64
	AccountType string
	Note        string
	Account     string
	Amount      float64
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
	Active           string
	Year             string
	Years            []int
	Lines            []TrialBalanceLine
	Groups           []TrialBalanceGroup
	IncomeTotal      float64
	ExpenditureTotal float64
	AssetTotal       float64
	LiabilityTotal   float64
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

// buildTrialBalanceData calculates a selected-year lookup directly from dated transactions.
// Every category type uses the same year boundary: a 2026 entry belongs to 2026 and cannot
// leak into 2025 or be carried into 2027 by this lookup.
func buildTrialBalanceData(year int) (TrialBalanceData, error) {
	years, err := reportYears(year)
	if err != nil {
		return TrialBalanceData{}, err
	}
	data := TrialBalanceData{Active: "trial-balance", Year: strconv.Itoa(year), Years: years}
	startDate, endDate := yearBounds(year)
	rows, err := db.DB.Query(`
		SELECT c.id, c.type, c.note_ref, c.name,
			COALESCE(SUM(posting.amount), 0)
		FROM categories c
		JOIN financial_postings posting
			ON posting.category_id = c.id
			AND posting.date >= ?
			AND posting.date < ?
		WHERE COALESCE(c.is_active, 1) = 1
		GROUP BY c.id, c.type, c.note_ref, c.name, c.parent_id
		ORDER BY CAST(NULLIF(c.note_ref, '') AS INTEGER),
			CASE c.type WHEN 'income' THEN 0 WHEN 'expenditure' THEN 1 WHEN 'asset' THEN 2 ELSE 3 END,
			CASE WHEN c.parent_id = 0 THEN c.id ELSE c.parent_id END, c.parent_id, c.id
	`, startDate, endDate)
	if err != nil {
		return TrialBalanceData{}, fmt.Errorf("query calculated Trial Balance rows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var line TrialBalanceLine
		if err := rows.Scan(&line.ID, &line.AccountType, &line.Note, &line.Account, &line.Amount); err != nil {
			return TrialBalanceData{}, fmt.Errorf("scan calculated Trial Balance row: %w", err)
		}
		appendTrialBalanceLine(&data, line)
	}
	if err := rows.Err(); err != nil {
		return TrialBalanceData{}, err
	}

	return data, nil
}

// appendTrialBalanceLine keeps category-type totals and grouping in the same operation, so a
// visible row cannot be omitted from the quick summary at the bottom of the page.
func appendTrialBalanceLine(data *TrialBalanceData, line TrialBalanceLine) {
	data.Lines = append(data.Lines, line)
	switch line.AccountType {
	case "income":
		data.IncomeTotal += line.Amount
	case "expenditure":
		data.ExpenditureTotal += line.Amount
	case "asset":
		data.AssetTotal += line.Amount
	case "liability":
		data.LiabilityTotal += line.Amount
	}
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
